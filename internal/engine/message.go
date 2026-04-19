package engine

import (
	"encoding/json"

	"ai-agent/internal/llm"
)

// UserMessage はユーザーロールのメッセージを生成する。
func UserMessage(content string) llm.Message {
	return llm.Message{Role: "user", Content: llm.StringPtr(content)}
}

// SystemMessage はシステムロールのメッセージを生成する。
func SystemMessage(content string) llm.Message {
	return llm.Message{Role: "system", Content: llm.StringPtr(content)}
}

// AssistantMessage はアシスタントロールのメッセージを生成する。
func AssistantMessage(content string) llm.Message {
	return llm.Message{Role: "assistant", Content: llm.StringPtr(content)}
}

// ToolCallMessage はルーターの出力をアシスタントのtool_calls形式に変換する合成メッセージを生成する。
// arguments はJSONオブジェクトを受け取り、OpenAI互換のJSON文字列形式に変換する。
// OpenAI APIの tool_calls.function.arguments は JSON文字列（"{"key":"val"}"）であり、
// JSONオブジェクト（{"key":"val"}）ではない。
func ToolCallMessage(callID, toolName string, arguments json.RawMessage) llm.Message {
	// json.RawMessage (JSONオブジェクト) → JSON文字列に変換
	// 例: {"message":"hello"} → "{\"message\":\"hello\"}"
	argsStr, err := json.Marshal(string(arguments))
	if err != nil {
		argsStr = arguments
	}
	return llm.Message{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{
			{
				ID:   callID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      toolName,
					Arguments: json.RawMessage(argsStr),
				},
			},
		},
	}
}

// ToolResultMessage はツール実行結果のメッセージを生成する。
func ToolResultMessage(callID string, content string) llm.Message {
	return llm.Message{
		Role:       "tool",
		Content:    llm.StringPtr(content),
		ToolCallID: callID,
	}
}
