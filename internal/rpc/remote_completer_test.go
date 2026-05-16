package rpc

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"ai-agent/internal/llm"
	"ai-agent/pkg/protocol"
)

// RemoteCompleter が llm.Completer インターフェースを満たすことをコンパイル時に検証。
var _ llm.Completer = (*RemoteCompleter)(nil)

func TestRemoteCompleter_ChatCompletion(t *testing.T) {
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()

	srv := New(clientToServerR, serverToClientW)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	rc := NewRemoteCompleter(srv, 5*time.Second)

	type chatResult struct {
		resp *llm.ChatResponse
		err  error
	}
	resultCh := make(chan chatResult, 1)
	temp := 0.0
	go func() {
		r, err := rc.ChatCompletion(ctx, &llm.ChatRequest{
			Model: "gpt-test",
			Messages: []llm.Message{
				{Role: "user", Content: llm.StringPtr("hi")},
			},
			Temperature: &temp,
		})
		resultCh <- chatResult{r, err}
	}()

	// llm.execute リクエストを読み取り
	buf := make([]byte, 8192)
	n, err := serverToClientR.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var req protocol.Request
	if err := json.Unmarshal(buf[:n-1], &req); err != nil {
		t.Fatalf("unmarshal: %v (data: %s)", err, buf[:n])
	}
	if req.Method != protocol.MethodLLMExecute {
		t.Fatalf("Method = %q, want %q", req.Method, protocol.MethodLLMExecute)
	}

	// Params が ChatRequest を内包していること
	var params protocol.LLMExecuteParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	var gotReq llm.ChatRequest
	if err := json.Unmarshal(params.Request, &gotReq); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if gotReq.Model != "gpt-test" {
		t.Errorf("Model = %q, want %q", gotReq.Model, "gpt-test")
	}
	if len(gotReq.Messages) != 1 || gotReq.Messages[0].ContentString() != "hi" {
		t.Errorf("Messages = %+v", gotReq.Messages)
	}

	// レスポンスを返す
	chatResp := llm.ChatResponse{
		ID:    "wrapper-1",
		Model: "gpt-test",
		Choices: []llm.Choice{
			{
				Index:        0,
				Message:      llm.Message{Role: "assistant", Content: llm.StringPtr("hello back")},
				FinishReason: "stop",
			},
		},
		Usage: llm.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}
	respJSON, _ := json.Marshal(chatResp)
	resultData, _ := json.Marshal(protocol.LLMExecuteResult{Response: respJSON})

	resp := protocol.Response{
		JSONRPC: protocol.Version,
		ID:      req.ID,
		Result:  resultData,
	}
	respBytes, _ := json.Marshal(resp)
	respBytes = append(respBytes, '\n')
	clientToServerW.Write(respBytes)

	select {
	case cr := <-resultCh:
		if cr.err != nil {
			t.Fatalf("ChatCompletion error: %v", cr.err)
		}
		if len(cr.resp.Choices) != 1 {
			t.Fatalf("Choices len = %d", len(cr.resp.Choices))
		}
		got := cr.resp.Choices[0].Message.ContentString()
		if got != "hello back" {
			t.Errorf("content = %q, want %q", got, "hello back")
		}
		if cr.resp.Usage.TotalTokens != 3 {
			t.Errorf("Usage.TotalTokens = %d, want 3", cr.resp.Usage.TotalTokens)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ChatCompletion timeout")
	}

	cancel()
	clientToServerW.Close()
}

func TestRemoteCompleter_WrapperError(t *testing.T) {
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()

	srv := New(clientToServerR, serverToClientW)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	rc := NewRemoteCompleter(srv, 5*time.Second)

	resultCh := make(chan error, 1)
	go func() {
		_, err := rc.ChatCompletion(ctx, &llm.ChatRequest{
			Model:    "x",
			Messages: []llm.Message{{Role: "user", Content: llm.StringPtr("ping")}},
		})
		resultCh <- err
	}()

	buf := make([]byte, 8192)
	n, _ := serverToClientR.Read(buf)

	var req protocol.Request
	json.Unmarshal(buf[:n-1], &req)

	resp := protocol.Response{
		JSONRPC: protocol.Version,
		ID:      req.ID,
		Error: &protocol.RPCError{
			Code:    protocol.ErrCodeInternal,
			Message: "wrapper boom",
		},
	}
	respBytes, _ := json.Marshal(resp)
	respBytes = append(respBytes, '\n')
	clientToServerW.Write(respBytes)

	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	cancel()
	clientToServerW.Close()
}

func TestRemoteCompleter_DefaultTimeout(t *testing.T) {
	rc := NewRemoteCompleter(nil, 0)
	if rc.timeout != DefaultLLMTimeout {
		t.Errorf("timeout = %v, want %v", rc.timeout, DefaultLLMTimeout)
	}
}
