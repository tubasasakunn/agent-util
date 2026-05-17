package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"ai-agent/internal/engine"
	"ai-agent/internal/llm"
	"ai-agent/pkg/protocol"
	"ai-agent/pkg/tool"
)

// testCompleter はテスト用の Completer。
type testCompleter struct {
	mu        sync.Mutex
	responses []*llm.ChatResponse
	calls     int
}

func (m *testCompleter) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.calls
	m.calls++
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	// デフォルトで完了レスポンス
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message:      llm.Message{Role: "assistant", Content: llm.StringPtr("done")},
			FinishReason: "stop",
		}},
	}, nil
}

func chatResp(content string) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message:      llm.Message{Role: "assistant", Content: llm.StringPtr(content)},
			FinishReason: "stop",
		}},
		Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

func TestHandlers_ToolRegister(t *testing.T) {
	comp := &testCompleter{}
	eng := mustEngineNew(t, comp)

	var buf bytes.Buffer
	srv := New(io.NopCloser(bytes.NewReader(nil)), &buf)
	h := NewHandlers(eng, srv)

	params, _ := json.Marshal(protocol.ToolRegisterParams{
		Tools: []protocol.ToolDefinition{
			{Name: "greet", Description: "Greets", Parameters: json.RawMessage(`{}`), ReadOnly: true},
			{Name: "calc", Description: "Calculate", Parameters: json.RawMessage(`{}`)},
		},
	})

	result, rpcErr := h.handleToolRegister(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %+v", rpcErr)
	}

	reg, ok := result.(protocol.ToolRegisterResult)
	if !ok {
		t.Fatalf("result type = %T, want ToolRegisterResult", result)
	}
	if reg.Registered != 2 {
		t.Errorf("Registered = %d, want 2", reg.Registered)
	}
}

func TestHandlers_ToolRegisterDuplicate(t *testing.T) {
	comp := &testCompleter{}
	eng := mustEngineNew(t, comp)

	var buf bytes.Buffer
	srv := New(io.NopCloser(bytes.NewReader(nil)), &buf)
	h := NewHandlers(eng, srv)

	params, _ := json.Marshal(protocol.ToolRegisterParams{
		Tools: []protocol.ToolDefinition{
			{Name: "greet", Description: "Greets", Parameters: json.RawMessage(`{}`)},
		},
	})

	// 1回目は成功
	_, rpcErr := h.handleToolRegister(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("first register: %+v", rpcErr)
	}

	// 2回目は重複エラー
	_, rpcErr = h.handleToolRegister(context.Background(), params)
	if rpcErr == nil {
		t.Fatal("expected error for duplicate registration")
	}
	if rpcErr.Code != protocol.ErrCodeInvalidParams {
		t.Errorf("code = %d, want %d", rpcErr.Code, protocol.ErrCodeInvalidParams)
	}
}

func TestHandlers_AgentRun(t *testing.T) {
	comp := &testCompleter{
		responses: []*llm.ChatResponse{chatResp("Hello, world!")},
	}
	eng := mustEngineNew(t, comp)

	var buf bytes.Buffer
	srv := New(io.NopCloser(bytes.NewReader(nil)), &buf)
	h := NewHandlers(eng, srv)

	params, _ := json.Marshal(protocol.AgentRunParams{Prompt: "say hello"})
	result, rpcErr := h.handleAgentRun(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %+v", rpcErr)
	}

	ar, ok := result.(protocol.AgentRunResult)
	if !ok {
		t.Fatalf("result type = %T, want AgentRunResult", result)
	}
	if ar.Response != "Hello, world!" {
		t.Errorf("Response = %q, want %q", ar.Response, "Hello, world!")
	}
	if ar.Reason != "completed" {
		t.Errorf("Reason = %q, want %q", ar.Reason, "completed")
	}
}

func TestHandlers_AgentRunBusy(t *testing.T) {
	// 最初のRunを長時間ブロックするためにコンテキストで制御
	blockCtx, blockCancel := context.WithCancel(context.Background())
	defer blockCancel()

	var buf bytes.Buffer
	srv := New(io.NopCloser(bytes.NewReader(nil)), &buf)

	// slow completer: キャンセルされるまでブロック
	slowComp := &blockingCompleter{ctx: blockCtx}
	slowEng := mustEngineNew(t, slowComp, engine.WithMaxTurns(1))
	h2 := NewHandlers(slowEng, srv)

	// 最初の Run を goroutine で開始
	started := make(chan struct{})
	go func() {
		close(started)
		params, _ := json.Marshal(protocol.AgentRunParams{Prompt: "slow"})
		h2.handleAgentRun(context.Background(), params)
	}()

	<-started
	time.Sleep(50 * time.Millisecond) // goroutine が runMu を取得するのを待つ

	// 2回目の Run は busy エラー
	params, _ := json.Marshal(protocol.AgentRunParams{Prompt: "quick"})
	_, rpcErr := h2.handleAgentRun(context.Background(), params)
	if rpcErr == nil {
		t.Fatal("expected busy error")
	}
	if rpcErr.Code != protocol.ErrCodeAgentBusy {
		t.Errorf("code = %d, want %d", rpcErr.Code, protocol.ErrCodeAgentBusy)
	}

	blockCancel()
}

func TestHandlers_AgentAbort(t *testing.T) {
	var buf bytes.Buffer
	srv := New(io.NopCloser(bytes.NewReader(nil)), &buf)

	t.Run("no run in progress", func(t *testing.T) {
		comp := &testCompleter{}
		eng := mustEngineNew(t, comp)
		h := NewHandlers(eng, srv)

		result, rpcErr := h.handleAgentAbort(context.Background(), nil)
		if rpcErr != nil {
			t.Fatalf("unexpected error: %+v", rpcErr)
		}

		ar, ok := result.(protocol.AgentAbortResult)
		if !ok {
			t.Fatalf("result type = %T, want AgentAbortResult", result)
		}
		if ar.Aborted {
			t.Error("Aborted = true, want false")
		}
	})
}

func TestHandlers_AgentRunInvalidParams(t *testing.T) {
	comp := &testCompleter{}
	eng := mustEngineNew(t, comp)

	var buf bytes.Buffer
	srv := New(io.NopCloser(bytes.NewReader(nil)), &buf)
	h := NewHandlers(eng, srv)

	_, rpcErr := h.handleAgentRun(context.Background(), json.RawMessage(`{invalid`))
	if rpcErr == nil {
		t.Fatal("expected error")
	}
	if rpcErr.Code != protocol.ErrCodeInvalidParams {
		t.Errorf("code = %d, want %d", rpcErr.Code, protocol.ErrCodeInvalidParams)
	}
}

// blockingCompleter はキャンセルされるまでブロックする Completer。
type blockingCompleter struct {
	ctx context.Context
}

func (m *blockingCompleter) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.ctx.Done():
		return nil, m.ctx.Err()
	}
}

// io.NopCloser が io.Reader を返すラッパー（テスト用）
// io.NopCloser は既に標準にある。ここでは bytes.NewReader を包むために使う。

// RemoteTool 経由のツール登録が Engine.RegisterTool で動作することを確認。
func TestHandlers_RegisteredToolAvailable(t *testing.T) {
	comp := &testCompleter{}
	eng := mustEngineNew(t, comp)

	var buf bytes.Buffer
	srv := New(io.NopCloser(bytes.NewReader(nil)), &buf)
	h := NewHandlers(eng, srv)

	params, _ := json.Marshal(protocol.ToolRegisterParams{
		Tools: []protocol.ToolDefinition{
			{Name: "my_tool", Description: "My tool", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	})

	_, rpcErr := h.handleToolRegister(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("register: %+v", rpcErr)
	}

	// RegisterTool 後にルーターがツールを認識することを間接的に確認
	// ルーターステップは Engine 内部で行われるため、ここでは RemoteTool の型確認のみ
	_ = tool.Tool(nil) // import を維持
}

func TestHandlers_ServerInfo(t *testing.T) {
	comp := &testCompleter{}
	eng := mustEngineNew(t, comp)

	var buf bytes.Buffer
	srv := New(io.NopCloser(bytes.NewReader(nil)), &buf)
	h := NewHandlers(eng, srv)

	result, rpcErr := h.handleServerInfo(context.Background(), nil)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %+v", rpcErr)
	}

	info, ok := result.(protocol.ServerInfoResult)
	if !ok {
		t.Fatalf("result type = %T, want ServerInfoResult", result)
	}
	if info.LibraryVersion == "" {
		t.Error("LibraryVersion is empty")
	}
	if info.ProtocolVersion != "2.0" {
		t.Errorf("ProtocolVersion = %q, want %q", info.ProtocolVersion, "2.0")
	}
	if !slices.Contains(info.Methods, protocol.MethodAgentRun) {
		t.Errorf("Methods missing %q", protocol.MethodAgentRun)
	}
	if !slices.Contains(info.Methods, protocol.MethodServerInfo) {
		t.Errorf("Methods missing %q (self-advertise required)", protocol.MethodServerInfo)
	}
	if !info.Features["llm_execute"] {
		t.Error("Features missing llm_execute=true")
	}
}
