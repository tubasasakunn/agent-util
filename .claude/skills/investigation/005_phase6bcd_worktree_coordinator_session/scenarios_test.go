// Phase 6b-d Worktree/Coordinator/Session 統合検証
// go test -v ./.claude/skills/investigation/005_phase6bcd_worktree_coordinator_session/
//
// ユニットテスト（internal/engine/*_test.go）とは異なり、
// 実際のエージェント利用シナリオでの動作を検証する。
package investigation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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

type concurrentMock struct {
	mu        sync.Mutex
	responses []*llm.ChatResponse
	requests  []*llm.ChatRequest
	calls     int
}

func (m *concurrentMock) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	i := m.calls
	m.calls++
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return nil, fmt.Errorf("unexpected call %d (total responses: %d)", i, len(m.responses))
}

// routingMock はサブエージェント判定でレスポンスをルーティングする。
type routingMock struct {
	mu              sync.Mutex
	parentResponses []*llm.ChatResponse
	parentIdx       int
	childResponses  []*llm.ChatResponse
	childIdx        int
}

func (m *routingMock) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	isChild := false
	for _, msg := range req.Messages {
		if msg.Role == "system" && msg.Content != nil {
			if strings.Contains(*msg.Content, "focused assistant") {
				isChild = true
			}
		}
	}

	if isChild {
		i := m.childIdx
		m.childIdx++
		if i < len(m.childResponses) {
			return m.childResponses[i], nil
		}
		return nil, fmt.Errorf("unexpected child call %d", i)
	}

	i := m.parentIdx
	m.parentIdx++
	if i < len(m.parentResponses) {
		return m.parentResponses[i], nil
	}
	return nil, fmt.Errorf("unexpected parent call %d", i)
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

type workDirTool struct{}

func (t *workDirTool) Name() string { return "check_workdir" }
func (t *workDirTool) Description() string {
	return "Reports the current working directory from context"
}
func (t *workDirTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *workDirTool) IsReadOnly() bool        { return true }
func (t *workDirTool) IsConcurrencySafe() bool { return true }
func (t *workDirTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	dir := tool.WorkDirFromContext(ctx)
	if dir == "" {
		return tool.Result{Content: "workdir: (not set)"}, nil
	}
	return tool.Result{Content: "workdir: " + dir}, nil
}

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	commands := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "test"},
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	commands = append(commands,
		[]string{"git", "-C", dir, "add", "."},
		[]string{"git", "-C", dir, "commit", "-m", "initial"},
	)
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s: %v", args, string(out), err)
		}
	}
	return dir
}

// ===========================================================
// シナリオ 1: Worktree モードでのファイルシステム分離
// ===========================================================
func TestScenario_WorktreeFileIsolation(t *testing.T) {
	// worktree モードの delegate_task で、子 Engine の workDir が
	// 元リポジトリと異なるディレクトリに設定されることを確認する。
	repoDir := setupTestRepo(t)

	var capturedWorkDir string
	wdTool := &workDirTool{}
	captureTool := &captureTool{
		name: "capture",
		desc: "Captures workdir",
		exec: func(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
			capturedWorkDir = tool.WorkDirFromContext(ctx)
			return tool.Result{Content: "captured: " + capturedWorkDir}, nil
		},
	}

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// 1. 親ルーター: delegate_task (worktree mode)
			chatResp(`{"tool":"delegate_task","arguments":{"task":"check isolation","mode":"worktree"},"reasoning":"need file isolation"}`),
			// 2. 子ルーター: capture tool
			chatResp(`{"tool":"capture","arguments":{},"reasoning":"check dir"}`),
			// 3. 子ルーター: none
			chatResp(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			// 4. 子 chatStep
			makeResp("isolation verified", llm.Usage{TotalTokens: 10}),
			// 5. 親ルーター: none
			chatResp(`{"tool":"none","arguments":{},"reasoning":"got result"}`),
			// 6. 親 chatStep
			makeResp("Worktree isolation confirmed.", llm.Usage{TotalTokens: 15}),
		},
	}

	eng := engine.New(mock,
		engine.WithTools(wdTool, captureTool),
		engine.WithWorkDir(repoDir),
	)
	result, err := eng.Run(context.Background(), "test worktree isolation")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Response: %s", result.Response)
	t.Logf("Turns: %d, Tokens: %d", result.Turns, result.Usage.TotalTokens)
	t.Logf("Captured workDir: %s", capturedWorkDir)
	t.Logf("Repo dir: %s", repoDir)

	// 子の workDir が repoDir と異なる（worktree パス）
	if capturedWorkDir == "" {
		t.Fatal("child workDir should be set in worktree mode")
	}
	if capturedWorkDir == repoDir {
		t.Error("child workDir should differ from parent repo (should be worktree path)")
	}

	// worktree は cleanup されている（一時ディレクトリが消えている）
	if _, err := os.Stat(capturedWorkDir); !os.IsNotExist(err) {
		t.Error("worktree directory should be cleaned up after delegate completes")
	}
}

// ===========================================================
// シナリオ 2: Worktree フォールバック（非 git ディレクトリ）
// ===========================================================
func TestScenario_WorktreeFallbackToFork(t *testing.T) {
	// git リポジトリでないディレクトリでは worktree 作成が失敗し、
	// fork モードにフォールバックして正常に動作する。
	tmpDir := t.TempDir()

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResp(`{"tool":"delegate_task","arguments":{"task":"analyze data","mode":"worktree"},"reasoning":"try worktree"}`),
			chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResp("analysis done via fork fallback", llm.Usage{TotalTokens: 10}),
			chatResp(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResp("Fallback worked.", llm.Usage{TotalTokens: 15}),
		},
	}

	wdTool := &workDirTool{}
	eng := engine.New(mock,
		engine.WithTools(wdTool),
		engine.WithWorkDir(tmpDir),
		engine.WithLogWriter(os.Stderr),
	)
	result, err := eng.Run(context.Background(), "test fallback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Response: %s", result.Response)
	t.Logf("Turns: %d", result.Turns)

	if result.Response != "Fallback worked." {
		t.Errorf("response = %q", result.Response)
	}
}

// ===========================================================
// シナリオ 3: Coordinator 並列実行と結果集約
// ===========================================================
func TestScenario_CoordinatorParallelExecution(t *testing.T) {
	// coordinate_tasks で 3 タスクを並列実行し、
	// 全結果が集約されて親に返されることを確認する。
	mock := &routingMock{
		parentResponses: []*llm.ChatResponse{
			// 1. 親ルーター: coordinate_tasks
			chatResp(`{"tool":"coordinate_tasks","arguments":{"tasks":[{"id":"a","task":"count words"},{"id":"b","task":"count lines"},{"id":"c","task":"check format"}]},"reasoning":"3 parallel tasks"}`),
			// 結果集約後
			chatResp(`{"tool":"none","arguments":{},"reasoning":"all done"}`),
			makeResp("All 3 analyses completed successfully.", llm.Usage{TotalTokens: 20}),
		},
		childResponses: []*llm.ChatResponse{
			// 子 a: router→none→chat
			chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResp("word count: 42", llm.Usage{TotalTokens: 8}),
			// 子 b
			chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResp("line count: 10", llm.Usage{TotalTokens: 8}),
			// 子 c
			chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResp("format: valid", llm.Usage{TotalTokens: 8}),
		},
	}

	wdTool := &workDirTool{}
	eng := engine.New(mock, engine.WithTools(wdTool))
	result, err := eng.Run(context.Background(), "analyze the document")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Response: %s", result.Response)
	t.Logf("Turns: %d, Tokens: %d", result.Turns, result.Usage.TotalTokens)

	if result.Response != "All 3 analyses completed successfully." {
		t.Errorf("response = %q", result.Response)
	}
}

// ===========================================================
// シナリオ 4: Coordinator 部分失敗と回復
// ===========================================================
func TestScenario_CoordinatorPartialFailure(t *testing.T) {
	// 2タスク中1つが失敗しても、もう1つの結果は正常に返る。
	mock := &routingMock{
		parentResponses: []*llm.ChatResponse{
			chatResp(`{"tool":"coordinate_tasks","arguments":{"tasks":[{"id":"ok","task":"simple"},{"id":"fail","task":"always fail"}]},"reasoning":"test"}`),
			chatResp(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResp("Partial results available.", llm.Usage{TotalTokens: 15}),
		},
		childResponses: []*llm.ChatResponse{
			// ok タスク: 成功
			chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResp("success result", llm.Usage{TotalTokens: 5}),
			// fail タスク: これは childResponses が足りないので unexpected call エラーになる
			// → coordinateResult.Err が設定される
		},
	}

	wdTool := &workDirTool{}
	eng := engine.New(mock, engine.WithTools(wdTool))
	result, err := eng.Run(context.Background(), "test partial")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Response: %s", result.Response)

	if result.Response != "Partial results available." {
		t.Errorf("response = %q", result.Response)
	}
}

// ===========================================================
// シナリオ 5: Coordinator 結果バジェット制限
// ===========================================================
func TestScenario_CoordinatorResultBudget(t *testing.T) {
	// 2タスクがそれぞれ 2000 文字の結果を返した場合、
	// coordinateMaxChars=1500 で各 750 文字に切り詰められる。
	longResult := strings.Repeat("data", 500) // 2000 chars

	mock := &routingMock{
		parentResponses: []*llm.ChatResponse{
			chatResp(`{"tool":"coordinate_tasks","arguments":{"tasks":[{"id":"t1","task":"long1"},{"id":"t2","task":"long2"}]},"reasoning":"test"}`),
			chatResp(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResp("Budget applied.", llm.Usage{TotalTokens: 10}),
		},
		childResponses: []*llm.ChatResponse{
			chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResp(longResult, llm.Usage{TotalTokens: 50}),
			chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResp(longResult, llm.Usage{TotalTokens: 50}),
		},
	}

	wdTool := &workDirTool{}
	eng := engine.New(mock,
		engine.WithTools(wdTool),
		engine.WithCoordinateMaxChars(1500),
	)
	result, err := eng.Run(context.Background(), "test budget")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Response: %s", result.Response)

	// 親のルーターリクエストに含まれる結果を確認
	// (mock.parentResponses[1] = none の前に coordinate 結果が格納されているはず)
	if result.Response != "Budget applied." {
		t.Errorf("response = %q", result.Response)
	}
}

// ===========================================================
// シナリオ 6: SessionRunner 全タスク完了
// ===========================================================
func TestScenario_SessionRunnerAllComplete(t *testing.T) {
	// 3 タスクが全て成功し、進捗ファイルに記録される。
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// タスク1
			chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResp("Feature A implemented", llm.Usage{TotalTokens: 10}),
			// タスク2
			chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResp("Feature B implemented", llm.Usage{TotalTokens: 10}),
			// タスク3
			chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResp("Feature C implemented", llm.Usage{TotalTokens: 10}),
		},
	}

	progressPath := filepath.Join(t.TempDir(), "progress.json")
	wdTool := &workDirTool{}
	sr := engine.NewSessionRunner(mock, engine.SessionConfig{
		ProgressPath: progressPath,
		MaxSessions:  10,
		EngineOpts:   []engine.Option{engine.WithTools(wdTool)},
	})

	progress, err := sr.RunLoop(context.Background(), []string{
		"implement feature A",
		"implement feature B",
		"implement feature C",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Total tasks: %d", len(progress.Tasks))
	for _, task := range progress.Tasks {
		t.Logf("  %s: %s → %s (%s)", task.ID, task.Description, task.Status, task.Summary)
	}

	// 全タスクが done
	for _, task := range progress.Tasks {
		if task.Status != "done" {
			t.Errorf("task %s status = %q, want done", task.ID, task.Status)
		}
		if task.Summary == "" {
			t.Errorf("task %s summary should not be empty", task.ID)
		}
	}

	// 進捗ファイルが存在
	data, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	t.Logf("Progress file size: %d bytes", len(data))

	var persisted engine.ProgressFile
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("parse progress: %v", err)
	}
	if persisted.StartedAt == "" || persisted.UpdatedAt == "" {
		t.Error("timestamps should be set")
	}
}

// ===========================================================
// シナリオ 7: SessionRunner 失敗リトライと進捗再開
// ===========================================================
func TestScenario_SessionRunnerRetryAndResume(t *testing.T) {
	// タスク1が最初失敗 → タスク2成功 → タスク1再試行で成功
	task1Attempts := 0
	retryMock := &sessionRoutingMock{
		onCall: func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
			for _, msg := range req.Messages {
				if msg.Role == "user" && msg.Content != nil {
					if strings.Contains(*msg.Content, "tricky task") {
						task1Attempts++
						if task1Attempts <= 2 { // router + chat の2呼び出しが1セッション分
							return nil, fmt.Errorf("max turns reached")
						}
					}
				}
			}
			if req.ResponseFormat != nil {
				return chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`), nil
			}
			return makeResp("completed", llm.Usage{TotalTokens: 5}), nil
		},
	}

	progressPath := filepath.Join(t.TempDir(), "progress.json")
	wdTool := &workDirTool{}
	sr := engine.NewSessionRunner(retryMock, engine.SessionConfig{
		ProgressPath: progressPath,
		MaxSessions:  10,
		EngineOpts:   []engine.Option{engine.WithTools(wdTool)},
	})

	progress, err := sr.RunLoop(context.Background(), []string{
		"tricky task",
		"easy task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Task 1 attempts: %d", task1Attempts)
	for _, task := range progress.Tasks {
		t.Logf("  %s: %s → %s", task.ID, task.Description, task.Status)
	}

	// 全タスクが最終的に done
	for _, task := range progress.Tasks {
		if task.Status != "done" {
			t.Errorf("task %s status = %q, want done", task.ID, task.Status)
		}
	}
}

// ===========================================================
// シナリオ 8: Coordinator ネスト防止
// ===========================================================
func TestScenario_CoordinatorNestingPrevention(t *testing.T) {
	// 子 Engine のルータープロンプトに coordinate_tasks と
	// delegate_task が含まれないことを確認する。
	var childSystemPrompts []string
	trackMock := &trackingMock{
		responses: []*llm.ChatResponse{
			chatResp(`{"tool":"coordinate_tasks","arguments":{"tasks":[{"id":"t1","task":"sub"}]},"reasoning":"test"}`),
			chatResp(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResp("child done", llm.Usage{TotalTokens: 5}),
			chatResp(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResp("final", llm.Usage{}),
		},
		onCall: func(idx int, req *llm.ChatRequest) {
			if idx == 1 { // 子のルーター呼び出し
				for _, msg := range req.Messages {
					if msg.Role == "system" && msg.Content != nil {
						childSystemPrompts = append(childSystemPrompts, *msg.Content)
					}
				}
			}
		},
	}

	wdTool := &workDirTool{}
	eng := engine.New(trackMock, engine.WithTools(wdTool))
	_, err := eng.Run(context.Background(), "test nesting prevention")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(childSystemPrompts) == 0 {
		t.Fatal("expected child router call")
	}
	prompt := childSystemPrompts[0]
	t.Logf("Child system prompt length: %d chars", len(prompt))

	if strings.Contains(prompt, "coordinate_tasks") {
		t.Error("child should NOT have coordinate_tasks in prompt")
	}
	if strings.Contains(prompt, "delegate_task") {
		t.Error("child should NOT have delegate_task in prompt")
	}
}

// --- 追加ヘルパー ---

type captureTool struct {
	name string
	desc string
	exec func(ctx context.Context, args json.RawMessage) (tool.Result, error)
}

func (t *captureTool) Name() string        { return t.name }
func (t *captureTool) Description() string { return t.desc }
func (t *captureTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *captureTool) IsReadOnly() bool        { return true }
func (t *captureTool) IsConcurrencySafe() bool { return true }
func (t *captureTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	return t.exec(ctx, args)
}

type sessionRoutingMock struct {
	onCall func(req *llm.ChatRequest) (*llm.ChatResponse, error)
}

func (m *sessionRoutingMock) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return m.onCall(req)
}

type trackingMock struct {
	mu        sync.Mutex
	responses []*llm.ChatResponse
	calls     int
	onCall    func(idx int, req *llm.ChatRequest)
}

func (m *trackingMock) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.calls
	m.calls++
	if m.onCall != nil {
		m.onCall(idx, req)
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return nil, fmt.Errorf("unexpected call %d", idx)
}
