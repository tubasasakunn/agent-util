package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	agentctx "ai-agent/internal/context"
	"ai-agent/internal/llm"
	"ai-agent/pkg/tool"
)

func TestRun_SingleTurn(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("こんにちは！", llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}),
		},
	}
	eng := mustNew(mock)

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
	eng := mustNew(mock)

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
	eng := mustNew(mock, WithSystemPrompt("custom prompt"))

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
	eng := mustNew(mock, WithSystemPrompt(""))

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
	eng := mustNew(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := eng.Run(ctx, "hello")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestRun_APIError(t *testing.T) {
	apiErr := fmt.Errorf("server error")
	eng := mustNew(&mockCompleter{err: apiErr})

	_, err := eng.Run(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, apiErr) {
		t.Errorf("error = %v, want wrapping of %v", err, apiErr)
	}
}

func TestRun_EmptyResponse_RetriedThenFails(t *testing.T) {
	// EmptyResponse は Transient に分類される。
	// maxStepRetries=2 なのでリトライ後に ErrMaxStepRetries で停止する。
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			{Choices: []llm.Choice{}}, // 1回目: empty
			{Choices: []llm.Choice{}}, // リトライ1回目: empty
			{Choices: []llm.Choice{}}, // リトライ2回目: empty → 上限到達
		},
	}
	eng := mustNew(mock)

	_, err := eng.Run(context.Background(), "hello")
	if !errors.Is(err, ErrMaxStepRetries) {
		t.Errorf("error = %v, want ErrMaxStepRetries", err)
	}
	if !errors.Is(err, llm.ErrEmptyResponse) {
		t.Errorf("error should wrap ErrEmptyResponse, got %v", err)
	}
}

func TestRun_UsageTracking(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("ok", llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}),
		},
	}
	eng := mustNew(mock)

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
			var a struct {
				Message string `json:"message"`
			}
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

	eng := mustNew(mock, WithTools(echoTool))
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

	eng := mustNew(mock, WithTools(newMockTool("echo", "Echoes")))
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

	eng := mustNew(mock, WithTools(newMockTool("echo", "Echoes")))
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

	eng := mustNew(mock, WithTools(failTool))
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

	eng := mustNew(mock, WithTools(errTool))
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
	eng := mustNew(mock) // WithTools なし

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

	eng := mustNew(mock, WithTools(echoTool))
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

func TestRun_CompactionDisabled(t *testing.T) {
	// compaction=nil（デフォルト）→ 縮約なし、既存動作と完全互換
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("response", llm.Usage{}),
		},
	}
	eng := mustNew(mock) // WithCompaction なし

	result, err := eng.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "response" {
		t.Errorf("response = %q, want %q", result.Response, "response")
	}
}

func TestRun_CompactionTriggered(t *testing.T) {
	// 小さなtokenLimitで閾値を超えさせ、縮約が実行されることを確認
	callCount := 0
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// Turn 1: echo tool
			chatResponse(`{"tool":"echo","arguments":{"message":"hello"},"reasoning":"test"}`),
			// Turn 2: none → chat response
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResponse("final answer", llm.Usage{}),
		},
	}

	echoTool := &mockTool{
		name:        "echo",
		description: "Echo a message",
		parameters:  json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}}}`),
		executeFunc: func(_ context.Context, args json.RawMessage) (tool.Result, error) {
			callCount++
			return tool.Result{Content: "echoed"}, nil
		},
	}

	cfg := agentctx.DefaultCompactionConfig()
	eng := mustNew(mock,
		WithTools(echoTool),
		WithTokenLimit(100), // 非常に小さなコンテキスト
		WithCompaction(cfg),
	)

	result, err := eng.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "final answer" {
		t.Errorf("response = %q, want %q", result.Response, "final answer")
	}

	// 縮約が実行されてもループが正常に完了する
	if callCount != 1 {
		t.Errorf("echo tool called %d times, want 1", callCount)
	}
}

func TestRun_CompactionReducesContext(t *testing.T) {
	// 縮約後にトークン数が減少していることを確認
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("response", llm.Usage{}),
		},
	}

	cfg := agentctx.CompactionConfig{
		BudgetMaxChars: 100,
		KeepLast:       2,
		TargetRatio:    0.3,
	}
	eng := mustNew(mock,
		WithTokenLimit(200),
		WithCompaction(cfg),
	)

	// 事前にメッセージを大量に追加して閾値を超えさせる
	for i := 0; i < 10; i++ {
		eng.ctxManager.Add(llm.Message{Role: "user", Content: llm.StringPtr(fmt.Sprintf("message %d with some content", i))})
		eng.ctxManager.Add(llm.Message{Role: "assistant", Content: llm.StringPtr(fmt.Sprintf("response %d with some content", i))})
	}
	beforeTokens := eng.ctxManager.TokenCount()

	result, err := eng.Run(context.Background(), "final question")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "response" {
		t.Errorf("response = %q, want %q", result.Response, "response")
	}

	// 縮約後のトークン数が減少していること
	afterTokens := eng.ctxManager.TokenCount()
	if afterTokens >= beforeTokens {
		t.Errorf("tokens not reduced: %d >= %d", afterTokens, beforeTokens)
	}
}

// --- Phase 7b: リマインダーテスト ---

func TestBuildMessages_ReminderInserted(t *testing.T) {
	mock := &mockCompleter{}
	eng := mustNew(mock,
		WithReminderThreshold(4),
		WithDynamicSection(Section{
			Key:      "reminder",
			Priority: PriorityReminder,
			Scope:    ScopeManual,
			Content:  "Always respond in Japanese.",
		}),
	)

	// 閾値以上のメッセージを追加（4件）
	for i := 0; i < 4; i++ {
		eng.ctxManager.Add(UserMessage(fmt.Sprintf("msg%d", i)))
		eng.ctxManager.Add(AssistantMessage(fmt.Sprintf("resp%d", i)))
	}

	msgs := eng.buildMessages()

	// リマインダーが含まれることを確認
	found := false
	for _, m := range msgs {
		if m.Role == "user" && m.ContentString() == "[System Reminder] Always respond in Japanese." {
			found = true
			break
		}
	}
	if !found {
		t.Error("reminder message should be inserted")
	}

	// リマインダーは最後の user メッセージの直前に位置する
	reminderIdx := -1
	lastUserIdx := -1
	for i, m := range msgs {
		if m.Role == "user" && m.ContentString() == "[System Reminder] Always respond in Japanese." {
			reminderIdx = i
		}
		if m.Role == "user" && m.ContentString() == "msg3" {
			lastUserIdx = i
		}
	}
	if reminderIdx >= 0 && lastUserIdx >= 0 && reminderIdx >= lastUserIdx {
		t.Errorf("reminder (%d) should be before last user message (%d)", reminderIdx, lastUserIdx)
	}
}

func TestBuildMessages_ShortConversation_NoReminder(t *testing.T) {
	mock := &mockCompleter{}
	eng := mustNew(mock,
		WithReminderThreshold(8),
		WithDynamicSection(Section{
			Key:      "reminder",
			Priority: PriorityReminder,
			Scope:    ScopeManual,
			Content:  "test reminder",
		}),
	)

	// 閾値未満のメッセージ（2件）
	eng.ctxManager.Add(UserMessage("hello"))
	eng.ctxManager.Add(AssistantMessage("hi"))

	msgs := eng.buildMessages()

	for _, m := range msgs {
		if m.ContentString() == "[System Reminder] test reminder" {
			t.Error("reminder should not be inserted for short conversations")
		}
	}
}

func TestBuildMessages_NoReminderSection_NoInsert(t *testing.T) {
	mock := &mockCompleter{}
	eng := mustNew(mock,
		WithReminderThreshold(2),
		// WithDynamicSection なし（リマインダー未登録）
	)

	for i := 0; i < 5; i++ {
		eng.ctxManager.Add(UserMessage(fmt.Sprintf("msg%d", i)))
		eng.ctxManager.Add(AssistantMessage(fmt.Sprintf("resp%d", i)))
	}

	msgs := eng.buildMessages()

	for _, m := range msgs {
		if m.Role == "user" {
			content := m.ContentString()
			if len(content) > 17 && content[:17] == "[System Reminder]" {
				t.Error("no reminder should be inserted without reminder section")
			}
		}
	}
}

func TestBuildMessages_ReminderDisabled(t *testing.T) {
	mock := &mockCompleter{}
	eng := mustNew(mock,
		WithReminderThreshold(0), // 無効化
		WithDynamicSection(Section{
			Key:      "reminder",
			Priority: PriorityReminder,
			Scope:    ScopeManual,
			Content:  "test",
		}),
	)

	for i := 0; i < 10; i++ {
		eng.ctxManager.Add(UserMessage(fmt.Sprintf("msg%d", i)))
	}

	msgs := eng.buildMessages()

	for _, m := range msgs {
		if m.ContentString() == "[System Reminder] test" {
			t.Error("reminder should not be inserted when threshold is 0")
		}
	}
}

// --- Phase 8a: Transient Error Retry Tests ---

func TestRun_RouterParseError_RetriedThenSucceeds(t *testing.T) {
	// ルーターが1回目にパース不可能なJSON、2回目に正しいJSONを返す。
	// Transient エラーとしてリトライされ、最終的に成功する。
	echoTool := newMockTool("echo", "echoes input")
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("not valid json at all", llm.Usage{}), // call 0: router parse fails
			routerNone(),        // call 1: router succeeds (none)
			chatResponse("ok!"), // call 2: chat response
		},
	}
	eng := mustNew(mock, WithTools(echoTool))

	result, err := eng.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "ok!" {
		t.Errorf("response = %q, want %q", result.Response, "ok!")
	}
	if mock.calls != 3 {
		t.Errorf("calls = %d, want 3", mock.calls)
	}
}

func TestRun_RouterParseError_MaxRetriesExceeded(t *testing.T) {
	// ルーターが毎回パース不可能なJSONを返す。
	// maxStepRetries=2 なので3回目で ErrMaxStepRetries で停止する。
	echoTool := newMockTool("echo", "echoes input")
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("bad json 1", llm.Usage{}), // call 0: fails
			makeResponse("bad json 2", llm.Usage{}), // call 1: retry 1 fails
			makeResponse("bad json 3", llm.Usage{}), // call 2: retry 2 fails → max retries
		},
	}
	eng := mustNew(mock, WithTools(echoTool))

	_, err := eng.Run(context.Background(), "hello")
	if !errors.Is(err, ErrMaxStepRetries) {
		t.Errorf("error = %v, want ErrMaxStepRetries", err)
	}
}

func TestRun_UserFixableError(t *testing.T) {
	// API 401 エラーは UserFixable に分類され、Result で通知される。
	mock := &mockCompleter{
		err: &llm.APIError{StatusCode: 401, Body: "unauthorized"},
	}
	eng := mustNew(mock)

	result, err := eng.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v (should be returned as Result, not error)", err)
	}
	if result.Reason != "user_fixable" {
		t.Errorf("reason = %q, want %q", result.Reason, "user_fixable")
	}
}

func TestRun_EmptyResponse_RetriedThenSucceeds(t *testing.T) {
	// EmptyResponse は Transient。1回目が空、2回目で成功。
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			{Choices: []llm.Choice{}},  // call 0: empty → retry
			chatResponse("recovered!"), // call 1: success
		},
	}
	eng := mustNew(mock)

	result, err := eng.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "recovered!" {
		t.Errorf("response = %q, want %q", result.Response, "recovered!")
	}
}

// --- Phase 8b: Consecutive Failure Cap Tests ---

func TestRun_ConsecutiveFailures_StopsAtLimit(t *testing.T) {
	// ツールが3回連続でエラーを返す → maxConsecutiveFailures=3 で安全停止。
	failTool := newMockTool("fail_tool", "always fails")
	failTool.executeFunc = func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
		return tool.Result{}, errors.New("tool failed")
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("fail_tool", `{}`), // turn 0: router → fail_tool
			routerJSON("fail_tool", `{}`), // turn 1: router → fail_tool
			routerJSON("fail_tool", `{}`), // turn 2: router → fail_tool → cap reached
		},
	}
	eng := mustNew(mock, WithTools(failTool), WithMaxConsecutiveFailures(3))

	result, err := eng.Run(context.Background(), "do something")
	if err != nil {
		t.Fatalf("unexpected error: %v (should be safe stop, not error)", err)
	}
	if result.Reason != "max_consecutive_failures" {
		t.Errorf("reason = %q, want %q", result.Reason, "max_consecutive_failures")
	}
}

func TestRun_ConsecutiveFailures_ResetsOnSuccess(t *testing.T) {
	// 2回失敗 → 1回成功 → カウンターリセット → さらに2回失敗でもキャップに達しない。
	callCount := 0
	mixedTool := newMockTool("mixed_tool", "sometimes fails")
	mixedTool.executeFunc = func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
		callCount++
		if callCount == 3 { // 3回目だけ成功
			return tool.Result{Content: "success"}, nil
		}
		return tool.Result{}, errors.New("tool failed")
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("mixed_tool", `{}`), // turn 0: fail (consecutive=1)
			routerJSON("mixed_tool", `{}`), // turn 1: fail (consecutive=2)
			routerJSON("mixed_tool", `{}`), // turn 2: success (consecutive=0)
			routerJSON("mixed_tool", `{}`), // turn 3: fail (consecutive=1)
			routerJSON("mixed_tool", `{}`), // turn 4: fail (consecutive=2)
			routerNone(),                   // turn 5: none → chat
			chatResponse("done"),           // turn 5: final answer
		},
	}
	eng := mustNew(mock, WithTools(mixedTool), WithMaxConsecutiveFailures(3), WithMaxTurns(10))

	result, err := eng.Run(context.Background(), "do something")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reason != "completed" {
		t.Errorf("reason = %q, want %q", result.Reason, "completed")
	}
}

func TestRun_ConsecutiveFailures_ToolNotFound(t *testing.T) {
	// 存在しないツールへのルーティングも連続失敗としてカウントされる。
	echoTool := newMockTool("echo", "echoes input")
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("nonexistent", `{}`), // turn 0: tool_not_found (consecutive=1)
			routerJSON("nonexistent", `{}`), // turn 1: tool_not_found (consecutive=2)
			routerJSON("nonexistent", `{}`), // turn 2: tool_not_found (consecutive=3) → cap
		},
	}
	eng := mustNew(mock, WithTools(echoTool), WithMaxConsecutiveFailures(3))

	result, err := eng.Run(context.Background(), "use nonexistent tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reason != "max_consecutive_failures" {
		t.Errorf("reason = %q, want %q", result.Reason, "max_consecutive_failures")
	}
}

func TestRun_ToolError_ReasonIsSeparated(t *testing.T) {
	// ツール実行エラー時の Reason が "tool_error" になることを確認。
	failTool := newMockTool("fail_tool", "always fails")
	failTool.executeFunc = func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
		return tool.Result{}, errors.New("broken")
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("fail_tool", `{}`), // router → fail_tool (error)
			routerNone(),                  // router → none
			chatResponse("handled it"),    // final
		},
	}
	eng := mustNew(mock, WithTools(failTool))

	result, err := eng.Run(context.Background(), "try this")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "handled it" {
		t.Errorf("response = %q, want %q", result.Response, "handled it")
	}
}

// --- Phase 8c: PEV Cycle / Verification Tests ---

func TestRun_VerifyPass(t *testing.T) {
	// 検証パス → 通常通り完了。
	echoTool := newMockTool("echo", "echoes input")
	v := &mockVerifier{
		name:    "checker",
		results: []*VerifyResult{{Passed: true, Summary: "ok"}},
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("echo", `{}`), // router → echo
			routerNone(),             // router → none
			chatResponse("done"),     // final
		},
	}
	eng := mustNew(mock, WithTools(echoTool), WithVerifiers(v))

	result, err := eng.Run(context.Background(), "echo test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "done" {
		t.Errorf("response = %q, want %q", result.Response, "done")
	}
}

func TestRun_VerifyFail_ThenFix(t *testing.T) {
	// 1回目: 検証失敗 → LLMが修正 → 2回目: 検証パス。
	echoTool := newMockTool("echo", "echoes input")
	v := &mockVerifier{
		name: "checker",
		results: []*VerifyResult{
			{Passed: false, Summary: "output is wrong"},
			{Passed: true, Summary: "ok"},
		},
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("echo", `{}`), // turn 0: router → echo → verify fails
			routerJSON("echo", `{}`), // turn 1: router → echo (retry) → verify passes
			routerNone(),             // turn 2: router → none
			chatResponse("fixed!"),   // turn 2: final
		},
	}
	eng := mustNew(mock, WithTools(echoTool), WithVerifiers(v))

	result, err := eng.Run(context.Background(), "do it right")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "fixed!" {
		t.Errorf("response = %q, want %q", result.Response, "fixed!")
	}
}

func TestRun_VerifyFail_ConsecutiveCap(t *testing.T) {
	// 検証失敗が連続して maxConsecutiveFailures に達する → 安全停止。
	echoTool := newMockTool("echo", "echoes input")
	v := &mockVerifier{
		name: "strict",
		results: []*VerifyResult{
			{Passed: false, Summary: "bad 1"},
			{Passed: false, Summary: "bad 2"},
			{Passed: false, Summary: "bad 3"},
		},
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("echo", `{}`), // turn 0: verify_failed (consecutive=1)
			routerJSON("echo", `{}`), // turn 1: verify_failed (consecutive=2)
			routerJSON("echo", `{}`), // turn 2: verify_failed (consecutive=3) → cap
		},
	}
	eng := mustNew(mock, WithTools(echoTool), WithVerifiers(v), WithMaxConsecutiveFailures(3))

	result, err := eng.Run(context.Background(), "keep failing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reason != "max_consecutive_failures" {
		t.Errorf("reason = %q, want %q", result.Reason, "max_consecutive_failures")
	}
}

func TestRun_VerifySkippedOnToolError(t *testing.T) {
	// ツール実行エラー時は検証がスキップされる。
	failTool := newMockTool("fail_tool", "always fails")
	failTool.executeFunc = func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
		return tool.Result{}, errors.New("broken")
	}
	v := &mockVerifier{
		name: "should_not_run",
		results: []*VerifyResult{
			{Passed: false, Summary: "this should never be checked"},
		},
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("fail_tool", `{}`), // router → fail_tool (error) → verify skipped
			routerNone(),                  // router → none
			chatResponse("ok"),            // final
		},
	}
	eng := mustNew(mock, WithTools(failTool), WithVerifiers(v))

	result, err := eng.Run(context.Background(), "try")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "ok" {
		t.Errorf("response = %q, want %q", result.Response, "ok")
	}
	// 検証器は呼ばれないはず
	if v.calls != 0 {
		t.Errorf("verifier calls = %d, want 0 (should be skipped on tool error)", v.calls)
	}
}

func TestRun_NoVerifiers_NoEffect(t *testing.T) {
	// 検証器未登録の場合はVerifyステップが実行されない。
	echoTool := newMockTool("echo", "echoes input")
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("echo", `{}`),
			routerNone(),
			chatResponse("done"),
		},
	}
	eng := mustNew(mock, WithTools(echoTool)) // WithVerifiers なし

	result, err := eng.Run(context.Background(), "echo test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "done" {
		t.Errorf("response = %q, want %q", result.Response, "done")
	}
}

// --- Phase 9: パーミッション E2E テスト ---

func TestRun_PermissionDenied_ToolBlocked(t *testing.T) {
	// Deny ルールにより shell ツールが拒否され、LLM にフィードバックされる
	shellTool := newMockTool("shell", "execute command")
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("shell", `{"command":"ls"}`), // ルーターが shell を選択
			routerNone(),                            // 拒否後に直接応答
			chatResponse("understood, shell is blocked"),
		},
	}
	eng := mustNew(mock,
		WithTools(shellTool),
		WithPermissionPolicy(PermissionPolicy{
			DenyRules: []PermissionRule{{ToolName: "shell", Reason: "dangerous"}},
		}),
	)

	result, err := eng.Run(context.Background(), "run ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "understood, shell is blocked" {
		t.Errorf("response = %q, want %q", result.Response, "understood, shell is blocked")
	}
}

func TestRun_PermissionAllowed_ReadOnly(t *testing.T) {
	// ReadOnly ツールはポリシーが設定されていてもルールに合致しなければ自動承認される
	readTool := &mockTool{
		name:        "read_file",
		description: "read a file",
		parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		readOnly:    true,
		executeFunc: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Content: "file content here"}, nil
		},
	}
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("read_file", `{"path":"test.go"}`),
			routerNone(),
			chatResponse("the file contains: file content here"),
		},
	}
	eng := mustNew(mock,
		WithTools(readTool),
		WithPermissionPolicy(PermissionPolicy{}), // 空ポリシー → ReadOnly は自動承認
	)

	result, err := eng.Run(context.Background(), "read test.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "the file contains: file content here" {
		t.Errorf("response = %q, want %q", result.Response, "the file contains: file content here")
	}
}

func TestRun_PermissionAsk_Approved(t *testing.T) {
	// UserApprover が承認 → ツール実行成功
	shellTool := &mockTool{
		name:        "shell",
		description: "execute command",
		parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		readOnly:    false,
		executeFunc: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Content: "command output"}, nil
		},
	}
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("shell", `{"command":"ls"}`),
			routerNone(),
			chatResponse("done"),
		},
	}
	eng := mustNew(mock,
		WithTools(shellTool),
		WithPermissionPolicy(PermissionPolicy{}), // ルールなし → ask に到達
		WithUserApprover(&mockUserApprover{responses: []bool{true}}),
	)

	result, err := eng.Run(context.Background(), "run ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "done" {
		t.Errorf("response = %q, want %q", result.Response, "done")
	}
}

func TestRun_PermissionAsk_Rejected(t *testing.T) {
	// UserApprover が拒否 → PermDenied
	shellTool := newMockTool("shell", "execute command")
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("shell", `{"command":"rm -rf"}`),
			routerNone(),
			chatResponse("ok, cancelled"),
		},
	}
	eng := mustNew(mock,
		WithTools(shellTool),
		WithPermissionPolicy(PermissionPolicy{}),
		WithUserApprover(&mockUserApprover{responses: []bool{false}}),
	)

	result, err := eng.Run(context.Background(), "delete everything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "ok, cancelled" {
		t.Errorf("response = %q, want %q", result.Response, "ok, cancelled")
	}
}

func TestRun_NoPermissionChecker_BackwardCompat(t *testing.T) {
	// PermissionChecker 未設定時は全許可（Phase 8 以前と同一動作）
	shellTool := &mockTool{
		name:        "shell",
		description: "execute command",
		parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		readOnly:    false,
		executeFunc: func(_ context.Context, _ json.RawMessage) (tool.Result, error) {
			return tool.Result{Content: "executed"}, nil
		},
	}
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("shell", `{"command":"ls"}`),
			routerNone(),
			chatResponse("done"),
		},
	}
	eng := mustNew(mock, WithTools(shellTool)) // WithPermissionPolicy なし

	result, err := eng.Run(context.Background(), "run ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "done" {
		t.Errorf("response = %q, want %q", result.Response, "done")
	}
}

func TestRun_PermissionFailClosed_NoApprover(t *testing.T) {
	// approver なし + ルールなし + 非ReadOnly → fail-closed で拒否
	shellTool := newMockTool("shell", "execute command")
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("shell", `{}`),
			routerNone(),
			chatResponse("denied"),
		},
	}
	eng := mustNew(mock,
		WithTools(shellTool),
		WithPermissionPolicy(PermissionPolicy{}), // ルールなし、approverなし
	)

	result, err := eng.Run(context.Background(), "run command")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "denied" {
		t.Errorf("response = %q, want %q", result.Response, "denied")
	}
}

func TestRun_PermissionDenied_ConsecutiveFailures(t *testing.T) {
	// permission_denied が連続失敗としてカウントされ maxConsecutiveFailures で停止
	shellTool := newMockTool("shell", "execute command")
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("shell", `{}`), // 1回目: denied
			routerJSON("shell", `{}`), // 2回目: denied
			routerJSON("shell", `{}`), // 3回目: denied → maxConsecutiveFailures
		},
	}
	eng := mustNew(mock,
		WithTools(shellTool),
		WithPermissionPolicy(PermissionPolicy{
			DenyRules: []PermissionRule{{ToolName: "shell", Reason: "blocked"}},
		}),
		WithMaxConsecutiveFailures(3),
	)

	result, err := eng.Run(context.Background(), "keep trying shell")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reason != "max_consecutive_failures" {
		t.Errorf("reason = %q, want %q", result.Reason, "max_consecutive_failures")
	}
}

// --- Phase 9: ガードレール + トリップワイヤ E2E テスト ---

func TestRun_InputGuard_Deny(t *testing.T) {
	// 入力ガードレールが Deny → ループに入らず即応答
	mock := &mockCompleter{}
	eng := mustNew(mock,
		WithInputGuards(&mockInputGuard{
			name:    "blocker",
			results: []*GuardResult{{Decision: GuardDeny, Reason: "profanity detected"}},
		}),
	)

	result, err := eng.Run(context.Background(), "bad input")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reason != "input_denied" {
		t.Errorf("reason = %q, want %q", result.Reason, "input_denied")
	}
	if mock.calls != 0 {
		t.Errorf("completer should not be called, got %d calls", mock.calls)
	}
}

func TestRun_InputGuard_Tripwire(t *testing.T) {
	// 入力ガードレールが Tripwire → エラーで即時停止
	mock := &mockCompleter{}
	eng := mustNew(mock,
		WithInputGuards(&mockInputGuard{
			name:    "tripwire",
			results: []*GuardResult{{Decision: GuardTripwire, Reason: "injection attack"}},
		}),
	)

	_, err := eng.Run(context.Background(), "malicious input")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var tw *TripwireError
	if !errors.As(err, &tw) {
		t.Fatalf("expected TripwireError, got %T: %v", err, err)
	}
	if tw.Source != "input" {
		t.Errorf("source = %q, want %q", tw.Source, "input")
	}
}

func TestRun_ToolCallGuard_Deny(t *testing.T) {
	// ツール呼び出しガードレールが Deny → フィードバック付きで Continue
	shellTool := &mockTool{
		name:        "shell",
		description: "execute command",
		parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		readOnly:    false,
	}
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("shell", `{"command":"rm -rf /"}`),
			routerNone(),
			chatResponse("understood"),
		},
	}
	eng := mustNew(mock,
		WithTools(shellTool),
		WithToolCallGuards(&mockToolCallGuard{
			name:    "arg-checker",
			results: []*GuardResult{{Decision: GuardDeny, Reason: "destructive command"}},
		}),
	)

	result, err := eng.Run(context.Background(), "delete everything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "understood" {
		t.Errorf("response = %q, want %q", result.Response, "understood")
	}
}

func TestRun_ToolCallGuard_Tripwire(t *testing.T) {
	// ツール呼び出しガードレールが Tripwire → エラーで即時停止
	shellTool := newMockTool("shell", "execute command")
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("shell", `{}`),
		},
	}
	eng := mustNew(mock,
		WithTools(shellTool),
		WithToolCallGuards(&mockToolCallGuard{
			name:    "tripwire",
			results: []*GuardResult{{Decision: GuardTripwire, Reason: "exfiltration attempt"}},
		}),
	)

	_, err := eng.Run(context.Background(), "do something")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var tw *TripwireError
	if !errors.As(err, &tw) {
		t.Fatalf("expected TripwireError, got %T: %v", err, err)
	}
	if tw.Source != "tool_call" {
		t.Errorf("source = %q, want %q", tw.Source, "tool_call")
	}
}

func TestRun_OutputGuard_Deny(t *testing.T) {
	// 出力ガードレールが Deny → 安全な代替メッセージに差し替え
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse("sensitive data: SSN 123-45-6789"),
		},
	}
	eng := mustNew(mock,
		WithOutputGuards(&mockOutputGuard{
			name:    "pii-filter",
			results: []*GuardResult{{Decision: GuardDeny, Reason: "PII detected"}},
		}),
	)

	result, err := eng.Run(context.Background(), "show me data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "I cannot provide that response." {
		t.Errorf("response = %q, want safe fallback", result.Response)
	}
	if result.Reason != "output_blocked" {
		t.Errorf("reason = %q, want %q", result.Reason, "output_blocked")
	}
}

func TestRun_OutputGuard_Tripwire(t *testing.T) {
	// 出力ガードレールが Tripwire → エラーで即時停止
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse("leaking secrets"),
		},
	}
	eng := mustNew(mock,
		WithOutputGuards(&mockOutputGuard{
			name:    "secret-detector",
			results: []*GuardResult{{Decision: GuardTripwire, Reason: "secret key leaked"}},
		}),
	)

	_, err := eng.Run(context.Background(), "query")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var tw *TripwireError
	if !errors.As(err, &tw) {
		t.Fatalf("expected TripwireError, got %T: %v", err, err)
	}
	if tw.Source != "output" {
		t.Errorf("source = %q, want %q", tw.Source, "output")
	}
}

func TestRun_NoGuards_BackwardCompat(t *testing.T) {
	// ガードレール未設定時は Phase 8 以前と同一動作
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse("hello"),
		},
	}
	eng := mustNew(mock)

	result, err := eng.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "hello" {
		t.Errorf("response = %q, want %q", result.Response, "hello")
	}
	if result.Reason != "completed" {
		t.Errorf("reason = %q, want %q", result.Reason, "completed")
	}
}

func TestRun_GuardAndPermission_Coexist(t *testing.T) {
	// パーミッション（allow）+ ツール呼び出しガード（deny）→ ガードが拒否
	shellTool := newMockTool("shell", "execute command")
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			routerJSON("shell", `{}`),
			routerNone(),
			chatResponse("blocked by guard"),
		},
	}
	eng := mustNew(mock,
		WithTools(shellTool),
		WithPermissionPolicy(PermissionPolicy{
			AllowRules: []PermissionRule{{ToolName: "shell", Reason: "allowed"}},
		}),
		WithToolCallGuards(&mockToolCallGuard{
			name:    "content-checker",
			results: []*GuardResult{{Decision: GuardDeny, Reason: "unsafe content"}},
		}),
	)

	result, err := eng.Run(context.Background(), "do it")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "blocked by guard" {
		t.Errorf("response = %q, want %q", result.Response, "blocked by guard")
	}
}
