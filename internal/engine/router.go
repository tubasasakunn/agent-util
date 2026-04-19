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

	resp, err := e.completer.ChatCompletion(ctx, &llm.ChatRequest{
		Messages:       msgs,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("router step: %w", err)
	}

	var rr routerResponse
	if err := llm.ParseContent(resp, &rr); err != nil {
		return nil, &resp.Usage, fmt.Errorf("router parse: %w", err)
	}

	if rr.Tool == "" {
		rr.Tool = "none"
	}

	return &rr, &resp.Usage, nil
}

// buildRouterMessages はルーター用のメッセージリストを構築する。
func (e *Engine) buildRouterMessages() []llm.Message {
	routerSys := e.routerSystemPrompt()
	history := e.ctxManager.Messages()
	msgs := make([]llm.Message, 0, len(history)+1)
	msgs = append(msgs, SystemMessage(routerSys))
	msgs = append(msgs, history...)
	return msgs
}

// routerSystemPrompt はルーター用のシステムプロンプトを構築する。
func (e *Engine) routerSystemPrompt() string {
	var sb strings.Builder

	if e.systemPrompt != "" {
		sb.WriteString(e.systemPrompt)
		sb.WriteString("\n\n")
	}

	sb.WriteString(e.registry.FormatForPrompt())

	if e.delegateEnabled {
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
		sb.WriteString("    }\n")
		sb.WriteString("  },\n")
		sb.WriteString("  \"required\": [\"task\"]\n")
		sb.WriteString("}\n")
		sb.WriteString("```\n\n")
	}

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
