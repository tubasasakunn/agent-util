package mcp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
)

// Transport は MCP メッセージの送受信を抽象化するインターフェース。
type Transport interface {
	Send(data []byte) error
	Receive() ([]byte, error)
	Close() error
}

// --- stdio トランスポート ---

// StdioTransport はサブプロセスの stdin/stdout を使う MCP トランスポート。
type StdioTransport struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
}

// NewStdioTransport は stdio トランスポートを起動する。
func NewStdioTransport(ctx context.Context, command string, args []string, env map[string]string) (*StdioTransport, error) {
	cmd := exec.CommandContext(ctx, command, args...)

	if len(env) > 0 {
		cmdEnv := cmd.Environ()
		for k, v := range env {
			cmdEnv = append(cmdEnv, k+"="+v)
		}
		cmd.Env = cmdEnv
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	return &StdioTransport{cmd: cmd, stdin: stdin, scanner: scanner}, nil
}

func (t *StdioTransport) Send(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := t.stdin.Write(append(data, '\n'))
	return err
}

func (t *StdioTransport) Receive() ([]byte, error) {
	for {
		if !t.scanner.Scan() {
			if err := t.scanner.Err(); err != nil {
				return nil, fmt.Errorf("read: %w", err)
			}
			return nil, fmt.Errorf("unexpected EOF")
		}
		line := t.scanner.Bytes()
		if len(line) > 0 {
			cp := make([]byte, len(line))
			copy(cp, line)
			return cp, nil
		}
	}
}

func (t *StdioTransport) Close() error {
	t.stdin.Close()
	return t.cmd.Wait()
}

// --- SSE トランスポート ---

// SSETransport は HTTP SSE (Server-Sent Events) を使う MCP トランスポート。
// MCP の SSE プロトコル:
// 1. GET /sse でSSEストリームに接続
// 2. サーバーが "endpoint" イベントでメッセージ送信先URLを通知
// 3. クライアントは POST でそのURLにJSON-RPCメッセージを送信
// 4. レスポンスは SSE ストリーム経由で受信
type SSETransport struct {
	baseURL     string
	postURL     string // endpoint イベントで受信した送信先URL
	client      *http.Client
	messages    chan []byte
	done        chan struct{}
	closeOnce   sync.Once
	mu          sync.Mutex
}

// NewSSETransport は SSE トランスポートを開始する。
func NewSSETransport(ctx context.Context, url string) (*SSETransport, error) {
	t := &SSETransport{
		baseURL:  url,
		client:   &http.Client{},
		messages: make(chan []byte, 64),
		done:     make(chan struct{}),
	}

	// SSE エンドポイントに接続
	sseURL := url
	if !strings.HasSuffix(sseURL, "/sse") {
		sseURL = strings.TrimRight(sseURL, "/") + "/sse"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect sse: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("sse status: %d", resp.StatusCode)
	}

	// SSE ストリームを読み取る goroutine
	go t.readSSE(resp.Body)

	// endpoint イベントを待つ
	select {
	case msg := <-t.messages:
		// 最初のメッセージは endpoint URL のはず（readSSE が "endpoint:" をチャネルに入れる前にフィルタする）
		// 実装上は readSSE 側で endpoint を設定するのでここでは確認のみ
		_ = msg
	case <-ctx.Done():
		t.Close()
		return nil, ctx.Err()
	}

	t.mu.Lock()
	if t.postURL == "" {
		t.mu.Unlock()
		t.Close()
		return nil, fmt.Errorf("no endpoint event received")
	}
	t.mu.Unlock()

	return t, nil
}

func (t *SSETransport) readSSE(body io.ReadCloser) {
	defer body.Close()
	defer close(t.done)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var eventType string
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// 空行 = イベント区切り
			if len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				if eventType == "endpoint" {
					// endpoint イベント: 送信先 URL を設定
					postURL := data
					if !strings.HasPrefix(postURL, "http") {
						// 相対URL → baseURL と結合
						postURL = strings.TrimRight(t.baseURL, "/") + "/" + strings.TrimLeft(postURL, "/")
					}
					t.mu.Lock()
					t.postURL = postURL
					t.mu.Unlock()
					// ダミーメッセージでブロック解除
					select {
					case t.messages <- []byte(`{"_endpoint":true}`):
					default:
					}
				} else if eventType == "message" || eventType == "" {
					// message イベント: JSON-RPC メッセージ
					select {
					case t.messages <- []byte(data):
					default:
						// チャネル満杯は無視
					}
				}
			}
			eventType = ""
			dataLines = nil
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func (t *SSETransport) Send(data []byte) error {
	t.mu.Lock()
	postURL := t.postURL
	t.mu.Unlock()

	if postURL == "" {
		return fmt.Errorf("no endpoint URL")
	}

	resp, err := t.client.Post(postURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("post status: %d", resp.StatusCode)
	}
	return nil
}

func (t *SSETransport) Receive() ([]byte, error) {
	select {
	case msg, ok := <-t.messages:
		if !ok {
			return nil, fmt.Errorf("sse stream closed")
		}
		return msg, nil
	case <-t.done:
		return nil, fmt.Errorf("sse stream ended")
	}
}

func (t *SSETransport) Close() error {
	t.closeOnce.Do(func() {
		// done チャネルを閉じて readSSE の停止を促す
		// （HTTP body の Close で scanner.Scan() が終了する）
	})
	return nil
}
