package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"ai-agent/internal/llm"
)

// delegateArgs は delegate_task のルーター引数。
type delegateArgs struct {
	Task    string `json:"task"`
	Context string `json:"context,omitempty"`
}

// delegateStep はサブエージェントを生成し、タスクを委譲して結果を返す。
// 子 Engine は独立したコンテキストで実行され、結果は凝縮して親の履歴に追加される。
func (e *Engine) delegateStep(ctx context.Context, rr *routerResponse, routerUsage *llm.Usage) (*LoopResult, error) {
	var da delegateArgs
	if err := json.Unmarshal(rr.Arguments, &da); err != nil {
		e.logf("[delegate] 引数パースエラー: %s", err)
		callID := generateCallID()
		e.ctxManager.Add(ToolCallMessage(callID, "delegate_task", rr.Arguments))
		e.ctxManager.Add(ToolResultMessage(callID, fmt.Sprintf("Error: invalid arguments: %s", err)))
		return &LoopResult{
			Kind:   Continue,
			Reason: "delegate_parse_error",
			Usage:  *routerUsage,
		}, nil
	}

	if da.Task == "" {
		callID := generateCallID()
		e.ctxManager.Add(ToolCallMessage(callID, "delegate_task", rr.Arguments))
		e.ctxManager.Add(ToolResultMessage(callID, "Error: task is required"))
		return &LoopResult{
			Kind:   Continue,
			Reason: "delegate_parse_error",
			Usage:  *routerUsage,
		}, nil
	}

	e.logf("[delegate] サブタスクを委譲: %s", da.Task)
	if rr.Reasoning != "" {
		e.logf("[delegate] 理由: %s", rr.Reasoning)
	}

	// 子 Engine を生成（delegate_task 無効でネスト再帰防止）
	child := e.Fork(
		WithSystemPrompt(e.buildDelegateSystemPrompt(da)),
	)

	// 子 Engine を実行（context.Context によるキャンセル伝播）
	result, err := child.Run(ctx, da.Task)

	// 合成メッセージの構築
	callID := generateCallID()
	e.ctxManager.Add(ToolCallMessage(callID, "delegate_task", rr.Arguments))

	var resultContent string
	if err != nil {
		resultContent = fmt.Sprintf("Subtask failed: %s", err.Error())
		e.logf("[delegate] サブタスク失敗: %s", err)
	} else {
		resultContent = e.condenseDelegateResult(result)
		e.logf("[delegate] サブタスク完了 (%d turns, %d tokens)", result.Turns, result.Usage.TotalTokens)
	}
	e.ctxManager.Add(ToolResultMessage(callID, resultContent))

	e.logf("[context] %d/%d tokens (%.0f%%)",
		e.ctxManager.TokenCount(), e.ctxManager.TokenLimit(), e.ctxManager.UsageRatio()*100)

	return &LoopResult{
		Kind:   Continue,
		Reason: "delegate_task",
		Usage:  *routerUsage,
	}, nil
}

// buildDelegateSystemPrompt はサブエージェント用のシステムプロンプトを構築する。
func (e *Engine) buildDelegateSystemPrompt(da delegateArgs) string {
	prompt := "You are a focused assistant working on a specific subtask. " +
		"Complete the task thoroughly and provide a clear, concise result.\n"

	if da.Context != "" {
		prompt += "\n## Context\n" + da.Context + "\n"
	}

	return prompt
}

// condenseDelegateResult はサブエージェントの結果を凝縮する。
// 最大 delegateMaxChars 文字に切り詰め、メタ情報を付加する。
func (e *Engine) condenseDelegateResult(result *Result) string {
	content := result.Response
	maxChars := e.delegateMaxChars

	if maxChars > 0 && len(content) > maxChars {
		content = content[:maxChars] + fmt.Sprintf("\n\n[... truncated, original: %d chars ...]", len(content))
	}

	return fmt.Sprintf("[Subtask result (%d turns, %d tokens)]\n%s",
		result.Turns, result.Usage.TotalTokens, content)
}
