package investigation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	agentctx "ai-agent/internal/context"
	"ai-agent/internal/engine"
	"ai-agent/internal/llm"
	"ai-agent/pkg/tool"
)

// --- テストヘルパー ---

type mockCompleter struct {
	responses []*llm.ChatResponse
	requests  []*llm.ChatRequest
	callIdx   int
}

func (m *mockCompleter) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.requests = append(m.requests, req)
	idx := m.callIdx
	m.callIdx++
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return makeResponse("fallback", llm.Usage{}), nil
}

func makeResponse(content string, usage llm.Usage) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: llm.StringPtr(content)}}},
		Usage:   usage,
	}
}

func chatResponse(content string) *llm.ChatResponse {
	return makeResponse(content, llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
}

type mockTool struct {
	name        string
	description string
	parameters  json.RawMessage
	executeFunc func(context.Context, json.RawMessage) (tool.Result, error)
}

func (t *mockTool) Name() string                { return t.name }
func (t *mockTool) Description() string         { return t.description }
func (t *mockTool) Parameters() json.RawMessage { return t.parameters }
func (t *mockTool) IsReadOnly() bool            { return true }
func (t *mockTool) IsConcurrencySafe() bool     { return true }
func (t *mockTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	if t.executeFunc != nil {
		return t.executeFunc(ctx, args)
	}
	return tool.Result{Content: "ok"}, nil
}

func newMockTool(name, desc string) *mockTool {
	return &mockTool{
		name:        name,
		description: desc,
		parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

// --- シナリオ1: PromptBuilder セクション順序 ---

func TestScenario1_SectionOrdering(t *testing.T) {
	eng := engine.New(&mockCompleter{},
		engine.WithSystemPrompt("You are a helpful assistant."),
		engine.WithTools(newMockTool("echo", "Echoes a message")),
		engine.WithDynamicSection(engine.Section{
			Key:      "developer",
			Priority: engine.PriorityDeveloper,
			Scope:    engine.ScopeRouter,
			Content:  "## Developer Notes\nFollow project conventions.",
		}),
	)

	// PromptBuilder 経由でルータープロンプトを取得
	// Engine は promptBuilder を公開していないので、buildRouterMessages 経由で確認
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"test"}`),
			makeResponse("ok", llm.Usage{}),
		},
	}
	eng2 := engine.New(mock,
		engine.WithSystemPrompt("You are a helpful assistant."),
		engine.WithTools(newMockTool("echo", "Echoes a message")),
		engine.WithDynamicSection(engine.Section{
			Key:      "developer",
			Priority: engine.PriorityDeveloper,
			Scope:    engine.ScopeRouter,
			Content:  "## Developer Notes\nFollow project conventions.",
		}),
	)

	_, err := eng2.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// ルーターリクエストの system メッセージを確認
	req := mock.requests[0]
	sysMsg := req.Messages[0].ContentString()

	// 順序: system prompt → tools → developer notes → instructions
	sysIdx := strings.Index(sysMsg, "You are a helpful assistant.")
	toolsIdx := strings.Index(sysMsg, "## Available Tools")
	devIdx := strings.Index(sysMsg, "## Developer Notes")
	instrIdx := strings.Index(sysMsg, "## Instructions")

	if sysIdx < 0 || toolsIdx < 0 || devIdx < 0 || instrIdx < 0 {
		t.Fatalf("missing sections in system prompt (sys=%d, tools=%d, dev=%d, instr=%d)", sysIdx, toolsIdx, devIdx, instrIdx)
	}

	if sysIdx >= toolsIdx {
		t.Errorf("system (%d) should come before tools (%d)", sysIdx, toolsIdx)
	}
	if toolsIdx >= devIdx {
		t.Errorf("tools (%d) should come before developer (%d)", toolsIdx, devIdx)
	}
	if devIdx >= instrIdx {
		t.Errorf("developer (%d) should come before instructions (%d)", devIdx, instrIdx)
	}

	t.Logf("PASS: セクション順序 system(%d) < tools(%d) < developer(%d) < instructions(%d)", sysIdx, toolsIdx, devIdx, instrIdx)

	_ = eng // 未使用変数回避
}

// --- シナリオ2: 動的セクション展開 ---

func TestScenario2_DynamicSectionExpansion(t *testing.T) {
	callCount := 0
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"test"}`),
			makeResponse("ok", llm.Usage{}),
		},
	}

	eng := engine.New(mock,
		engine.WithTools(newMockTool("echo", "Echoes")),
		engine.WithDynamicSection(engine.Section{
			Key:      "dynamic_info",
			Priority: engine.PriorityDeveloper,
			Scope:    engine.ScopeRouter,
			Dynamic: func() string {
				callCount++
				return "## Runtime Info\nCurrent time: 2026-04-25T10:00:00Z"
			},
		}),
	)

	_, err := eng.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Dynamic 関数が呼ばれたことを確認
	if callCount == 0 {
		t.Error("Dynamic function was not called")
	}

	// ルータープロンプトにコンテンツが含まれる
	req := mock.requests[0]
	sysMsg := req.Messages[0].ContentString()
	if !strings.Contains(sysMsg, "Runtime Info") {
		t.Error("dynamic content not found in router system prompt")
	}
	if !strings.Contains(sysMsg, "2026-04-25") {
		t.Error("dynamic date not found in prompt")
	}

	t.Logf("PASS: Dynamic 関数が %d 回呼ばれ、コンテンツが展開された", callCount)
}

// --- シナリオ3: リマインダー挿入 ---

func TestScenario3_ReminderInsertion(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("final answer", llm.Usage{}),
		},
	}

	eng := engine.New(mock,
		engine.WithSystemPrompt("You are a helpful assistant."),
		engine.WithReminderThreshold(4),
		engine.WithDynamicSection(engine.Section{
			Key:      "reminder",
			Priority: engine.PriorityReminder,
			Scope:    engine.ScopeManual,
			Content:  "Always respond concisely.",
		}),
	)

	// 閾値以上のメッセージを事前追加（8件 >= threshold 4）
	for i := 0; i < 4; i++ {
		eng.AddMessage(llm.Message{Role: "user", Content: llm.StringPtr(fmt.Sprintf("question %d", i))})
		eng.AddMessage(llm.Message{Role: "assistant", Content: llm.StringPtr(fmt.Sprintf("answer %d", i))})
	}

	_, err := eng.Run(context.Background(), "final question")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// リクエストメッセージにリマインダーが含まれる
	req := mock.requests[0]
	reminderFound := false
	for _, m := range req.Messages {
		if m.Role == "user" && strings.Contains(m.ContentString(), "[System Reminder]") {
			reminderFound = true
			if !strings.Contains(m.ContentString(), "Always respond concisely.") {
				t.Error("reminder content mismatch")
			}
			break
		}
	}
	if !reminderFound {
		t.Error("reminder was not inserted into messages")
	}

	t.Log("PASS: リマインダーが長い会話に正しく挿入された")
}

// --- シナリオ4: リマインダー非挿入（短い会話） ---

func TestScenario4_NoReminderForShortConversation(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("ok", llm.Usage{}),
		},
	}

	eng := engine.New(mock,
		engine.WithReminderThreshold(8),
		engine.WithDynamicSection(engine.Section{
			Key:      "reminder",
			Priority: engine.PriorityReminder,
			Scope:    engine.ScopeManual,
			Content:  "test reminder",
		}),
	)

	_, err := eng.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	req := mock.requests[0]
	for _, m := range req.Messages {
		if strings.Contains(m.ContentString(), "[System Reminder]") {
			t.Error("reminder should NOT be inserted for short conversations")
		}
	}

	t.Log("PASS: 短い会話ではリマインダーが挿入されない")
}

// --- シナリオ5: MEMORY インデックス ---

func TestScenario5_MemoryIndex(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"test"}`),
			makeResponse("ok", llm.Usage{}),
		},
	}

	eng := engine.New(mock,
		engine.WithTools(newMockTool("read_file", "Reads a file")),
		engine.WithMemoryEntries(
			engine.MemoryEntry{Key: "adr-001", Summary: "JSON-RPC over stdio", Path: "decisions/001.md"},
			engine.MemoryEntry{Key: "project", Summary: "プロジェクト構造", Path: "CLAUDE.md"},
		),
	)

	_, err := eng.Run(context.Background(), "tell me about the project")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	req := mock.requests[0]
	sysMsg := req.Messages[0].ContentString()

	if !strings.Contains(sysMsg, "Knowledge Index") {
		t.Error("missing Knowledge Index header")
	}
	if !strings.Contains(sysMsg, "[adr-001] JSON-RPC over stdio") {
		t.Error("missing adr-001 entry")
	}
	if !strings.Contains(sysMsg, "decisions/001.md") {
		t.Error("missing path for adr-001")
	}
	if !strings.Contains(sysMsg, "read_file") {
		t.Error("missing read_file instruction in memory index")
	}

	t.Log("PASS: MEMORY インデックスがルータープロンプトに正しく含まれる")
}

// --- シナリオ6: ツールスコーピング MaxTools ---

func TestScenario6_ToolScoping(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"test"}`),
			makeResponse("ok", llm.Usage{}),
		},
	}

	eng := engine.New(mock,
		engine.WithTools(
			newMockTool("alpha", "A tool"),
			newMockTool("beta", "B tool"),
			newMockTool("gamma", "G tool"),
		),
		engine.WithToolScope(engine.ToolScope{MaxTools: 2}),
	)

	_, err := eng.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	req := mock.requests[0]
	sysMsg := req.Messages[0].ContentString()

	if !strings.Contains(sysMsg, "### alpha") {
		t.Error("should contain alpha")
	}
	if !strings.Contains(sysMsg, "### beta") {
		t.Error("should contain beta")
	}
	if strings.Contains(sysMsg, "### gamma") {
		t.Error("should NOT contain gamma (MaxTools=2)")
	}

	t.Log("PASS: MaxTools=2 で3ツール中2つだけが含まれる")
}

// --- シナリオ7: ツールスコーピング IncludeAlways ---

func TestScenario7_ToolScopingIncludeAlways(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"test"}`),
			makeResponse("ok", llm.Usage{}),
		},
	}

	eng := engine.New(mock,
		engine.WithTools(
			newMockTool("alpha", "A tool"),
			newMockTool("beta", "B tool"),
			newMockTool("gamma", "G tool"),
		),
		engine.WithToolScope(engine.ToolScope{
			MaxTools:      2,
			IncludeAlways: map[string]bool{"gamma": true},
		}),
	)

	_, err := eng.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	req := mock.requests[0]
	sysMsg := req.Messages[0].ContentString()

	if !strings.Contains(sysMsg, "### gamma") {
		t.Error("IncludeAlways gamma should be present")
	}
	if !strings.Contains(sysMsg, "### alpha") {
		t.Error("should fill remaining with first registered (alpha)")
	}
	if strings.Contains(sysMsg, "### beta") {
		t.Error("beta should be excluded (bumped by IncludeAlways)")
	}

	t.Log("PASS: IncludeAlways で gamma が優先され、残り枠に alpha が入る")
}

// --- シナリオ8: 予約トークン整合性 ---

func TestScenario8_ReservedTokensConsistency(t *testing.T) {
	eng := engine.New(&mockCompleter{},
		engine.WithSystemPrompt("You are a helpful assistant."),
		engine.WithTools(newMockTool("echo", "Echoes")),
		engine.WithTokenLimit(8192),
		engine.WithMemoryEntries(
			engine.MemoryEntry{Key: "adr-001", Summary: "JSON-RPC", Path: "adr.md"},
		),
		engine.WithDynamicSection(engine.Section{
			Key:      "reminder",
			Priority: engine.PriorityReminder,
			Scope:    engine.ScopeManual,
			Content:  "Remember the rules.",
		}),
	)

	// reserved tokens が合理的な範囲にある
	reserved := eng.ReservedTokens()
	if reserved <= 0 {
		t.Errorf("reserved tokens should be positive, got %d", reserved)
	}
	if reserved > 2000 {
		t.Errorf("reserved tokens too high for small prompt: %d", reserved)
	}

	// usage ratio は reserved/limit 以上
	ratio := eng.UsageRatio()
	expectedMinRatio := float64(reserved) / 8192.0
	if ratio < expectedMinRatio*0.9 { // 10% の誤差を許容
		t.Errorf("usage ratio %.4f is too low for reserved %d / 8192", ratio, reserved)
	}

	t.Logf("PASS: reserved=%d tokens, usage=%.1f%%", reserved, ratio*100)
}

// --- シナリオ9: E2E PromptBuilder + ツール実行 ---

func TestScenario9_E2E_PromptBuilderWithToolExecution(t *testing.T) {
	echoTool := &mockTool{
		name:        "echo",
		description: "Echoes a message",
		parameters:  json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`),
		executeFunc: func(_ context.Context, args json.RawMessage) (tool.Result, error) {
			var a struct {
				Message string `json:"message"`
			}
			json.Unmarshal(args, &a)
			return tool.Result{Content: "Echo: " + a.Message}, nil
		},
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. ルーター: echo を選択
			chatResponse(`{"tool":"echo","arguments":{"message":"hello"},"reasoning":"echo test"}`),
			// 2. ルーター: none → 最終応答
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"have result"}`),
			// 3. chatStep: 最終応答
			makeResponse("The result is: Echo: hello", llm.Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30}),
		},
	}

	cfg := agentctx.DefaultCompactionConfig()
	eng := engine.New(mock,
		engine.WithSystemPrompt("You are a helpful assistant."),
		engine.WithTools(echoTool),
		engine.WithCompaction(cfg),
		engine.WithMemoryEntries(
			engine.MemoryEntry{Key: "test", Summary: "Test entry", Path: "test.md"},
		),
		engine.WithDynamicSection(engine.Section{
			Key:      "developer",
			Priority: engine.PriorityDeveloper,
			Scope:    engine.ScopeRouter,
			Content:  "## Dev Notes\nFollow conventions.",
		}),
	)

	result, err := eng.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if result.Response != "The result is: Echo: hello" {
		t.Errorf("response = %q", result.Response)
	}
	if result.Turns != 2 {
		t.Errorf("turns = %d, want 2", result.Turns)
	}

	// ルーターリクエストにセクションが含まれていることを確認
	routerReq := mock.requests[0]
	sysMsg := routerReq.Messages[0].ContentString()
	if !strings.Contains(sysMsg, "You are a helpful assistant.") {
		t.Error("missing system prompt")
	}
	if !strings.Contains(sysMsg, "### echo") {
		t.Error("missing echo tool definition")
	}
	if !strings.Contains(sysMsg, "Knowledge Index") {
		t.Error("missing memory index")
	}
	if !strings.Contains(sysMsg, "Dev Notes") {
		t.Error("missing developer notes")
	}
	if !strings.Contains(sysMsg, "## Instructions") {
		t.Error("missing instructions")
	}

	t.Log("PASS: PromptBuilder 経由で全セクション含む E2E フロー完了")
}
