package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"ai-agent/internal/llm"
	"ai-agent/pkg/tool"
)

// mockCompleter は Completer インターフェースのテスト用実装。
// responses に積んだ順にレスポンスを返す。
type mockCompleter struct {
	responses []*llm.ChatResponse
	requests  []*llm.ChatRequest
	calls     int
	err       error // 全呼び出しで返すエラー
}

func (m *mockCompleter) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.requests = append(m.requests, req)
	if m.err != nil {
		return nil, m.err
	}
	i := m.calls
	m.calls++
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return nil, fmt.Errorf("unexpected call %d", i)
}

// makeResponse はテスト用のChatResponseを生成する。
func makeResponse(content string, usage llm.Usage) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{
				Message:      llm.Message{Role: "assistant", Content: llm.StringPtr(content)},
				FinishReason: "stop",
			},
		},
		Usage: usage,
	}
}

// chatResponse はcontent付きのChatResponseを生成する（Usage付き）。
func chatResponse(content string) *llm.ChatResponse {
	return makeResponse(content, llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
}

// toolCallResponse はtool_calls付きのChatResponseを生成する。
func toolCallResponse(callID, toolName string, args json.RawMessage) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{
				Message: llm.Message{
					Role: "assistant",
					ToolCalls: []llm.ToolCall{
						{
							ID:   callID,
							Type: "function",
							Function: llm.FunctionCall{
								Name:      toolName,
								Arguments: args,
							},
						},
					},
				},
				FinishReason: "tool_calls",
			},
		},
		Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

// mockTool はテスト用のTool実装。
type mockTool struct {
	name        string
	description string
	parameters  json.RawMessage
	readOnly    bool
	executeFunc func(ctx context.Context, args json.RawMessage) (tool.Result, error)
}

func (m *mockTool) Name() string                { return m.name }
func (m *mockTool) Description() string          { return m.description }
func (m *mockTool) Parameters() json.RawMessage  { return m.parameters }
func (m *mockTool) IsReadOnly() bool             { return m.readOnly }
func (m *mockTool) IsConcurrencySafe() bool      { return false }
func (m *mockTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, args)
	}
	return tool.Result{Content: "mock result"}, nil
}

func newMockTool(name, desc string) *mockTool {
	return &mockTool{
		name:        name,
		description: desc,
		parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
	}
}
