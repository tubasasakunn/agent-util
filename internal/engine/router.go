package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	var sb strings.Builder
	sb.WriteString("### delegate_task\n")
	sb.WriteString("Delegates a subtask to a separate agent with its own context window. ")
	sb.WriteString("Use when the current task is too complex or requires extensive information gathering ")
	sb.WriteString("that would consume too much context.\n")
	sb.WriteString("Parameters:\n```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"type\": \"object\",\n")
	sb.WriteString("  \"properties\": {\n")
	sb.WriteString("    \"task\": {\n")
	sb.WriteString("      \"type\": \"string\",\n")
	sb.WriteString("      \"description\": \"Clear, specific description of the subtask to perform\"\n")
	sb.WriteString("    },\n")
	sb.WriteString("    \"context\": {\n")
	sb.WriteString("      \"type\": \"string\",\n")
	sb.WriteString("      \"description\": \"Relevant context the subtask needs to know\"\n")
	sb.WriteString("    },\n")
	sb.WriteString("    \"mode\": {\n")
	sb.WriteString("      \"type\": \"string\",\n")
	sb.WriteString("      \"description\": \"Execution mode: fork (default, shared filesystem) or worktree (isolated filesystem via git worktree)\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  },\n")
	sb.WriteString("  \"required\": [\"task\"]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
	return sb.String()
}

// coordinatorToolDef は coordinate_tasks のツール定義テキストを返す。
func coordinatorToolDef() string {
	var sb strings.Builder
	sb.WriteString("### coordinate_tasks\n")
	sb.WriteString("Run multiple subtasks in parallel, each in its own context window. ")
	sb.WriteString("Use when several independent subtasks can be executed concurrently.\n")
	sb.WriteString("Parameters:\n```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"type\": \"object\",\n")
	sb.WriteString("  \"properties\": {\n")
	sb.WriteString("    \"tasks\": {\n")
	sb.WriteString("      \"type\": \"array\",\n")
	sb.WriteString("      \"items\": {\n")
	sb.WriteString("        \"type\": \"object\",\n")
	sb.WriteString("        \"properties\": {\n")
	sb.WriteString("          \"id\": {\"type\": \"string\", \"description\": \"Unique task identifier\"},\n")
	sb.WriteString("          \"task\": {\"type\": \"string\", \"description\": \"Task description\"}\n")
	sb.WriteString("        },\n")
	sb.WriteString("        \"required\": [\"id\", \"task\"]\n")
	sb.WriteString("      }\n")
	sb.WriteString("    }\n")
	sb.WriteString("  },\n")
	sb.WriteString("  \"required\": [\"tasks\"]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
	return sb.String()
}

// routerInstructions はルーターの指示テキストを返す。
func routerInstructions() string {
	var sb strings.Builder
	sb.WriteString("## Instructions\n\n")
	sb.WriteString("Based on the user's request and conversation history, select the most appropriate tool to use.\n\n")
	sb.WriteString("Select tool \"none\" when:\n")
	sb.WriteString("- You already have enough information to answer (e.g., tool results are already in the conversation)\n")
	sb.WriteString("- The question can be answered directly without any tool\n")
	sb.WriteString("- You need to summarize, explain, or respond based on previous tool results\n\n")
	sb.WriteString("IMPORTANT: Do NOT use a tool to deliver your answer. Tools are for gathering information only. ")
	sb.WriteString("When you have the information needed, select \"none\" and the system will generate the response.\n\n")
	sb.WriteString("You MUST respond with a JSON object in this exact format:\n")
	sb.WriteString("{\"tool\": \"<tool_name or none>\", \"arguments\": {<tool arguments>}, \"reasoning\": \"<brief explanation>\"}\n\n")
	sb.WriteString("Respond with valid JSON only.\n")
	return sb.String()
}
