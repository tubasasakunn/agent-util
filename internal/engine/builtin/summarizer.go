package builtin

import (
	"context"
	"fmt"
	"strings"

	agentctx "ai-agent/internal/context"
	"ai-agent/internal/llm"
)

// NewLLMSummarizer は LLM を呼び出してメッセージ列を要約する Summarizer を返す。
// model は ChatRequest に渡されるモデル名（空なら Completer 側のデフォルト）。
func NewLLMSummarizer(c llm.Completer, model string) agentctx.Summarizer {
	return func(ctx context.Context, msgs []llm.Message) (string, error) {
		var sb strings.Builder
		sb.WriteString("Summarize the following conversation history concisely (under 500 chars), preserving:\n")
		sb.WriteString("- key decisions and their reasoning\n")
		sb.WriteString("- important file paths, identifiers, and tool results\n")
		sb.WriteString("- unresolved tasks or blockers\n\n")
		sb.WriteString("--- history ---\n")
		for _, m := range msgs {
			sb.WriteString(fmt.Sprintf("[%s] %s\n", m.Role, m.ContentString()))
		}

		req := &llm.ChatRequest{
			Model: model,
			Messages: []llm.Message{
				{Role: "user", Content: llm.StringPtr(sb.String())},
			},
		}
		resp, err := c.ChatCompletion(ctx, req)
		if err != nil {
			return "", fmt.Errorf("llm summarizer: %w", err)
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("llm summarizer: %w", llm.ErrEmptyResponse)
		}
		return strings.TrimSpace(resp.Choices[0].Message.ContentString()), nil
	}
}
