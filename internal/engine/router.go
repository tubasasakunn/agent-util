package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"ai-agent/internal/llm"
)

// routerResponse はルーターのJSON mode出力をマッピングする構造体。
type routerResponse struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Reasoning string          `json:"reasoning"`
}

// routerStep はルーターステップを実行し、ツール選択結果を返す。
// ルーターはJSON modeでLLMを呼び出し、どのツールを使うか（または使わないか）を判断する。
func (e *Engine) routerStep(ctx context.Context) (*routerResponse, *llm.Usage, error) {
	msgs := e.buildRouterMessages()
	req := &llm.ChatRequest{
		Messages:       msgs,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}

	resp, err := e.complete(ctx, req, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("router step: %w", err)
	}

	var rr routerResponse
	if err := llm.ParseContent(resp, &rr); err != nil {
		return nil, &resp.Usage, &RouterParseError{Cause: err}
	}

	if rr.Tool == "" {
		rr.Tool = "none"
	}

	return &rr, &resp.Usage, nil
}

// buildRouterMessages はルーター用のメッセージリストを構築する。
func (e *Engine) buildRouterMessages() []llm.Message {
	routerSys := e.promptBuilder.BuildRouterSystemPrompt()
	history := e.ctxManager.Messages()
	msgs := make([]llm.Message, 0, len(history)+1)
	msgs = append(msgs, SystemMessage(routerSys))
	msgs = append(msgs, history...)
	return msgs
}

// delegateToolDef は delegate_task のツール定義テキストを返す。
func delegateToolDef() string {
	return `### delegate_task
Delegates a subtask to a separate agent with its own context window. ` +
		`Use when the current task is too complex or requires extensive information gathering ` +
		`that would consume too much context.
Parameters:
` + "```json" + `
{
  "type": "object",
  "properties": {
    "task": {
      "type": "string",
      "description": "Clear, specific description of the subtask to perform"
    },
    "context": {
      "type": "string",
      "description": "Relevant context the subtask needs to know"
    },
    "mode": {
      "type": "string",
      "description": "Execution mode: fork (default, shared filesystem) or worktree (isolated filesystem via git worktree)"
    }
  },
  "required": ["task"]
}
` + "```\n\n"
}

// coordinatorToolDef は coordinate_tasks のツール定義テキストを返す。
func coordinatorToolDef() string {
	return `### coordinate_tasks
Run multiple subtasks in parallel, each in its own context window. ` +
		`Use when several independent subtasks can be executed concurrently.
Parameters:
` + "```json" + `
{
  "type": "object",
  "properties": {
    "tasks": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "string", "description": "Unique task identifier"},
          "task": {"type": "string", "description": "Task description"}
        },
        "required": ["id", "task"]
      }
    }
  },
  "required": ["tasks"]
}
` + "```\n\n"
}

// routerInstructions はルーターの指示テキストを返す。
func routerInstructions() string {
	return `## Instructions

Based on the user's request and conversation history, select the most appropriate tool to use.

Select tool "none" when:
- You already have enough information to answer (e.g., tool results are already in the conversation)
- The question can be answered directly without any tool
- You need to summarize, explain, or respond based on previous tool results

IMPORTANT: Do NOT use a tool to deliver your answer. Tools are for gathering information only. When you have the information needed, select "none" and the system will generate the response.

You MUST respond with a JSON object in this exact format:
{"tool": "<tool_name or none>", "arguments": {<tool arguments>}, "reasoning": "<brief explanation>"}

Respond with valid JSON only.
`
}
