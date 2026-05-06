package rpc

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"ai-agent/pkg/protocol"
	"ai-agent/pkg/tool"
)

// RemoteTool が tool.Tool インターフェースを満たすことをコンパイル時に検証。
var _ tool.Tool = (*RemoteTool)(nil)

func TestRemoteTool_Metadata(t *testing.T) {
	def := tool.Definition{
		Name:        "greet",
		Description: "Greets a person",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		ReadOnly:    true,
	}
	rt := NewRemoteTool(def, nil)

	if rt.Name() != "greet" {
		t.Errorf("Name() = %q, want %q", rt.Name(), "greet")
	}
	if rt.Description() != "Greets a person" {
		t.Errorf("Description() = %q, want %q", rt.Description(), "Greets a person")
	}
	if string(rt.Parameters()) != `{"type":"object"}` {
		t.Errorf("Parameters() = %s, want %s", rt.Parameters(), `{"type":"object"}`)
	}
	if !rt.IsReadOnly() {
		t.Error("IsReadOnly() = false, want true")
	}
}

func TestRemoteTool_Execute(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	srv := New(clientToServer_r, serverToClient_w)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	def := tool.Definition{
		Name:        "greet",
		Description: "Greets",
		Parameters:  json.RawMessage(`{}`),
	}
	rt := NewRemoteTool(def, srv)

	// Execute を goroutine で実行
	type execResult struct {
		result tool.Result
		err    error
	}
	resultCh := make(chan execResult, 1)
	go func() {
		r, err := rt.Execute(ctx, json.RawMessage(`{"name":"Alice"}`))
		resultCh <- execResult{r, err}
	}()

	// tool.execute リクエストを読み取り
	buf := make([]byte, 4096)
	n, err := serverToClient_r.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var req protocol.Request
	if err := json.Unmarshal(buf[:n-1], &req); err != nil { // -1 for newline
		t.Fatalf("unmarshal: %v (data: %s)", err, buf[:n])
	}
	if req.Method != protocol.MethodToolExecute {
		t.Fatalf("Method = %q, want %q", req.Method, protocol.MethodToolExecute)
	}

	// レスポンスを返す
	resp := protocol.Response{
		JSONRPC: protocol.Version,
		ID:      req.ID,
	}
	resultData, _ := json.Marshal(protocol.ToolExecuteResult{Content: "Hello, Alice!"})
	resp.Result = resultData
	respBytes, _ := json.Marshal(resp)
	respBytes = append(respBytes, '\n')
	clientToServer_w.Write(respBytes)

	// Execute の結果を確認
	select {
	case er := <-resultCh:
		if er.err != nil {
			t.Fatalf("Execute error: %v", er.err)
		}
		if er.result.Content != "Hello, Alice!" {
			t.Errorf("Content = %q, want %q", er.result.Content, "Hello, Alice!")
		}
		if er.result.IsError {
			t.Error("IsError = true, want false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute timeout")
	}

	cancel()
	clientToServer_w.Close()
}

func TestRemoteTool_ExecuteError(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	srv := New(clientToServer_r, serverToClient_w)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	def := tool.Definition{Name: "fail_tool"}
	rt := NewRemoteTool(def, srv)

	type execResult struct {
		result tool.Result
		err    error
	}
	resultCh := make(chan execResult, 1)
	go func() {
		r, err := rt.Execute(ctx, json.RawMessage(`{}`))
		resultCh <- execResult{r, err}
	}()

	// リクエストを読み取り
	buf := make([]byte, 4096)
	n, _ := serverToClient_r.Read(buf)

	var req protocol.Request
	json.Unmarshal(buf[:n-1], &req)

	// エラーレスポンスを返す
	resp := protocol.Response{
		JSONRPC: protocol.Version,
		Error: &protocol.RPCError{
			Code:    protocol.ErrCodeToolExecFailed,
			Message: "tool failed",
		},
		ID: req.ID,
	}
	respBytes, _ := json.Marshal(resp)
	respBytes = append(respBytes, '\n')
	clientToServer_w.Write(respBytes)

	select {
	case er := <-resultCh:
		if er.err != nil {
			t.Fatalf("Execute error: %v", er.err)
		}
		if !er.result.IsError {
			t.Error("IsError = false, want true")
		}
		if er.result.Content != "tool failed" {
			t.Errorf("Content = %q, want %q", er.result.Content, "tool failed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	cancel()
	clientToServer_w.Close()
}

func TestRemoteTool_ExecuteTimeout(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	srv := New(clientToServer_r, serverToClient_w)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	// サーバーの出力を読み捨てる（パイプがブロックしないように）
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverToClient_r.Read(buf); err != nil {
				return
			}
		}
	}()

	def := tool.Definition{Name: "slow_tool"}
	rt := NewRemoteTool(def, srv)
	rt.timeout = 100 * time.Millisecond // 短いタイムアウト

	_, err := rt.Execute(ctx, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	cancel()
	clientToServer_w.Close()
}
