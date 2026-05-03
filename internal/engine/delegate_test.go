package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"ai-agent/internal/llm"
	"ai-agent/pkg/tool"
)

func TestDelegateStep_BasicFlow(t *testing.T) {
	// ルーター → delegate_task → 子Engine実行 → 結果返却 → 最終応答
	echoTool := newMockTool("echo", "Echoes a message")

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. 親ルーター: delegate_task 選択
			chatResponse(`{"tool":"delegate_task","arguments":{"task":"analyze this"},"reasoning":"complex task"}`),
			// 2. 子ルーター: none 選択（ツールがあるので routerStep が走る）
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"can answer directly"}`),
			// 3. 子 chatStep: サブタスク結果
			makeResponse("analysis complete: everything looks good", llm.Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30}),
			// 4. 親ルーター: none 選択（サブタスク結果を見て）
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"have subtask result"}`),
			// 5. 親 chatStep: 最終応答
			makeResponse("Based on the analysis, everything looks good.", llm.Usage{PromptTokens: 30, CompletionTokens: 15, TotalTokens: 45}),
		},
	}

	eng := mustNew(mock, WithTools(echoTool))
	result, err := eng.Run(context.Background(), "analyze this complex topic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "Based on the analysis, everything looks good." {
		t.Errorf("response = %q", result.Response)
	}

	// 親の履歴に delegate_task のツール呼び出しと結果が含まれる
	msgs := eng.ctxManager.Messages()
	foundDelegateCall := false
	foundDelegateResult := false
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			if tc.Function.Name == "delegate_task" {
				foundDelegateCall = true
			}
		}
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "Subtask result") {
			foundDelegateResult = true
		}
	}
	if !foundDelegateCall {
		t.Error("expected delegate_task tool call in parent history")
	}
	if !foundDelegateResult {
		t.Error("expected subtask result in parent history")
	}
}

func TestDelegateStep_ResultCondensation(t *testing.T) {
	// 長い結果が delegateMaxChars に切り詰められる
	echoTool := newMockTool("echo", "Echoes")
	longResponse := strings.Repeat("x", 3000) // 3000文字の長い応答

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"delegate_task","arguments":{"task":"generate long text"},"reasoning":"test"}`),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"direct answer"}`),
			makeResponse(longResponse, llm.Usage{TotalTokens: 100}),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResponse("summarized", llm.Usage{}),
		},
	}

	eng := mustNew(mock, WithTools(echoTool), WithDelegateMaxChars(500))
	result, err := eng.Run(context.Background(), "generate long text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "summarized" {
		t.Errorf("response = %q", result.Response)
	}

	// 履歴内のサブタスク結果が切り詰められている
	for _, msg := range eng.ctxManager.Messages() {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "Subtask result") {
			content := msg.ContentString()
			if !strings.Contains(content, "truncated") {
				t.Error("expected truncated marker in condensed result")
			}
			if !strings.Contains(content, "original: 3000 chars") {
				t.Error("expected original length in truncation marker")
			}
		}
	}
}

func TestDelegateStep_ChildEngineError(t *testing.T) {
	// 子 Engine が maxTurns に達した場合、エラーがツール結果として親に返る
	echoTool := newMockTool("echo", "Echoes")

	// 親maxTurns=5, 子もmaxTurns=5（継承）
	// 子Engineが全ターンで echo を選択し続けて maxTurns に達する
	responses := []*llm.ChatResponse{
		// 親ルーター: delegate_task
		chatResponse(`{"tool":"delegate_task","arguments":{"task":"infinite loop"},"reasoning":"test"}`),
	}
	// 子Engineの5ターン分のechoレスポンス
	for i := 0; i < 5; i++ {
		responses = append(responses,
			chatResponse(`{"tool":"echo","arguments":{},"reasoning":"keep going"}`))
	}
	// 子Engine maxTurns=5 → ErrMaxTurnsReached → 親に戻る
	responses = append(responses,
		chatResponse(`{"tool":"none","arguments":{},"reasoning":"subtask failed"}`),
		makeResponse("The subtask could not complete.", llm.Usage{}),
	)

	mock := &mockCompleter{responses: responses}

	eng := mustNew(mock, WithTools(echoTool), WithMaxTurns(5))
	result, err := eng.Run(context.Background(), "do something complex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 親の履歴にエラー結果が含まれる
	foundError := false
	for _, msg := range eng.ctxManager.Messages() {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "Subtask failed") {
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected subtask failure message in parent history")
	}
	if result.Response != "The subtask could not complete." {
		t.Errorf("response = %q", result.Response)
	}
}

func TestDelegateStep_ContextCancellation(t *testing.T) {
	// 親の context キャンセルが子 Engine に伝播する
	echoTool := newMockTool("echo", "Echoes")

	ctx, cancel := context.WithCancel(context.Background())

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"delegate_task","arguments":{"task":"long task"},"reasoning":"test"}`),
		},
	}
	// 子Engineの ChatCompletion でキャンセルを発生させる
	cancellingMock := &mockCompleter{
		responses: nil,
		err:       nil,
	}
	_ = cancellingMock

	// ルーター呼び出し後にキャンセルする mockCompleter
	callCount := 0
	cancelOnCall := &mockCompleter{}
	cancelOnCall.responses = nil
	cancelOnCall.err = nil

	// カスタム mockCompleter を使う代わりに、シンプルにテスト
	// 子Engine生成前にキャンセルすることで伝播を検証
	mock2 := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"delegate_task","arguments":{"task":"test"},"reasoning":"test"}`),
		},
	}
	_ = callCount
	_ = mock

	// mockCompleter にキャンセル動作を組み込む
	callIdx := 0
	cancelMock := &cancellingCompleter{
		responses: []*llm.ChatResponse{
			// 1. 親ルーター: delegate_task
			chatResponse(`{"tool":"delegate_task","arguments":{"task":"test"},"reasoning":"test"}`),
			// 2. 子ルーター: この呼び出し中にキャンセル
		},
		cancelFunc: func(idx int) {
			if idx == 1 { // 子Engineのルーター呼び出し時にキャンセル
				cancel()
			}
		},
	}
	_ = mock2
	_ = callIdx

	eng := mustNew(cancelMock, WithTools(echoTool))
	_, err := eng.Run(ctx, "test cancellation")

	// context.Canceled が返ること（子Engineのエラーがバブルアップ）
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// delegateStep 内の child.Run がキャンセルされ、エラーが親に返る
	// ただし delegateStep はエラーをツール結果に変換するので、
	// context.Canceled は child.Run から返り、それが Subtask failed として記録される
	// 実際の動作: child.Run → context.Canceled → delegateStep が結果として格納 → Continue
	// その後の親の step でもキャンセルが検出される
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// cancellingCompleter はテスト用の Completer。
// 特定の呼び出し回数で cancelFunc を実行する。
type cancellingCompleter struct {
	responses  []*llm.ChatResponse
	cancelFunc func(callIdx int)
	calls      int
}

func (c *cancellingCompleter) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	idx := c.calls
	c.calls++

	if c.cancelFunc != nil {
		c.cancelFunc(idx)
	}

	// キャンセル済みなら即返す
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

func TestDelegateStep_InvalidArgs(t *testing.T) {
	echoTool := newMockTool("echo", "Echoes")

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// ルーター: 不正な引数で delegate_task
			chatResponse(`{"tool":"delegate_task","arguments":{"invalid":true},"reasoning":"test"}`),
			// ルーター: リカバリ
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"recovered"}`),
			makeResponse("recovered from error", llm.Usage{}),
		},
	}

	eng := mustNew(mock, WithTools(echoTool))
	result, err := eng.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// task が空なのでエラーがツール結果として返る
	foundError := false
	for _, msg := range eng.ctxManager.Messages() {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "task is required") {
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected 'task is required' error in tool result")
	}
	if result.Response != "recovered from error" {
		t.Errorf("response = %q", result.Response)
	}
}

func TestDelegateStep_NestingPrevention(t *testing.T) {
	// 子Engineのルータープロンプトに delegate_task が含まれないことを確認
	echoTool := newMockTool("echo", "Echoes")

	var childRequests []*llm.ChatRequest
	trackingMock := &trackingCompleter{
		responses: []*llm.ChatResponse{
			// 1. 親ルーター: delegate_task
			chatResponse(`{"tool":"delegate_task","arguments":{"task":"sub task"},"reasoning":"test"}`),
			// 2. 子ルーター: none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"answer directly"}`),
			// 3. 子 chatStep
			makeResponse("child answer", llm.Usage{TotalTokens: 10}),
			// 4. 親ルーター: none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			// 5. 親 chatStep
			makeResponse("final", llm.Usage{}),
		},
		onCall: func(idx int, req *llm.ChatRequest) {
			if idx == 1 { // 子ルーターの呼び出し
				childRequests = append(childRequests, req)
			}
		},
	}

	eng := mustNew(trackingMock, WithTools(echoTool))
	_, err := eng.Run(context.Background(), "test nesting")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 子ルーターのシステムプロンプトに delegate_task が含まれないこと
	if len(childRequests) == 0 {
		t.Fatal("expected child router request")
	}
	sysPrompt := childRequests[0].Messages[0].ContentString()
	if strings.Contains(sysPrompt, "delegate_task") {
		t.Error("child engine should NOT have delegate_task in router prompt")
	}
}

// trackingCompleter はテスト用の Completer。全リクエストを追跡する。
type trackingCompleter struct {
	responses []*llm.ChatResponse
	calls     int
	onCall    func(idx int, req *llm.ChatRequest)
}

func (c *trackingCompleter) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	idx := c.calls
	c.calls++
	if c.onCall != nil {
		c.onCall(idx, req)
	}
	if idx < len(c.responses) {
		return c.responses[idx], nil
	}
	return nil, fmt.Errorf("unexpected call %d", idx)
}

func TestFork_InheritsSettings(t *testing.T) {
	mock := &mockCompleter{}
	echoTool := newMockTool("echo", "Echoes")

	parent := mustNew(mock,
		WithTools(echoTool),
		WithTokenLimit(4096),
		WithMaxTurns(5),
		WithDelegateEnabled(true),
	)

	child := parent.Fork()

	// Completer は共有
	if child.completer != parent.completer {
		t.Error("child should share parent's completer")
	}
	// tokenLimit は継承
	if child.ctxManager.TokenLimit() != 4096 {
		t.Errorf("child tokenLimit = %d, want 4096", child.ctxManager.TokenLimit())
	}
	// maxTurns は継承
	if child.maxTurns != 5 {
		t.Errorf("child maxTurns = %d, want 5", child.maxTurns)
	}
	// ツールは継承
	if child.registry.Len() != 1 {
		t.Errorf("child tools = %d, want 1", child.registry.Len())
	}
	// delegate は無効
	if child.delegateEnabled {
		t.Error("child should have delegate disabled")
	}
}

func TestFork_IndependentContext(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("parent response", llm.Usage{}),
		},
	}
	parent := mustNew(mock)

	// 親にメッセージを追加
	parent.ctxManager.Add(llm.Message{Role: "user", Content: llm.StringPtr("hello")})
	parent.ctxManager.Add(llm.Message{Role: "assistant", Content: llm.StringPtr("hi")})

	child := parent.Fork()

	// 子のコンテキストは空
	if child.ctxManager.Len() != 0 {
		t.Errorf("child messages = %d, want 0 (clean context)", child.ctxManager.Len())
	}
	// 親のコンテキストは影響を受けない
	if parent.ctxManager.Len() != 2 {
		t.Errorf("parent messages = %d, want 2", parent.ctxManager.Len())
	}
}

func TestFork_OverrideOptions(t *testing.T) {
	mock := &mockCompleter{}
	parent := mustNew(mock, WithMaxTurns(10), WithSystemPrompt("parent prompt"))

	child := parent.Fork(
		WithMaxTurns(3),
		WithSystemPrompt("child prompt"),
	)

	if child.maxTurns != 3 {
		t.Errorf("child maxTurns = %d, want 3", child.maxTurns)
	}
	if child.systemPrompt != "child prompt" {
		t.Errorf("child systemPrompt = %q, want %q", child.systemPrompt, "child prompt")
	}
}

func TestCondenseDelegateResult_Short(t *testing.T) {
	eng := mustNew(&mockCompleter{}, WithDelegateMaxChars(1500))

	result := &Result{
		Response: "short answer",
		Turns:    1,
		Usage:    llm.Usage{TotalTokens: 20},
	}

	condensed := eng.condenseDelegateResult(result)
	if !strings.Contains(condensed, "short answer") {
		t.Errorf("condensed should contain original response, got %q", condensed)
	}
	if !strings.Contains(condensed, "1 turns") {
		t.Errorf("condensed should contain turns info, got %q", condensed)
	}
	if strings.Contains(condensed, "truncated") {
		t.Error("short response should not be truncated")
	}
}

func TestCondenseDelegateResult_Long(t *testing.T) {
	eng := mustNew(&mockCompleter{}, WithDelegateMaxChars(100))

	longText := strings.Repeat("abcde", 200) // 1000 chars
	result := &Result{
		Response: longText,
		Turns:    3,
		Usage:    llm.Usage{TotalTokens: 500},
	}

	condensed := eng.condenseDelegateResult(result)
	if !strings.Contains(condensed, "truncated") {
		t.Error("long response should be truncated")
	}
	if !strings.Contains(condensed, "original: 1000 chars") {
		t.Error("truncation marker should show original length")
	}
	if !strings.Contains(condensed, "3 turns") {
		t.Error("condensed should contain turns info")
	}
}

func TestDelegateStep_WithContext(t *testing.T) {
	// delegate_task に context パラメータが渡される場合
	echoTool := newMockTool("echo", "Echoes")

	var childSystemPrompt string
	trackingMock := &trackingCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"delegate_task","arguments":{"task":"read the file","context":"The file is at /tmp/test.txt"},"reasoning":"need context"}`),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"answer"}`),
			makeResponse("file contents: hello", llm.Usage{TotalTokens: 10}),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResponse("The file says hello", llm.Usage{}),
		},
		onCall: func(idx int, req *llm.ChatRequest) {
			if idx == 1 { // 子ルーターの呼び出し
				childSystemPrompt = req.Messages[0].ContentString()
			}
		},
	}

	eng := mustNew(trackingMock, WithTools(echoTool))
	result, err := eng.Run(context.Background(), "what's in the file?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "The file says hello" {
		t.Errorf("response = %q", result.Response)
	}

	// 子のシステムプロンプトにコンテキストが含まれる
	if !strings.Contains(childSystemPrompt, "/tmp/test.txt") {
		t.Error("child system prompt should contain provided context")
	}
}

func TestDelegateStep_WorktreeMode(t *testing.T) {
	// worktree モードで子 Engine が workDir 付きで生成されることを検証
	repoDir := setupTestRepo(t)

	echoTool := newMockTool("echo", "Echoes")

	var childWorkDir string
	// workDir 付きで実行されるツールを使って workDir を検出する
	workDirTool := &mockTool{
		name:        "check_dir",
		description: "Check working directory",
		parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		executeFunc: func(ctx context.Context, args json.RawMessage) (tool.Result, error) {
			childWorkDir = tool.WorkDirFromContext(ctx)
			return tool.Result{Content: "dir: " + childWorkDir}, nil
		},
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. 親ルーター: delegate_task(worktree)
			chatResponse(`{"tool":"delegate_task","arguments":{"task":"check files","mode":"worktree"},"reasoning":"need isolation"}`),
			// 2. 子ルーター: check_dir
			chatResponse(`{"tool":"check_dir","arguments":{},"reasoning":"check"}`),
			// 3. 子ルーター: none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			// 4. 子 chatStep
			makeResponse("checked", llm.Usage{TotalTokens: 10}),
			// 5. 親ルーター: none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			// 6. 親 chatStep
			makeResponse("final", llm.Usage{}),
		},
	}

	eng := mustNew(mock, WithTools(echoTool, workDirTool), WithWorkDir(repoDir))
	result, err := eng.Run(context.Background(), "test worktree")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "final" {
		t.Errorf("response = %q", result.Response)
	}

	// 子 Engine の workDir が worktree ディレクトリに設定されている
	if childWorkDir == "" {
		t.Error("child workDir should be set in worktree mode")
	}
	if childWorkDir == repoDir {
		t.Error("child workDir should differ from parent repoDir (should be worktree path)")
	}
}

func TestDelegateStep_WorktreeFallback(t *testing.T) {
	// git リポジトリでないディレクトリでは fork にフォールバック
	echoTool := newMockTool("echo", "Echoes")

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. 親ルーター: delegate_task(worktree)
			chatResponse(`{"tool":"delegate_task","arguments":{"task":"do something","mode":"worktree"},"reasoning":"test"}`),
			// worktree 作成失敗 → fork フォールバック
			// 2. 子ルーター: none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"answer"}`),
			// 3. 子 chatStep
			makeResponse("fallback result", llm.Usage{TotalTokens: 10}),
			// 4. 親ルーター: none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			// 5. 親 chatStep
			makeResponse("final fallback", llm.Usage{}),
		},
	}

	// 非 git ディレクトリを workDir に指定
	tmpDir := t.TempDir()
	eng := mustNew(mock, WithTools(echoTool), WithWorkDir(tmpDir))
	result, err := eng.Run(context.Background(), "test fallback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "final fallback" {
		t.Errorf("response = %q", result.Response)
	}
}

func TestDelegateStep_DefaultModeFork(t *testing.T) {
	// mode 未指定時は fork モードとして動作する（既存テストと同等だが明示的にテスト）
	echoTool := newMockTool("echo", "Echoes")

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"delegate_task","arguments":{"task":"simple task"},"reasoning":"test"}`),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"answer"}`),
			makeResponse("fork result", llm.Usage{TotalTokens: 10}),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResponse("final fork", llm.Usage{}),
		},
	}

	eng := mustNew(mock, WithTools(echoTool))
	result, err := eng.Run(context.Background(), "test default mode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "final fork" {
		t.Errorf("response = %q", result.Response)
	}
}

func TestDelegateDisabled_FallsThrough(t *testing.T) {
	// delegateEnabled=false の場合、delegate_task はツール未発見として処理される
	echoTool := newMockTool("echo", "Echoes")

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// ルーターが delegate_task を選択（しかし無効）
			chatResponse(`{"tool":"delegate_task","arguments":{"task":"test"},"reasoning":"test"}`),
			// tool not found → リカバリ
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"recovered"}`),
			makeResponse("direct answer", llm.Usage{}),
		},
	}

	eng := mustNew(mock, WithTools(echoTool), WithDelegateEnabled(false))
	result, err := eng.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// delegate_task は tool not found として処理される
	foundNotFound := false
	for _, msg := range eng.ctxManager.Messages() {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "not found") {
			foundNotFound = true
		}
	}
	if !foundNotFound {
		t.Error("expected 'not found' error when delegate is disabled")
	}
	if result.Response != "direct answer" {
		t.Errorf("response = %q", result.Response)
	}
}
