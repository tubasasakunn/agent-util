package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"ai-agent/internal/llm"
	"ai-agent/pkg/tool"
)

func TestRun_SingleTurn(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("こんにちは！", llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}),
		},
	}
	eng := New(mock)

	result, err := eng.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "こんにちは！" {
		t.Errorf("response = %q, want %q", result.Response, "こんにちは！")
	}
	if result.Reason != "completed" {
		t.Errorf("reason = %q, want %q", result.Reason, "completed")
	}
	if result.Turns != 1 {
		t.Errorf("turns = %d, want 1", result.Turns)
	}
}

func TestRun_MessageHistory(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("response1", llm.Usage{}),
			makeResponse("response2", llm.Usage{}),
		},
	}
	eng := New(mock)

	if _, err := eng.Run(context.Background(), "first"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := eng.Run(context.Background(), "second"); err != nil {
		t.Fatalf("second run: %v", err)
	}

	// 2回目のリクエストには system + user1 + assistant1 + user2 が含まれる
	req := mock.requests[1]
	if len(req.Messages) != 4 {
		t.Fatalf("messages count = %d, want 4 (system + user1 + assistant1 + user2)", len(req.Messages))
	}
	roles := []string{"system", "user", "assistant", "user"}
	for i, want := range roles {
		if req.Messages[i].Role != want {
			t.Errorf("messages[%d].Role = %q, want %q", i, req.Messages[i].Role, want)
		}
	}
}

func TestRun_SystemPrompt(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("ok", llm.Usage{}),
		},
	}
	eng := New(mock, WithSystemPrompt("custom prompt"))

	if _, err := eng.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := mock.requests[0]
	if req.Messages[0].Role != "system" {
		t.Fatalf("messages[0].Role = %q, want %q", req.Messages[0].Role, "system")
	}
	if req.Messages[0].ContentString() != "custom prompt" {
		t.Errorf("system prompt = %q, want %q", req.Messages[0].ContentString(), "custom prompt")
	}
}

func TestRun_NoSystemPrompt(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("ok", llm.Usage{}),
		},
	}
	eng := New(mock, WithSystemPrompt(""))

	if _, err := eng.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := mock.requests[0]
	if req.Messages[0].Role != "user" {
		t.Errorf("messages[0].Role = %q, want %q (no system prompt expected)", req.Messages[0].Role, "user")
	}
}

func TestRun_ContextCanceled(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("ok", llm.Usage{}),
		},
	}
	eng := New(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := eng.Run(ctx, "hello")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestRun_APIError(t *testing.T) {
	apiErr := fmt.Errorf("server error")
	eng := New(&mockCompleter{err: apiErr})

	_, err := eng.Run(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, apiErr) {
		t.Errorf("error = %v, want wrapping of %v", err, apiErr)
	}
}

func TestRun_EmptyResponse(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			{Choices: []llm.Choice{}},
		},
	}
	eng := New(mock)

	_, err := eng.Run(context.Background(), "hello")
	if !errors.Is(err, llm.ErrEmptyResponse) {
		t.Errorf("error = %v, want ErrEmptyResponse", err)
	}
}

func TestRun_UsageTracking(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("ok", llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}),
		},
	}
	eng := New(mock)

	result, err := eng.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", result.Usage.PromptTokens)
	}
	if result.Usage.CompletionTokens != 5 {
		t.Errorf("CompletionTokens = %d, want 5", result.Usage.CompletionTokens)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", result.Usage.TotalTokens)
	}
}

// --- Phase 3: ツール実行テスト ---

func TestRun_WithTools_EndToEnd(t *testing.T) {
	// ルーター → ツール実行 → 最終応答 の完全フロー
	echoTool := &mockTool{
		name:        "echo",
		description: "Echoes a message",
		parameters:  json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`),
		executeFunc: func(_ context.Context, args json.RawMessage) (tool.Result, error) {
			var a struct{ Message string `json:"message"` }
			json.Unmarshal(args, &a)
			return tool.Result{Content: a.Message}, nil
		},
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. ルーター: echo を選択
			chatResponse(`{"tool":"echo","arguments":{"message":"hello"},"reasoning":"user wants echo"}`),
			// 2. ルーター: tool=none (ツール結果を見て最終応答へ)
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"already have result"}`),
			// 3. chatStep: 最終応答
			makeResponse("The echo result is: hello", llm.Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30}),
		},
	}

	eng := New(mock, WithTools(echoTool))
	result, err := eng.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "The echo result is: hello" {
		t.Errorf("response = %q, want %q", result.Response, "The echo result is: hello")
	}
	if result.Turns != 2 {
		t.Errorf("turns = %d, want 2", result.Turns)
	}
}

func TestToolStep_RouterSelectsNone(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. ルーター: tool=none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"simple question"}`),
			// 2. chatStep: 直接応答
			makeResponse("2", llm.Usage{}),
		},
	}

	eng := New(mock, WithTools(newMockTool("echo", "Echoes")))
	result, err := eng.Run(context.Background(), "1+1は?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "2" {
		t.Errorf("response = %q, want %q", result.Response, "2")
	}
	if result.Turns != 1 {
		t.Errorf("turns = %d, want 1", result.Turns)
	}
}

func TestToolStep_ToolNotFound(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. ルーター: 存在しないツール
			chatResponse(`{"tool":"nonexistent","arguments":{},"reasoning":"mistake"}`),
			// 2. ルーター: 修正して none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"no tool needed"}`),
			// 3. chatStep: 最終応答
			makeResponse("sorry, corrected", llm.Usage{}),
		},
	}

	eng := New(mock, WithTools(newMockTool("echo", "Echoes")))
	result, err := eng.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "sorry, corrected" {
		t.Errorf("response = %q", result.Response)
	}

	// 履歴にツール不明エラーが含まれることを確認
	found := false
	for _, msg := range eng.ctxManager.Messages() {
		if msg.Role == "tool" && msg.ContentString() != "" {
			if errors.Is(nil, nil) { // just checking content
				content := msg.ContentString()
				if len(content) > 0 && content[:5] == "Error" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("expected tool error message in history")
	}
}

func TestToolStep_ToolExecutionError(t *testing.T) {
	failTool := &mockTool{
		name:        "fail",
		description: "Always fails",
		parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		executeFunc: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{}, fmt.Errorf("disk full")
		},
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. ルーター: fail ツール選択
			chatResponse(`{"tool":"fail","arguments":{},"reasoning":"test"}`),
			// 2. ルーター: エラーを見て none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"tool failed"}`),
			// 3. chatStep: エラー報告
			makeResponse("The tool failed with disk full error", llm.Usage{}),
		},
	}

	eng := New(mock, WithTools(failTool))
	result, err := eng.Run(context.Background(), "run fail")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "The tool failed with disk full error" {
		t.Errorf("response = %q", result.Response)
	}
}

func TestToolStep_ToolBusinessError(t *testing.T) {
	errTool := &mockTool{
		name:        "read_file",
		description: "Reads a file",
		parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		executeFunc: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Content: "file not found", IsError: true}, nil
		},
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"read_file","arguments":{"path":"missing.txt"},"reasoning":"read file"}`),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"file not found"}`),
			makeResponse("The file was not found", llm.Usage{}),
		},
	}

	eng := New(mock, WithTools(errTool))
	result, err := eng.Run(context.Background(), "read missing.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 履歴に "Error:" プレフィックスのツール結果が含まれる
	foundError := false
	for _, msg := range eng.ctxManager.Messages() {
		if msg.Role == "tool" {
			content := msg.ContentString()
			if len(content) > 6 && content[:6] == "Error:" {
				foundError = true
			}
		}
	}
	if !foundError {
		t.Error("expected error-prefixed tool result in history")
	}
	if result.Response != "The file was not found" {
		t.Errorf("response = %q", result.Response)
	}
}

func TestRun_NoTools_Phase2Compatible(t *testing.T) {
	// ツール未登録時はPhase 2と同じ挙動
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("direct answer", llm.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8}),
		},
	}
	eng := New(mock) // WithTools なし

	result, err := eng.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "direct answer" {
		t.Errorf("response = %q, want %q", result.Response, "direct answer")
	}
	if result.Turns != 1 {
		t.Errorf("turns = %d, want 1", result.Turns)
	}

	// ResponseFormat が設定されていないことを確認 (ルーターが呼ばれていない)
	if mock.requests[0].ResponseFormat != nil {
		t.Error("ResponseFormat should be nil when no tools registered")
	}
}

func TestToolStep_MessageHistory(t *testing.T) {
	echoTool := &mockTool{
		name:        "echo",
		description: "Echoes",
		parameters:  json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}}}`),
		executeFunc: func(_ context.Context, args json.RawMessage) (tool.Result, error) {
			return tool.Result{Content: "echoed"}, nil
		},
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"echo","arguments":{"message":"hi"},"reasoning":"echo"}`),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResponse("done", llm.Usage{}),
		},
	}

	eng := New(mock, WithTools(echoTool))
	_, err := eng.Run(context.Background(), "echo hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// メッセージ履歴を確認: user → assistant(tool_calls) → tool → assistant(final)
	expectedRoles := []string{"user", "assistant", "tool", "assistant"}
	if len(eng.ctxManager.Messages()) != len(expectedRoles) {
		t.Fatalf("messages count = %d, want %d", len(eng.ctxManager.Messages()), len(expectedRoles))
	}
	for i, want := range expectedRoles {
		if eng.ctxManager.Messages()[i].Role != want {
			t.Errorf("messages[%d].Role = %q, want %q", i, eng.ctxManager.Messages()[i].Role, want)
		}
	}

	// assistant メッセージに tool_calls があること
	if len(eng.ctxManager.Messages()[1].ToolCalls) != 1 {
		t.Errorf("messages[1].ToolCalls count = %d, want 1", len(eng.ctxManager.Messages()[1].ToolCalls))
	}
	if eng.ctxManager.Messages()[1].ToolCalls[0].Function.Name != "echo" {
		t.Errorf("tool call name = %q, want %q", eng.ctxManager.Messages()[1].ToolCalls[0].Function.Name, "echo")
	}

	// tool メッセージの内容
	if eng.ctxManager.Messages()[2].ContentString() != "echoed" {
		t.Errorf("tool result = %q, want %q", eng.ctxManager.Messages()[2].ContentString(), "echoed")
	}

	// tool_call ID の対応
	if eng.ctxManager.Messages()[2].ToolCallID != eng.ctxManager.Messages()[1].ToolCalls[0].ID {
		t.Errorf("tool_call_id mismatch: %q != %q", eng.ctxManager.Messages()[2].ToolCallID, eng.ctxManager.Messages()[1].ToolCalls[0].ID)
	}
}
