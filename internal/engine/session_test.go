package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-agent/internal/llm"
)

func TestSessionRunner_AllTasksComplete(t *testing.T) {
	// 3タスクが全て成功して終了する
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// タスク1: router→none→chat
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"direct"}`),
			makeResponse("task 1 done", llm.Usage{TotalTokens: 10}),
			// タスク2
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"direct"}`),
			makeResponse("task 2 done", llm.Usage{TotalTokens: 10}),
			// タスク3
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"direct"}`),
			makeResponse("task 3 done", llm.Usage{TotalTokens: 10}),
		},
	}

	progressPath := filepath.Join(t.TempDir(), "progress.json")
	echoTool := newMockTool("echo", "Echoes")
	sr := NewSessionRunner(mock, SessionConfig{
		ProgressPath: progressPath,
		MaxSessions:  10,
		EngineOpts:   []Option{WithTools(echoTool)},
	})

	progress, err := sr.RunLoop(context.Background(), []string{
		"implement feature A",
		"implement feature B",
		"implement feature C",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 全タスクが done
	for _, task := range progress.Tasks {
		if task.Status != "done" {
			t.Errorf("task %s status = %q, want done", task.ID, task.Status)
		}
	}

	// 進捗ファイルが存在する
	if _, err := os.Stat(progressPath); os.IsNotExist(err) {
		t.Error("progress file should exist")
	}
}

func TestSessionRunner_TaskFailureRetry(t *testing.T) {
	// タスク1が失敗 → タスク2成功 → タスク1が再試行で成功
	callCount := 0
	mock := &mockCompleter{}
	mock.responses = nil

	// カスタム Completer: 最初のタスク1呼び出しはエラー、2回目は成功
	task1Calls := 0
	retryMock := &routingSessionMock{
		onCall: func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
			callCount++
			// ユーザーメッセージからタスクを判定
			for _, msg := range req.Messages {
				if msg.Role == "user" && msg.Content != nil {
					if strings.Contains(*msg.Content, "fail first") {
						task1Calls++
						if task1Calls <= 2 {
							// 1回目（router + chat）は ErrMaxTurnsReached を返す
							// ただしここでは簡単にエラーを返す
							return nil, ErrMaxTurnsReached
						}
						// 2回目以降は成功
					}
				}
			}
			// JSON mode (router) なら none を返す
			if req.ResponseFormat != nil {
				return chatResponse(`{"tool":"none","arguments":{},"reasoning":"ok"}`), nil
			}
			return makeResponse("done", llm.Usage{TotalTokens: 5}), nil
		},
	}

	progressPath := filepath.Join(t.TempDir(), "progress.json")
	sr := NewSessionRunner(retryMock, SessionConfig{
		ProgressPath: progressPath,
		MaxSessions:  10,
		EngineOpts:   []Option{WithTools(newMockTool("echo", "Echoes"))},
	})

	progress, err := sr.RunLoop(context.Background(), []string{
		"fail first time",
		"always succeed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 全タスクが done（タスク1は再試行で成功）
	for _, task := range progress.Tasks {
		if task.Status != "done" {
			t.Errorf("task %s (%s) status = %q, want done", task.ID, task.Description, task.Status)
		}
	}
}

func TestSessionRunner_MaxSessionsReached(t *testing.T) {
	// 全タスクが常に失敗し、maxSessions に達する
	alwaysFailMock := &routingSessionMock{
		onCall: func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
			return nil, ErrMaxTurnsReached
		},
	}

	progressPath := filepath.Join(t.TempDir(), "progress.json")
	sr := NewSessionRunner(alwaysFailMock, SessionConfig{
		ProgressPath: progressPath,
		MaxSessions:  3,
		EngineOpts:   []Option{WithTools(newMockTool("echo", "Echoes"))},
	})

	_, err := sr.RunLoop(context.Background(), []string{"impossible task"})
	if err == nil {
		t.Fatal("expected error for max sessions reached")
	}
	if !strings.Contains(err.Error(), "max sessions") {
		t.Errorf("error = %q, want 'max sessions' message", err)
	}
}

func TestSessionRunner_ProgressResumption(t *testing.T) {
	// 既存の進捗ファイルから再開する
	progressPath := filepath.Join(t.TempDir(), "progress.json")

	// タスク1=done, タスク2=pending の進捗ファイルを作成
	existing := ProgressFile{
		Tasks: []TaskEntry{
			{ID: "task-1", Description: "already done", Status: "done", Summary: "completed"},
			{ID: "task-2", Description: "still pending", Status: "pending"},
		},
		StartedAt: "2026-04-19T00:00:00Z",
		UpdatedAt: "2026-04-19T00:00:00Z",
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(progressPath, data, 0o644)

	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// タスク2 のみ実行
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResponse("task 2 done", llm.Usage{TotalTokens: 5}),
		},
	}

	sr := NewSessionRunner(mock, SessionConfig{
		ProgressPath: progressPath,
		MaxSessions:  10,
		EngineOpts:   []Option{WithTools(newMockTool("echo", "Echoes"))},
	})

	progress, err := sr.RunLoop(context.Background(), []string{"ignored"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// タスク1は既に done、タスク2も done
	if progress.Tasks[0].Status != "done" {
		t.Errorf("task-1 status = %q, want done", progress.Tasks[0].Status)
	}
	if progress.Tasks[1].Status != "done" {
		t.Errorf("task-2 status = %q, want done", progress.Tasks[1].Status)
	}
	// mock は 2回しか呼ばれない（タスク2 の router + chat のみ）
	if mock.calls != 2 {
		t.Errorf("mock calls = %d, want 2 (only task-2)", mock.calls)
	}
}

func TestSessionRunner_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mock := &cancellingCompleter{
		responses: []*llm.ChatResponse{},
		cancelFunc: func(idx int) {
			cancel()
		},
	}

	progressPath := filepath.Join(t.TempDir(), "progress.json")
	sr := NewSessionRunner(mock, SessionConfig{
		ProgressPath: progressPath,
		MaxSessions:  10,
		EngineOpts:   []Option{WithTools(newMockTool("echo", "Echoes"))},
	})

	_, err := sr.RunLoop(ctx, []string{"task"})
	if err == nil {
		t.Fatal("expected error from cancellation")
	}
}

func TestSessionRunner_BuildSessionPrompt(t *testing.T) {
	sr := &SessionRunner{}
	progress := &ProgressFile{
		Tasks: []TaskEntry{
			{ID: "task-1", Description: "feature A", Status: "done", Summary: "completed"},
			{ID: "task-2", Description: "feature B", Status: "in_progress"},
			{ID: "task-3", Description: "feature C", Status: "pending"},
			{ID: "task-4", Description: "feature D", Status: "failed", Summary: "error occurred"},
		},
	}
	currentTask := &progress.Tasks[1]

	prompt := sr.buildSessionPrompt(currentTask, progress)

	if !strings.Contains(prompt, "feature B") {
		t.Error("prompt should contain current task description")
	}
	if !strings.Contains(prompt, "[x] task-1") {
		t.Error("prompt should show done task with [x]")
	}
	if !strings.Contains(prompt, "[>] task-2") {
		t.Error("prompt should show current task with [>]")
	}
	if !strings.Contains(prompt, "[ ] task-3") {
		t.Error("prompt should show pending task with [ ]")
	}
	if !strings.Contains(prompt, "[!] task-4") {
		t.Error("prompt should show failed task with [!]")
	}
	if !strings.Contains(prompt, "completed") {
		t.Error("prompt should include done task summary")
	}
}

func TestSessionRunner_ProgressFilePersistence(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResponse("result", llm.Usage{TotalTokens: 5}),
		},
	}

	progressPath := filepath.Join(t.TempDir(), "progress.json")
	sr := NewSessionRunner(mock, SessionConfig{
		ProgressPath: progressPath,
		MaxSessions:  10,
		EngineOpts:   []Option{WithTools(newMockTool("echo", "Echoes"))},
	})

	_, err := sr.RunLoop(context.Background(), []string{"single task"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 進捗ファイルを読み直して内容を検証
	data, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("read progress file: %v", err)
	}
	var progress ProgressFile
	if err := json.Unmarshal(data, &progress); err != nil {
		t.Fatalf("parse progress file: %v", err)
	}
	if len(progress.Tasks) != 1 {
		t.Fatalf("tasks count = %d, want 1", len(progress.Tasks))
	}
	if progress.Tasks[0].Status != "done" {
		t.Errorf("task status = %q, want done", progress.Tasks[0].Status)
	}
	if progress.Tasks[0].Summary != "result" {
		t.Errorf("task summary = %q, want result", progress.Tasks[0].Summary)
	}
	if progress.StartedAt == "" {
		t.Error("started_at should be set")
	}
	if progress.UpdatedAt == "" {
		t.Error("updated_at should be set")
	}
}

// routingSessionMock はセッションテスト用のカスタム Completer。
type routingSessionMock struct {
	onCall func(req *llm.ChatRequest) (*llm.ChatResponse, error)
}

func (m *routingSessionMock) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return m.onCall(req)
}
