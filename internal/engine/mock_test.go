package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

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

// concurrentMockCompleter はスレッドセーフな mockCompleter。
// Coordinator テストなど並列呼び出し時に使用する。
type concurrentMockCompleter struct {
	mu        sync.Mutex
	responses []*llm.ChatResponse
	calls     int
	err       error
}

func (m *concurrentMockCompleter) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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

// routingMockCompleter は並列テスト用のルーティングmock。
// ユーザーメッセージに "always fail" を含む子タスクにはエラーを返し、
// それ以外の子タスクには成功レスポンスを返す。
// 親の呼び出し（systemPromptベースでない呼び出し）には parentResponses を順に返す。
type routingMockCompleter struct {
	mu                    sync.Mutex
	parentResponses       []*llm.ChatResponse
	parentIdx             int
	childSuccessResponses []*llm.ChatResponse
	childSuccessIdx       int
	childFailErr          error
}

func (m *routingMockCompleter) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// サブエージェントかどうかをシステムプロンプトで判定
	isChild := false
	isFailChild := false
	for _, msg := range req.Messages {
		if msg.Role == "system" && msg.Content != nil {
			content := *msg.Content
			if strings.Contains(content, "focused assistant") {
				isChild = true
			}
		}
		if msg.Role == "user" && msg.Content != nil {
			content := *msg.Content
			if strings.Contains(content, "always fail") {
				isFailChild = true
			}
		}
	}

	if isChild && isFailChild {
		return nil, m.childFailErr
	}

	if isChild {
		i := m.childSuccessIdx
		m.childSuccessIdx++
		if i < len(m.childSuccessResponses) {
			return m.childSuccessResponses[i], nil
		}
		return nil, fmt.Errorf("unexpected child call %d", i)
	}

	i := m.parentIdx
	m.parentIdx++
	if i < len(m.parentResponses) {
		return m.parentResponses[i], nil
	}
	return nil, fmt.Errorf("unexpected parent call %d", i)
}
