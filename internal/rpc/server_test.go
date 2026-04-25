package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-agent/pkg/protocol"
)

// readLine は reader から 1 行読み取って JSON メッセージとして返す。
func readLine(t *testing.T, r io.Reader, buf *bytes.Buffer) []byte {
	t.Helper()
	// buf に蓄積された分から改行を探す
	for {
		if idx := bytes.IndexByte(buf.Bytes(), '\n'); idx >= 0 {
			line := make([]byte, idx)
			copy(line, buf.Bytes()[:idx])
			buf.Next(idx + 1)
			return line
		}
		tmp := make([]byte, 4096)
		n, err := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			if buf.Len() > 0 {
				rest := buf.Bytes()
				buf.Reset()
				return rest
			}
			t.Fatalf("read: %v", err)
		}
	}
}

func sendRequest(t *testing.T, w io.Writer, req protocol.Request) {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

func parseResponse(t *testing.T, data []byte) protocol.Response {
	t.Helper()
	var resp protocol.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal response: %v (data: %s)", err, data)
	}
	return resp
}

func TestServer_HandleRequest(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	srv := New(clientToServer_r, serverToClient_w)
	srv.Handle("echo", func(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
		var p struct {
			Message string `json:"message"`
		}
		json.Unmarshal(params, &p)
		return map[string]string{"echo": p.Message}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var serveErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		serveErr = srv.Serve(ctx)
	})

	// リクエスト送信
	sendRequest(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  "echo",
		Params:  json.RawMessage(`{"message":"hello"}`),
		ID:      protocol.IntPtr(1),
	})

	// レスポンス読み取り
	buf := &bytes.Buffer{}
	line := readLine(t, serverToClient_r, buf)
	resp := parseResponse(t, line)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if *resp.ID != 1 {
		t.Errorf("ID = %d, want 1", *resp.ID)
	}

	var result map[string]string
	json.Unmarshal(resp.Result, &result)
	if result["echo"] != "hello" {
		t.Errorf("echo = %q, want %q", result["echo"], "hello")
	}

	cancel()
	clientToServer_w.Close()
	wg.Wait()

	if serveErr != nil && serveErr != context.Canceled {
		t.Errorf("Serve error: %v", serveErr)
	}
}

func TestServer_MethodNotFound(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	srv := New(clientToServer_r, serverToClient_w)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	sendRequest(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  "nonexistent",
		ID:      protocol.IntPtr(1),
	})

	buf := &bytes.Buffer{}
	line := readLine(t, serverToClient_r, buf)
	resp := parseResponse(t, line)

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != protocol.ErrCodeMethodNotFound {
		t.Errorf("code = %d, want %d", resp.Error.Code, protocol.ErrCodeMethodNotFound)
	}

	cancel()
	clientToServer_w.Close()
}

func TestServer_InvalidJSON(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	srv := New(clientToServer_r, serverToClient_w)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	// 不正な JSON を送信
	clientToServer_w.Write([]byte("{invalid json}\n"))

	buf := &bytes.Buffer{}
	line := readLine(t, serverToClient_r, buf)
	resp := parseResponse(t, line)

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != protocol.ErrCodeParse {
		t.Errorf("code = %d, want %d", resp.Error.Code, protocol.ErrCodeParse)
	}

	cancel()
	clientToServer_w.Close()
}

func TestServer_InvalidVersion(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	srv := New(clientToServer_r, serverToClient_w)
	srv.Handle("test", func(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
		return "ok", nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	sendRequest(t, clientToServer_w, protocol.Request{
		JSONRPC: "1.0", // 無効なバージョン
		Method:  "test",
		ID:      protocol.IntPtr(1),
	})

	buf := &bytes.Buffer{}
	line := readLine(t, serverToClient_r, buf)
	resp := parseResponse(t, line)

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != protocol.ErrCodeInvalidRequest {
		t.Errorf("code = %d, want %d", resp.Error.Code, protocol.ErrCodeInvalidRequest)
	}

	cancel()
	clientToServer_w.Close()
}

func TestServer_NotificationNoResponse(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	called := make(chan bool, 1)
	srv := New(clientToServer_r, serverToClient_w)
	srv.Handle("notify.test", func(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
		called <- true
		return "should not be sent", nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	// 通知（ID なし）を送信
	sendRequest(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  "notify.test",
		// ID なし → 通知
	})

	// ハンドラが呼ばれることを確認
	select {
	case <-called:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}

	// レスポンスが返らないことを確認（短いタイムアウトで）
	done := make(chan bool, 1)
	go func() {
		tmp := make([]byte, 1)
		serverToClient_r.Read(tmp)
		done <- true
	}()

	select {
	case <-done:
		t.Error("unexpected response for notification")
	case <-time.After(100 * time.Millisecond):
		// OK: レスポンスなし
	}

	cancel()
	clientToServer_w.Close()
}

func TestServer_Notify(t *testing.T) {
	var buf bytes.Buffer
	srv := New(strings.NewReader(""), &buf)

	err := srv.Notify("stream.delta", protocol.StreamDeltaParams{Text: "hi", Turn: 1})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}

	line := strings.TrimSpace(buf.String())
	var req protocol.Request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if req.Method != "stream.delta" {
		t.Errorf("Method = %q, want %q", req.Method, "stream.delta")
	}
	if req.ID != nil {
		t.Errorf("ID = %v, want nil", *req.ID)
	}
}

func TestServer_SendRequest(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	srv := New(clientToServer_r, serverToClient_w)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	// サーバーから SendRequest を goroutine で実行
	type sendResult struct {
		resp *protocol.Response
		err  error
	}
	resultCh := make(chan sendResult, 1)
	go func() {
		resp, err := srv.SendRequest(ctx, "tool.execute", protocol.ToolExecuteParams{
			Name: "greet",
			Args: json.RawMessage(`{"name":"Alice"}`),
		})
		resultCh <- sendResult{resp, err}
	}()

	// サーバーが書き出したリクエストを読み取る
	buf := &bytes.Buffer{}
	line := readLine(t, serverToClient_r, buf)

	var req protocol.Request
	if err := json.Unmarshal(line, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if req.Method != "tool.execute" {
		t.Errorf("Method = %q, want %q", req.Method, "tool.execute")
	}

	// レスポンスを返す（ラッパーからの応答をシミュレート）
	resp := protocol.Response{
		JSONRPC: protocol.Version,
		Result:  json.RawMessage(`{"content":"Hello, Alice!"}`),
		ID:      req.ID,
	}
	respData, _ := json.Marshal(resp)
	respData = append(respData, '\n')
	clientToServer_w.Write(respData)

	// SendRequest の結果を確認
	select {
	case sr := <-resultCh:
		if sr.err != nil {
			t.Fatalf("SendRequest error: %v", sr.err)
		}
		if sr.resp.Error != nil {
			t.Fatalf("unexpected error: %+v", sr.resp.Error)
		}
		var result protocol.ToolExecuteResult
		json.Unmarshal(sr.resp.Result, &result)
		if result.Content != "Hello, Alice!" {
			t.Errorf("Content = %q, want %q", result.Content, "Hello, Alice!")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SendRequest timeout")
	}

	cancel()
	clientToServer_w.Close()
}

func TestServer_ConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	// sync.Mutex で保護された writer
	w := &safeWriter{w: &buf, mu: &mu}

	srv := New(strings.NewReader(""), w)

	// 並行して複数の通知を書き込む
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			srv.Notify("test", map[string]int{"n": n})
		}(i)
	}
	wg.Wait()

	// 全ての行が有効な JSON であることを確認
	mu.Lock()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	mu.Unlock()

	if len(lines) != 10 {
		t.Fatalf("got %d lines, want 10", len(lines))
	}

	for i, line := range lines {
		var req protocol.Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
	}
}

type safeWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (sw *safeWriter) Write(p []byte) (n int, err error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.w.Write(p)
}

func TestServer_EOF(t *testing.T) {
	// stdin が即座に EOF → Serve は nil を返す
	srv := New(strings.NewReader(""), io.Discard)
	err := srv.Serve(context.Background())
	if err != nil {
		t.Errorf("Serve() = %v, want nil", err)
	}
}

func TestServer_EmptyLines(t *testing.T) {
	// 空行は無視される
	input := "\n\n\n"
	srv := New(strings.NewReader(input), io.Discard)
	err := srv.Serve(context.Background())
	if err != nil {
		t.Errorf("Serve() = %v, want nil", err)
	}
}

func TestServer_HandlerError(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	srv := New(clientToServer_r, serverToClient_w)
	srv.Handle("fail", func(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
		return nil, &protocol.RPCError{
			Code:    protocol.ErrCodeInternal,
			Message: "something broke",
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	sendRequest(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  "fail",
		ID:      protocol.IntPtr(1),
	})

	buf := &bytes.Buffer{}
	line := readLine(t, serverToClient_r, buf)
	resp := parseResponse(t, line)

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != protocol.ErrCodeInternal {
		t.Errorf("code = %d, want %d", resp.Error.Code, protocol.ErrCodeInternal)
	}
	if resp.Error.Message != "something broke" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "something broke")
	}

	cancel()
	clientToServer_w.Close()
}
