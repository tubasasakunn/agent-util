package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	// ErrAPIError はAPI側が返すエラー（400系/500系の最終結果）。
	ErrAPIError = errors.New("api error")
	// ErrEmptyResponse は choices が空のレスポンス。
	ErrEmptyResponse = errors.New("empty response")
)

// APIError はAPIエラーの詳細情報を持つ。
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error: status %d: %s", e.StatusCode, e.Body)
}

func (e *APIError) Unwrap() error { return ErrAPIError }

// Completer はチャット補完を実行するインターフェース。
// 異なるLLMバックエンドを差し替える場合はこのインターフェースを実装する。
type Completer interface {
	ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
}

// Client はOpenAI互換APIのHTTPクライアント。Completer を満たす。
type Client struct {
	endpoint   string
	model      string
	apiKey     string
	httpClient *http.Client
	maxRetries int
	logw       io.Writer
}

// NewClient は Functional Options で構成された Client を返す。
func NewClient(opts ...Option) *Client {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	httpClient := cfg.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.httpTimeout}
	}

	return &Client{
		endpoint:   cfg.endpoint,
		model:      cfg.model,
		apiKey:     cfg.apiKey,
		httpClient: httpClient,
		maxRetries: cfg.maxRetries,
		logw:       cfg.logWriter,
	}
}

// ChatCompletion は非ストリーミングのチャット補完を実行する。
func (c *Client) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	c.logRequest(req)
	start := time.Now()

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := c.buildHTTPRequest(ctx, bodyBytes)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.doWithRetry(ctx, bodyBytes, httpReq)
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	chatResp, err := parseResponse(respBody)
	if err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	c.logResponse(chatResp, time.Since(start))

	return chatResp, nil
}

// ParseContent は ChatResponse の content 文字列に FixJSON を適用して dest にデコードする。
// JSON mode やルーターパターンの出力パースに使う。
func ParseContent(resp *ChatResponse, dest any) error {
	if len(resp.Choices) == 0 {
		return fmt.Errorf("parse content: %w", ErrEmptyResponse)
	}
	raw := resp.Choices[0].Message.ContentString()
	if raw == "" {
		return fmt.Errorf("parse content: empty content")
	}
	fixed := FixJSON([]byte(raw))
	if err := json.Unmarshal(fixed, dest); err != nil {
		return fmt.Errorf("parse content: %w", err)
	}
	return nil
}

// buildHTTPRequest は HTTP リクエストを構築する。
func (c *Client) buildHTTPRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	return req, nil
}

// parseResponse はレスポンスボディを ChatResponse にデコードする。
func parseResponse(body []byte) (*ChatResponse, error) {
	var resp ChatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &resp, nil
}

// logf はログメッセージを出力する。logw が nil の場合は何もしない。
func (c *Client) logf(format string, args ...any) {
	if c.logw != nil {
		fmt.Fprintf(c.logw, format+"\n", args...)
	}
}

// logRequest はリクエストの概要をログ出力する。
func (c *Client) logRequest(req *ChatRequest) {
	if c.logw == nil {
		return
	}

	mode := "chat"
	if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object" {
		mode = "json"
	}

	// メッセージのロール構成を表示
	roles := make([]string, len(req.Messages))
	for i, m := range req.Messages {
		roles[i] = m.Role
	}

	c.logf("[llm] → %s mode=%s msgs=[%s] tools=%d",
		req.Model, mode, formatRoles(roles), len(req.Tools))
}

// logResponse はレスポンスの概要をログ出力する。
func (c *Client) logResponse(resp *ChatResponse, elapsed time.Duration) {
	if c.logw == nil {
		return
	}

	if len(resp.Choices) == 0 {
		c.logf("[llm] ← (empty) %.1fs", elapsed.Seconds())
		return
	}

	msg := resp.Choices[0].Message
	finish := resp.Choices[0].FinishReason

	if len(msg.ToolCalls) > 0 {
		names := make([]string, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			names[i] = tc.Function.Name
		}
		c.logf("[llm] ← tool_calls=[%s] finish=%s %dtok %.1fs",
			joinStr(names, ","), finish, resp.Usage.TotalTokens, elapsed.Seconds())
		return
	}

	content := msg.ContentString()
	preview := content
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	// 改行をスペースに置換してログの可読性を保つ
	preview = replaceNewlines(preview)

	c.logf("[llm] ← \"%s\" finish=%s %dtok %.1fs",
		preview, finish, resp.Usage.TotalTokens, elapsed.Seconds())
}

// formatRoles はロール一覧を連続する同一ロールをまとめて表示する。
// 例: [system, user, assistant, tool, assistant] → "S,U,A,T,A"
func formatRoles(roles []string) string {
	abbrevs := make([]string, len(roles))
	for i, r := range roles {
		switch r {
		case "system":
			abbrevs[i] = "S"
		case "user":
			abbrevs[i] = "U"
		case "assistant":
			abbrevs[i] = "A"
		case "tool":
			abbrevs[i] = "T"
		default:
			abbrevs[i] = r[:1]
		}
	}
	return joinStr(abbrevs, ",")
}

func joinStr(s []string, sep string) string {
	result := ""
	for i, v := range s {
		if i > 0 {
			result += sep
		}
		result += v
	}
	return result
}

func replaceNewlines(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			result = append(result, ' ')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}
