package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	}
}

// ChatCompletion は非ストリーミングのチャット補完を実行する。
func (c *Client) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if req.Model == "" {
		req.Model = c.model
	}

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
