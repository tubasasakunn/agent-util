package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"ai-agent/internal/llm"
)

// mockCompleter は Completer インターフェースのテスト用実装。
type mockCompleter struct {
	responses []*llm.ChatResponse
	requests  []*llm.ChatRequest
	calls     int
}

func (m *mockCompleter) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.requests = append(m.requests, req)
	i := m.calls
	m.calls++
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return nil, fmt.Errorf("unexpected call %d", i)
}

// mockErrorCompleter はエラーを返す Completer。
type mockErrorCompleter struct {
	err error
}

func (m *mockErrorCompleter) ChatCompletion(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, m.err
}

func makeResponse(content string, usage llm.Usage) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{Message: llm.Message{Role: "assistant", Content: llm.StringPtr(content)}},
		},
		Usage: usage,
	}
}

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
	eng := New(&mockErrorCompleter{err: apiErr})

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
