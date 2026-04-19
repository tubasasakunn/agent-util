package engine

import "ai-agent/internal/llm"

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
