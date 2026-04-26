// Phase 6a delegate_task サブエージェント委譲の統合検証
// go test -v ./.claude/skills/investigation/004_phase6a_delegate_task/
//
// ユニットテスト（internal/engine/delegate_test.go）とは異なり、
// 実際のエージェント利用シナリオでの委譲動作を検証する。
package investigation

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
	calls     int
}

func (m *mockCompleter) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.requests = append(m.requests, req)
	i := m.calls
	m.calls++
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return nil, fmt.Errorf("unexpected call %d (total responses: %d)", i, len(m.responses))
}

func chatResp(content string) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{Message: llm.Message{Role: "assistant", Content: llm.StringPtr(content)}, FinishReason: "stop"},
		},
		Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

func makeResp(content string, usage llm.Usage) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{Message: llm.Message{Role: "assistant", Content: llm.StringPtr(content)}, FinishReason: "stop"},
		},
		Usage: usage,
	}
}

type readFileTool struct {
	content string // Execute で返す内容
}

func (t *readFileTool) Name() string        { return "read_file" }
func (t *readFileTool) Description() string { return "Read a file" }
func (t *readFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (t *readFileTool) IsReadOnly() bool        { return true }
func (t *readFileTool) IsConcurrencySafe() bool { return true }
func (t *readFileTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: t.content}, nil
}

// --- シナリオ 1: 単一 delegate の完全フロー ---

func TestScenario_SingleDelegateCompleteFlow(t *testing.T) {
	// 親がdelegate_taskを選択 → 子Engineが独立コンテキストで実行 → 結果を凝縮して返却 → 最終応答
	fileTool := &readFileTool{content: "func main() { fmt.Println(\"hello\") }"}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. 親ルーター: delegate_task を選択
			chatResp(`{"tool":"delegate_task","arguments":{"task":"analyze the main function in main.go","context":"The project is a Go CLI tool"},"reasoning":"complex analysis needed"}`),
			// 2. 子ルーター: read_file を選択（子にもツールがある）
			chatResp(`{"tool":"read_file","arguments":{"path":"main.go"},"reasoning":"need to read the file"}`),
			// 3. 子ルーター: none（ファイルを読んだので回答）
			chatResp(`{"tool":"none","arguments":{},"reasoning":"have the file content"}`),
			// 4. 子 chatStep: サブタスク結果
			makeResp("The main function prints hello. It uses fmt package.", llm.Usage{PromptTokens: 50, CompletionTokens: 20, TotalTokens: 70}),
			// 5. 親ルーター: none（サブタスク結果を見て最終応答）
			chatResp(`{"tool":"none","arguments":{},"reasoning":"have analysis from subtask"}`),
			// 6. 親 chatStep: 最終応答
			makeResp("Based on the analysis, the main function is a simple hello world program using the fmt package.",
				llm.Usage{PromptTokens: 80, CompletionTokens: 25, TotalTokens: 105}),
		},
	}

	eng := engine.New(mock,
		engine.WithTools(fileTool),
		engine.WithTokenLimit(8192),
		engine.WithSystemPrompt("You are a code analysis assistant."),
	)

	result, err := eng.Run(context.Background(), "analyze the main function")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	t.Logf("Response: %s", result.Response)
	t.Logf("Turns: %d", result.Turns)
	t.Logf("Total tokens: %d", result.Usage.TotalTokens)

	// 応答確認
	if !strings.Contains(result.Response, "hello world") {
		t.Errorf("unexpected response: %q", result.Response)
	}

	// ターン数: delegate_task(1) + final(1) = 2 (親のターン)
	if result.Turns != 2 {
		t.Errorf("turns = %d, want 2 (delegate + final)", result.Turns)
	}

	// 子Engineがツールを使ったことの確認（リクエスト数で間接的に）
	// 親ルーター(1) + 子ルーター(2) + 子chatStep(1) + 親ルーター(1) + 親chatStep(1) = 6
	if mock.calls != 6 {
		t.Errorf("total LLM calls = %d, want 6", mock.calls)
	}
}

// --- シナリオ 2: delegate 結果の凝縮 ---

func TestScenario_DelegateResultCondensation(t *testing.T) {
	fileTool := &readFileTool{content: "short content"}

	// 子Engineが3000文字の長い応答を返す
	longAnalysis := "Analysis result: " + strings.Repeat("This is a detailed analysis of the code. ", 75) // ~3000 chars

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. 親ルーター: delegate_task
			chatResp(`{"tool":"delegate_task","arguments":{"task":"detailed analysis"},"reasoning":"complex"}`),
			// 2. 子ルーター: none
			chatResp(`{"tool":"none","arguments":{},"reasoning":"can answer"}`),
			// 3. 子 chatStep: 長い応答
			makeResp(longAnalysis, llm.Usage{TotalTokens: 200}),
			// 4. 親ルーター: none
			chatResp(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			// 5. 親 chatStep
			makeResp("Summary: the analysis is complete.", llm.Usage{}),
		},
	}

	eng := engine.New(mock,
		engine.WithTools(fileTool),
		engine.WithTokenLimit(8192),
		engine.WithDelegateMaxChars(500), // 500文字に制限
	)

	result, err := eng.Run(context.Background(), "analyze in detail")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	t.Logf("Response: %s", result.Response)

	// 親ルーターへの最後のリクエストを確認（サブタスク結果が凝縮されているはず）
	// リクエスト[3]が親の2回目のルーター呼び出し
	parentRouterReq := mock.requests[3]
	foundCondensed := false
	for _, msg := range parentRouterReq.Messages {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "Subtask result") {
			content := msg.ContentString()
			t.Logf("Condensed result length: %d chars", len(content))

			if strings.Contains(content, "truncated") {
				foundCondensed = true
				t.Logf("Truncation marker found")
			}
			if strings.Contains(content, fmt.Sprintf("original: %d chars", len(longAnalysis))) {
				t.Logf("Original length preserved in marker")
			}

			// 凝縮後の内容がdelegateMaxChars + メタ情報程度に収まっているか
			// メタヘッダー + 500文字 + truncation marker = ~600文字以下
			if len(content) > 700 {
				t.Errorf("condensed result too long: %d chars (max expected ~700)", len(content))
			}
		}
	}
	if !foundCondensed {
		t.Error("expected condensed (truncated) subtask result in parent history")
	}
}

// --- シナリオ 3: delegate キャンセル伝播 ---

func TestScenario_DelegateCancelPropagation(t *testing.T) {
	fileTool := &readFileTool{content: "content"}
	ctx, cancel := context.WithCancel(context.Background())

	// 子EngineのLLM呼び出し中にキャンセルする
	callCount := 0
	cancellingMock := &cancelOnCallCompleter{
		responses: []*llm.ChatResponse{
			// 1. 親ルーター: delegate_task
			chatResp(`{"tool":"delegate_task","arguments":{"task":"slow task"},"reasoning":"test"}`),
			// 2. 子ルーターの呼び出し中にキャンセルが発生
		},
		cancelFunc: func(idx int) {
			if idx == 1 {
				cancel()
			}
		},
		callCount: &callCount,
	}

	eng := engine.New(cancellingMock,
		engine.WithTools(fileTool),
		engine.WithTokenLimit(8192),
	)

	_, err := eng.Run(ctx, "do a slow analysis")

	// context.Canceled がバブルアップすること
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	t.Logf("Error: %v", err)
	t.Logf("Total LLM calls before cancel: %d", callCount)

	// 子Engineで少なくとも1回のキャンセルが検出された
	if callCount < 2 {
		t.Errorf("expected at least 2 calls (parent router + child attempt), got %d", callCount)
	}
}

type cancelOnCallCompleter struct {
	responses  []*llm.ChatResponse
	cancelFunc func(callIdx int)
	callCount  *int
}

func (c *cancelOnCallCompleter) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	idx := *c.callCount
	*c.callCount++

	if c.cancelFunc != nil {
		c.cancelFunc(idx)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if idx < len(c.responses) {
		return c.responses[idx], nil
	}
	return nil, fmt.Errorf("unexpected call %d", idx)
}

// --- シナリオ 4: delegate 失敗時の回復 ---

func TestScenario_DelegateFailureRecovery(t *testing.T) {
	fileTool := &readFileTool{content: "content"}

	// 子Engineのmock応答を全ターン分用意して maxTurns に達させる
	responses := []*llm.ChatResponse{
		// 1. 親ルーター: delegate_task
		chatResp(`{"tool":"delegate_task","arguments":{"task":"impossible task"},"reasoning":"try delegate"}`),
	}
	// 子Engine: 全ターン(10回)ツール実行を繰り返して maxTurns に達する
	for i := 0; i < 10; i++ {
		responses = append(responses,
			chatResp(`{"tool":"read_file","arguments":{"path":"file.go"},"reasoning":"keep trying"}`))
	}
	// 子Engine maxTurns=10 → ErrMaxTurnsReached
	// 親に戻る
	responses = append(responses,
		// 2. 親ルーター: delegate 失敗を受けて read_file を直接試行
		chatResp(`{"tool":"read_file","arguments":{"path":"file.go"},"reasoning":"delegate failed, trying directly"}`),
		// 3. 親ルーター: none
		chatResp(`{"tool":"none","arguments":{},"reasoning":"have result now"}`),
		// 4. 親 chatStep: 最終応答
		makeResp("I read the file directly and found the answer.", llm.Usage{}),
	)

	mock := &mockCompleter{responses: responses}

	eng := engine.New(mock,
		engine.WithTools(fileTool),
		engine.WithTokenLimit(8192),
		engine.WithMaxTurns(10),
	)

	result, err := eng.Run(context.Background(), "analyze file.go")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	t.Logf("Response: %s", result.Response)
	t.Logf("Turns: %d", result.Turns)

	// 親が delegate 失敗後にリカバリして最終応答を返せること
	if !strings.Contains(result.Response, "directly") {
		t.Errorf("unexpected response: %q", result.Response)
	}

	// 親の履歴に失敗メッセージが含まれること
	parentRouterReq := mock.requests[11] // delegate失敗後の親ルーターリクエスト
	foundFailure := false
	for _, msg := range parentRouterReq.Messages {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "Subtask failed") {
			foundFailure = true
			t.Logf("Failure message: %s", msg.ContentString()[:min(100, len(msg.ContentString()))])
		}
	}
	if !foundFailure {
		t.Error("expected 'Subtask failed' in parent history after delegate failure")
	}
}

// --- シナリオ 5: delegate 後のコンテキスト使用率（8Kシミュレーション） ---

func TestScenario_DelegateContextUsageIn8K(t *testing.T) {
	// 8Kコンテキストで複数回のdelegate + 通常ツール使用のシナリオ
	fileTool := &readFileTool{content: strings.Repeat("code line\n", 50)} // 500文字

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// Turn 1: 通常のツール使用
			chatResp(`{"tool":"read_file","arguments":{"path":"file1.go"},"reasoning":"read file"}`),
			// Turn 2: delegate_task
			chatResp(`{"tool":"delegate_task","arguments":{"task":"analyze file1.go contents"},"reasoning":"complex analysis"}`),
			// 子ルーター: read_file
			chatResp(`{"tool":"read_file","arguments":{"path":"file1.go"},"reasoning":"read"}`),
			// 子ルーター: none
			chatResp(`{"tool":"none","arguments":{},"reasoning":"analyzed"}`),
			// 子 chatStep
			makeResp("The file has 50 lines of code.", llm.Usage{TotalTokens: 50}),
			// Turn 3: もう一つの delegate_task
			chatResp(`{"tool":"delegate_task","arguments":{"task":"check code quality"},"reasoning":"another subtask"}`),
			// 子ルーター: none
			chatResp(`{"tool":"none","arguments":{},"reasoning":"can answer"}`),
			// 子 chatStep
			makeResp("Code quality is good. No issues found.", llm.Usage{TotalTokens: 30}),
			// Turn 4: 最終応答
			chatResp(`{"tool":"none","arguments":{},"reasoning":"have all results"}`),
			makeResp("The code has 50 lines and quality is good.", llm.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}),
		},
	}

	cfg := agentctx.DefaultCompactionConfig()
	eng := engine.New(mock,
		engine.WithTools(fileTool),
		engine.WithTokenLimit(8192),
		engine.WithCompaction(cfg),
		engine.WithDelegateMaxChars(1500),
		engine.WithSystemPrompt("You are a code analysis assistant."),
	)

	result, err := eng.Run(context.Background(), "analyze my codebase quality")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	t.Logf("Response: %s", result.Response)
	t.Logf("Turns: %d", result.Turns)
	t.Logf("Total tokens used: %d", result.Usage.TotalTokens)

	// 応答確認
	if !strings.Contains(result.Response, "good") {
		t.Errorf("unexpected response: %q", result.Response)
	}

	// 4ターンで完了
	if result.Turns != 4 {
		t.Errorf("turns = %d, want 4 (tool + delegate + delegate + final)", result.Turns)
	}

	// 親の最終リクエストのメッセージ数を確認（コンテキストが爆発していないか）
	lastReq := mock.requests[len(mock.requests)-1]
	t.Logf("Final request message count: %d", len(lastReq.Messages))

	// サブタスク結果が2つ含まれる
	subtaskResults := 0
	for _, msg := range lastReq.Messages {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "Subtask result") {
			subtaskResults++
		}
	}
	if subtaskResults != 2 {
		t.Errorf("expected 2 subtask results in final context, got %d", subtaskResults)
	}
}

// --- シナリオ 6: ネスト delegate の防止 ---

func TestScenario_NestingDelegatePrevention(t *testing.T) {
	fileTool := &readFileTool{content: "content"}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. 親ルーター: delegate_task
			chatResp(`{"tool":"delegate_task","arguments":{"task":"subtask"},"reasoning":"test"}`),
			// 2. 子ルーター: none（子にはdelegate_taskがないので通常応答）
			chatResp(`{"tool":"none","arguments":{},"reasoning":"answer directly"}`),
			// 3. 子 chatStep
			makeResp("subtask done", llm.Usage{TotalTokens: 10}),
			// 4. 親ルーター: none
			chatResp(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			// 5. 親 chatStep
			makeResp("All done.", llm.Usage{}),
		},
	}

	eng := engine.New(mock,
		engine.WithTools(fileTool),
		engine.WithTokenLimit(8192),
	)

	result, err := eng.Run(context.Background(), "test nesting prevention")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	t.Logf("Response: %s", result.Response)

	// 子Engineのルータープロンプトに delegate_task が含まれていないことを確認
	// リクエスト[1]が子ルーターの呼び出し
	childRouterReq := mock.requests[1]
	if len(childRouterReq.Messages) == 0 {
		t.Fatal("expected child router request")
	}

	childSysPrompt := childRouterReq.Messages[0].ContentString()
	t.Logf("Child system prompt length: %d chars", len(childSysPrompt))

	if strings.Contains(childSysPrompt, "delegate_task") {
		t.Error("child engine should NOT have delegate_task in router prompt")
		t.Logf("Child system prompt (first 500 chars): %s", childSysPrompt[:min(500, len(childSysPrompt))])
	} else {
		t.Log("VERIFIED: delegate_task not in child router prompt (nesting prevented)")
	}

	// 親のルータープロンプトには delegate_task が含まれていること
	parentRouterReq := mock.requests[0]
	parentSysPrompt := parentRouterReq.Messages[0].ContentString()
	if !strings.Contains(parentSysPrompt, "delegate_task") {
		t.Error("parent engine SHOULD have delegate_task in router prompt")
	} else {
		t.Log("VERIFIED: delegate_task present in parent router prompt")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
