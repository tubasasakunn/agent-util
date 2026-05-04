package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"ai-agent/internal/llm"
)

// coordinateArgs は coordinate_tasks のルーター引数。
type coordinateArgs struct {
	Tasks []coordinateTask `json:"tasks"`
}

// coordinateTask は並列実行する個別タスク。
type coordinateTask struct {
	ID   string `json:"id"`
	Task string `json:"task"`
}

// coordinateResult は1つのサブタスクの実行結果。
type coordinateResult struct {
	ID     string
	Result *Result
	Err    error
}

// coordinateStep は複数のサブタスクを並列実行し、結果を集約して親の履歴に追加する。
func (e *Engine) coordinateStep(ctx context.Context, rr *routerResponse, routerUsage *llm.Usage) (*LoopResult, error) {
	var ca coordinateArgs
	if err := json.Unmarshal(rr.Arguments, &ca); err != nil {
		e.logf("[coordinate] 引数パースエラー: %s", err)
		callID := generateCallID()
		e.ctxManager.Add(ToolCallMessage(callID, "coordinate_tasks", rr.Arguments))
		e.ctxManager.Add(ToolResultMessage(callID, fmt.Sprintf("Error: invalid arguments: %s", err)))
		return &LoopResult{
			Kind:   Continue,
			Reason: "coordinate_parse_error",
			Usage:  *routerUsage,
		}, nil
	}

	if len(ca.Tasks) == 0 {
		callID := generateCallID()
		e.ctxManager.Add(ToolCallMessage(callID, "coordinate_tasks", rr.Arguments))
		e.ctxManager.Add(ToolResultMessage(callID, "Error: tasks array is required and must not be empty"))
		return &LoopResult{
			Kind:   Continue,
			Reason: "coordinate_parse_error",
			Usage:  *routerUsage,
		}, nil
	}

	e.logf("[coordinate] %d タスクを並列実行", len(ca.Tasks))
	if rr.Reasoning != "" {
		e.logf("[coordinate] 理由: %s", rr.Reasoning)
	}

	results := e.runParallel(ctx, ca.Tasks)
	content := e.aggregateResults(results)

	callID := generateCallID()
	e.ctxManager.Add(ToolCallMessage(callID, "coordinate_tasks", rr.Arguments))
	e.ctxManager.Add(ToolResultMessage(callID, content))

	e.logf("[context] %d/%d tokens (%.0f%%)",
		e.ctxManager.TokenCount(), e.ctxManager.TokenLimit(), e.ctxManager.UsageRatio()*100)

	return &LoopResult{
		Kind:   Continue,
		Reason: "coordinate_tasks",
		Usage:  *routerUsage,
	}, nil
}

// runParallel は複数タスクを並列に実行する。
// 各タスクは独立した子 Engine で実行される。
// 同時実行数は coordinatorMaxParallelism で上限を設ける（デフォルト 10）。
func (e *Engine) runParallel(ctx context.Context, tasks []coordinateTask) []coordinateResult {
	results := make([]coordinateResult, len(tasks))
	var wg sync.WaitGroup

	concurrency := e.coordinatorMaxParallelism
	if concurrency <= 0 {
		concurrency = defaultCoordinatorMaxParallelism
	}
	if concurrency > len(tasks) {
		concurrency = len(tasks)
	}
	sem := make(chan struct{}, concurrency)

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t coordinateTask) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			da := delegateArgs{Task: t.Task}
			child := e.Fork(
				WithSystemPrompt(e.buildDelegateSystemPrompt(da)),
			)

			result, err := child.Run(ctx, t.Task)
			results[idx] = coordinateResult{
				ID:     t.ID,
				Result: result,
				Err:    err,
			}
		}(i, task)
	}

	wg.Wait()
	return results
}

// aggregateResults は並列タスクの結果を集約して1つの文字列にする。
// 各タスクの結果は coordinateMaxChars / タスク数 に制限される。
func (e *Engine) aggregateResults(results []coordinateResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Coordinated results: %d tasks]\n", len(results)))

	perTaskBudget := 0
	if e.coordinateMaxChars > 0 && len(results) > 0 {
		perTaskBudget = e.coordinateMaxChars / len(results)
	}

	for _, r := range results {
		if r.Err != nil {
			sb.WriteString(fmt.Sprintf("\n--- Task %s: FAILED ---\n%s\n", r.ID, r.Err))
			continue
		}

		// 個別タスクの結果を凝縮（バジェット制限付き）
		content := r.Result.Response
		if perTaskBudget > 0 && len(content) > perTaskBudget {
			content = content[:perTaskBudget] + fmt.Sprintf("\n[... truncated, original: %d chars ...]", len(content))
		}

		sb.WriteString(fmt.Sprintf("\n--- Task %s (%d turns, %d tokens) ---\n%s\n",
			r.ID, r.Result.Turns, r.Result.Usage.TotalTokens, content))
	}

	return sb.String()
}
