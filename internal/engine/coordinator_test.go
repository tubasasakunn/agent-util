package engine

import (
	"context"
	"strings"
	"testing"

	"ai-agent/internal/llm"
)

func TestCoordinateStep_BasicParallel(t *testing.T) {
	// 2つのタスクを並列実行し、結果が集約される
	echoTool := newMockTool("echo", "Echoes")

	// 並列実行されるため順序不定 — 十分なレスポンスを用意する
	// 親: router → coordinate_tasks → [子1: router→chat, 子2: router→chat] → router→chat
	mock := &concurrentMockCompleter{
		responses: []*llm.ChatResponse{
			// 1. 親ルーター: coordinate_tasks
			chatResponse(`{"tool":"coordinate_tasks","arguments":{"tasks":[{"id":"t1","task":"task one"},{"id":"t2","task":"task two"}]},"reasoning":"parallel"}`),
			// 2. 子1 ルーター: none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"direct"}`),
			// 3. 子1 chatStep
			makeResponse("result one", llm.Usage{TotalTokens: 10}),
			// 4. 子2 ルーター: none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"direct"}`),
			// 5. 子2 chatStep
			makeResponse("result two", llm.Usage{TotalTokens: 10}),
			// 6. 親ルーター: none
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			// 7. 親 chatStep
			makeResponse("Both tasks completed.", llm.Usage{}),
		},
	}

	eng := New(mock, WithTools(echoTool))
	result, err := eng.Run(context.Background(), "run two tasks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "Both tasks completed." {
		t.Errorf("response = %q", result.Response)
	}

	// 親の履歴に coordinate_tasks の結果が含まれる
	found := false
	for _, msg := range eng.ctxManager.Messages() {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "Coordinated results: 2 tasks") {
			found = true
			content := msg.ContentString()
			if !strings.Contains(content, "Task t1") {
				t.Error("aggregate should contain Task t1")
			}
			if !strings.Contains(content, "Task t2") {
				t.Error("aggregate should contain Task t2")
			}
		}
	}
	if !found {
		t.Error("expected coordinated results in parent history")
	}
}

func TestCoordinateStep_PartialFailure(t *testing.T) {
	// 1タスク成功、1タスク失敗
	// 並列実行のためルーティングmockを使用（ユーザーメッセージ内容で応答を分岐）
	echoTool := newMockTool("echo", "Echoes")

	mock := &routingMockCompleter{
		// 親の呼び出し（coordinate_tasks 選択 → 最終応答）
		parentResponses: []*llm.ChatResponse{
			chatResponse(`{"tool":"coordinate_tasks","arguments":{"tasks":[{"id":"ok","task":"succeed"},{"id":"fail","task":"always fail"}]},"reasoning":"test"}`),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResponse("partial result", llm.Usage{}),
		},
		// "succeed" タスクは成功
		childSuccessResponses: []*llm.ChatResponse{
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResponse("success", llm.Usage{TotalTokens: 5}),
		},
		// "always fail" タスクはエラーを返す
		childFailErr: ErrMaxTurnsReached,
	}

	eng := New(mock, WithTools(echoTool))
	result, err := eng.Run(context.Background(), "run two tasks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 結果に成功と失敗の両方が含まれる
	for _, msg := range eng.ctxManager.Messages() {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "Coordinated results") {
			content := msg.ContentString()
			if !strings.Contains(content, "Task ok") {
				t.Error("aggregate should contain successful task")
			}
			if !strings.Contains(content, "Task fail: FAILED") {
				t.Error("aggregate should contain failed task")
			}
		}
	}
	if result.Response != "partial result" {
		t.Errorf("response = %q", result.Response)
	}
}

func TestCoordinateStep_EmptyTasks(t *testing.T) {
	echoTool := newMockTool("echo", "Echoes")

	mock := &concurrentMockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"coordinate_tasks","arguments":{"tasks":[]},"reasoning":"test"}`),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"recovered"}`),
			makeResponse("recovered", llm.Usage{}),
		},
	}

	eng := New(mock, WithTools(echoTool))
	result, err := eng.Run(context.Background(), "test empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 空配列エラーがツール結果に含まれる
	foundError := false
	for _, msg := range eng.ctxManager.Messages() {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "must not be empty") {
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected empty tasks error in tool result")
	}
	if result.Response != "recovered" {
		t.Errorf("response = %q", result.Response)
	}
}

func TestCoordinateStep_ContextCancellation(t *testing.T) {
	echoTool := newMockTool("echo", "Echoes")
	ctx, cancel := context.WithCancel(context.Background())

	mock := &cancellingCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"coordinate_tasks","arguments":{"tasks":[{"id":"t1","task":"long task"}]},"reasoning":"test"}`),
			// 子の router 呼び出し時にキャンセル
		},
		cancelFunc: func(idx int) {
			if idx == 1 {
				cancel()
			}
		},
	}

	eng := New(mock, WithTools(echoTool))
	_, err := eng.Run(ctx, "test cancel")

	if err == nil {
		t.Fatal("expected error from cancellation")
	}
}

func TestCoordinateStep_NestingPrevention(t *testing.T) {
	// 子 Engine のルータープロンプトに coordinate_tasks が含まれないことを確認
	echoTool := newMockTool("echo", "Echoes")

	var childPrompts []string
	mock := &trackingCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"coordinate_tasks","arguments":{"tasks":[{"id":"t1","task":"sub"}]},"reasoning":"test"}`),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResponse("child result", llm.Usage{TotalTokens: 5}),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResponse("final", llm.Usage{}),
		},
		onCall: func(idx int, req *llm.ChatRequest) {
			if idx == 1 { // 子のルーター呼び出し
				childPrompts = append(childPrompts, req.Messages[0].ContentString())
			}
		},
	}

	eng := New(mock, WithTools(echoTool))
	_, err := eng.Run(context.Background(), "test nesting")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(childPrompts) == 0 {
		t.Fatal("expected child router call")
	}
	if strings.Contains(childPrompts[0], "coordinate_tasks") {
		t.Error("child engine should NOT have coordinate_tasks in router prompt")
	}
}

func TestCoordinateStep_ResultBudget(t *testing.T) {
	echoTool := newMockTool("echo", "Echoes")
	longResult := strings.Repeat("x", 3000)

	mock := &concurrentMockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"coordinate_tasks","arguments":{"tasks":[{"id":"t1","task":"long1"},{"id":"t2","task":"long2"}]},"reasoning":"test"}`),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResponse(longResult, llm.Usage{TotalTokens: 10}),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"ok"}`),
			makeResponse(longResult, llm.Usage{TotalTokens: 10}),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			makeResponse("done", llm.Usage{}),
		},
	}

	// coordinateMaxChars=2000, 2タスク → 各1000文字に制限
	eng := New(mock, WithTools(echoTool), WithCoordinateMaxChars(2000))
	_, err := eng.Run(context.Background(), "long tasks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, msg := range eng.ctxManager.Messages() {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "Coordinated results") {
			content := msg.ContentString()
			if !strings.Contains(content, "truncated") {
				t.Error("long results should be truncated")
			}
			if !strings.Contains(content, "original: 3000 chars") {
				t.Error("truncation marker should show original length")
			}
		}
	}
}

func TestCoordinateStep_InvalidArgs(t *testing.T) {
	echoTool := newMockTool("echo", "Echoes")

	mock := &concurrentMockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"coordinate_tasks","arguments":{"invalid":true},"reasoning":"test"}`),
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"recovered"}`),
			makeResponse("recovered", llm.Usage{}),
		},
	}

	eng := New(mock, WithTools(echoTool))
	result, err := eng.Run(context.Background(), "test invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundError := false
	for _, msg := range eng.ctxManager.Messages() {
		if msg.Role == "tool" && strings.Contains(msg.ContentString(), "must not be empty") {
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected error for empty tasks (JSON decoded but tasks array is nil/empty)")
	}
	if result.Response != "recovered" {
		t.Errorf("response = %q", result.Response)
	}
}
