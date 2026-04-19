// Phase 5 コンテキスト縮約の統合検証
// go test -v -run TestScenario ./...claude/skills/investigation/003_phase5_context_compaction/
//
// ユニットテスト（internal/context/compaction_test.go）とは異なり、
// 実際のエージェント利用シナリオでの縮約動作を検証する。
package investigation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	agentctx "ai-agent/internal/context"
	"ai-agent/internal/engine"
	"ai-agent/internal/llm"
	"ai-agent/pkg/tool"
)

// --- テストヘルパー ---

type mockCompleter struct {
	responses []*llm.ChatResponse
	calls     int
}

func (m *mockCompleter) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	i := m.calls
	m.calls++
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return nil, fmt.Errorf("unexpected call %d", i)
}

func chatResp(content string) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{Message: llm.Message{Role: "assistant", Content: llm.StringPtr(content)}, FinishReason: "stop"},
		},
		Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

type largeTool struct{}

func (t *largeTool) Name() string             { return "read_file" }
func (t *largeTool) Description() string       { return "Read a file" }
func (t *largeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (t *largeTool) IsReadOnly() bool          { return true }
func (t *largeTool) IsConcurrencySafe() bool   { return false }
func (t *largeTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	// 5000文字のファイル内容を返す（8Kコンテキストの大部分を消費）
	content := strings.Repeat("line of code with some content\n", 167) // ~5010 chars
	return tool.Result{Content: content}, nil
}

// --- シナリオ 1: BudgetTrim が巨大ツール結果を切り詰める ---

func TestScenario_BudgetTrimsLargeToolResult(t *testing.T) {
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			// ルーター: read_file 選択
			chatResp(`{"tool":"read_file","arguments":{"path":"big.go"},"reasoning":"read"}`),
			// ルーター: 応答完了
			chatResp(`{"tool":"none","arguments":{},"reasoning":"done"}`),
			// チャット: 最終応答
			chatResp("The file contains code."),
		},
	}

	cfg := agentctx.CompactionConfig{
		BudgetMaxChars: 500,
		KeepLast:       6,
		TargetRatio:    0.6,
	}
	eng := engine.New(mock,
		engine.WithTools(&largeTool{}),
		engine.WithTokenLimit(8192),
		engine.WithCompaction(cfg),
		engine.WithSystemPrompt("You are a helpful assistant."),
	)

	result, err := eng.Run(context.Background(), "read big.go")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	t.Logf("Response: %s", result.Response)
	t.Logf("Turns: %d", result.Turns)
	t.Logf("Tokens: %d", result.Usage.TotalTokens)

	if result.Response != "The file contains code." {
		t.Errorf("unexpected response: %q", result.Response)
	}
}

// --- シナリオ 2: 多ターン対話でコンテキストが溢れない ---

func TestScenario_MultiTurnDoesNotOverflow(t *testing.T) {
	// 10回の対話を小さなコンテキスト(500トークン)で行う
	mgr := agentctx.NewManager(500, agentctx.WithThreshold(0.8))

	mgr.OnThreshold(func(evt agentctx.Event) {
		if evt.Kind == agentctx.ThresholdExceeded {
			t.Logf("Threshold exceeded: ratio=%.2f, tokens=%d/%d",
				evt.UsageRatio, evt.TokenCount, evt.TokenLimit)
		}
	})

	// 多ターンのメッセージを追加
	for i := 0; i < 10; i++ {
		mgr.Add(llm.Message{
			Role:    "user",
			Content: llm.StringPtr(fmt.Sprintf("Question %d: %s", i, strings.Repeat("x", 50))),
		})
		mgr.Add(llm.Message{
			Role:    "assistant",
			Content: llm.StringPtr(fmt.Sprintf("Answer %d: %s", i, strings.Repeat("y", 50))),
		})

		// 閾値超過時に縮約
		if mgr.UsageRatio() >= mgr.Threshold() {
			cfg := agentctx.CompactionConfig{
				BudgetMaxChars: 2000,
				KeepLast:       4,
				TargetRatio:    0.5,
			}
			err := mgr.Compact(context.Background(), cfg)
			if err != nil {
				t.Fatalf("Compact failed at turn %d: %v", i, err)
			}
			t.Logf("Turn %d: compacted → %d tokens (%.0f%%)",
				i, mgr.TokenCount(), mgr.UsageRatio()*100)
		}
	}

	t.Logf("Final: %d messages, %d tokens, ratio=%.2f",
		mgr.Len(), mgr.TokenCount(), mgr.UsageRatio())

	// 最終的にコンテキストが上限を超えていないこと
	if mgr.UsageRatio() > 1.0 {
		t.Errorf("context overflow: ratio=%.2f", mgr.UsageRatio())
	}
	// 直近メッセージが保持されていること
	msgs := mgr.Messages()
	lastMsg := msgs[len(msgs)-1]
	if !strings.Contains(lastMsg.ContentString(), "Answer 9") {
		t.Errorf("last message lost: %q", lastMsg.ContentString())
	}
}

// --- シナリオ 3: ツール実行ペアの整合性が縮約後も維持される ---

func TestScenario_ToolCallPairIntegrityAfterCompaction(t *testing.T) {
	mgr := agentctx.NewManager(300, agentctx.WithThreshold(0.5))

	// ToolCall + ToolResult ペアを複数追加
	for i := 0; i < 5; i++ {
		callID := fmt.Sprintf("call_%d", i)
		toolName := "read_file"

		// user
		mgr.Add(llm.Message{
			Role:    "user",
			Content: llm.StringPtr(fmt.Sprintf("read file_%d.go", i)),
		})
		// assistant (tool_calls)
		mgr.Add(llm.Message{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{
				{ID: callID, Type: "function", Function: llm.FunctionCall{Name: toolName}},
			},
		})
		// tool (result)
		mgr.Add(llm.Message{
			Role:       "tool",
			Content:    llm.StringPtr(fmt.Sprintf("content of file_%d: %s", i, strings.Repeat("x", 100))),
			ToolCallID: callID,
		})
	}

	t.Logf("Before compaction: %d messages, %d tokens", mgr.Len(), mgr.TokenCount())

	cfg := agentctx.CompactionConfig{
		BudgetMaxChars: 2000,
		KeepLast:       6, // 直近2サイクル分（user + tool_call + tool_result = 3 × 2 = 6）
		TargetRatio:    0.3,
	}
	err := mgr.Compact(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	t.Logf("After compaction: %d messages, %d tokens", mgr.Len(), mgr.TokenCount())

	// ToolCall と ToolResult のペア整合性を検証
	msgs := mgr.Messages()
	callIDs := make(map[string]bool)
	resultIDs := make(map[string]bool)
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			callIDs[tc.ID] = true
		}
		if m.ToolCallID != "" {
			resultIDs[m.ToolCallID] = true
		}
	}

	for id := range callIDs {
		if !resultIDs[id] {
			t.Errorf("orphaned tool call: %s (no matching tool result)", id)
		}
	}
	for id := range resultIDs {
		if !callIDs[id] {
			t.Errorf("orphaned tool result: %s (no matching tool call)", id)
		}
	}

	if len(callIDs) != len(resultIDs) {
		t.Errorf("call/result count mismatch: calls=%d, results=%d", len(callIDs), len(resultIDs))
	}
	t.Logf("ToolCall pairs remaining: %d (all consistent)", len(callIDs))
}

// --- シナリオ 4: 観測マスキングが推論トレースを保持する ---

func TestScenario_ObservationMaskPreservesReasoning(t *testing.T) {
	mgr := agentctx.NewManager(400, agentctx.WithThreshold(0.5))

	// 古いツール実行サイクル
	mgr.Add(llm.Message{Role: "user", Content: llm.StringPtr("read old_file.go")})
	mgr.Add(llm.Message{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{
			{ID: "call_old", Type: "function", Function: llm.FunctionCall{
				Name:      "read_file",
				Arguments: json.RawMessage(`{"path":"old_file.go"}`),
			}},
		},
	})
	mgr.Add(llm.Message{
		Role:       "tool",
		Content:    llm.StringPtr(strings.Repeat("old file content line\n", 50)),
		ToolCallID: "call_old",
	})

	// 新しいツール実行サイクル
	mgr.Add(llm.Message{Role: "user", Content: llm.StringPtr("read new_file.go")})
	mgr.Add(llm.Message{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{
			{ID: "call_new", Type: "function", Function: llm.FunctionCall{
				Name:      "read_file",
				Arguments: json.RawMessage(`{"path":"new_file.go"}`),
			}},
		},
	})
	mgr.Add(llm.Message{
		Role:       "tool",
		Content:    llm.StringPtr("new file content"),
		ToolCallID: "call_new",
	})

	tokensBefore := mgr.TokenCount()
	t.Logf("Before compaction: %d tokens", tokensBefore)

	cfg := agentctx.CompactionConfig{
		BudgetMaxChars: 2000,
		KeepLast:       3, // 直近1サイクル（user + tool_call + tool_result）
		TargetRatio:    0.3,
	}
	err := mgr.Compact(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	tokensAfter := mgr.TokenCount()
	t.Logf("After compaction: %d tokens (%.0f%% reduction)",
		tokensAfter, float64(tokensBefore-tokensAfter)/float64(tokensBefore)*100)

	msgs := mgr.Messages()
	for _, m := range msgs {
		t.Logf("  [%s] content=%d chars, toolcalls=%d, toolcallid=%q",
			m.Role, len(m.ContentString()), len(m.ToolCalls), m.ToolCallID)
	}

	// 古いtool結果がマスクされているか Snip されていること
	for _, m := range msgs {
		if m.ToolCallID == "call_old" && m.Role == "tool" {
			content := m.ContentString()
			if content != "[observation masked]" && !strings.Contains(content, "snipped") {
				t.Errorf("old tool result not masked or snipped: %q", content[:min(50, len(content))])
			}
		}
	}

	// 新しいtool結果が保持されていること
	found := false
	for _, m := range msgs {
		if m.ToolCallID == "call_new" && m.ContentString() == "new file content" {
			found = true
		}
	}
	if !found {
		t.Error("new tool result was lost after compaction")
	}
}

// --- シナリオ 5: 8K コンテキストでの現実的な利用シミュレーション ---

func TestScenario_Realistic8KSimulation(t *testing.T) {
	mgr := agentctx.NewManager(8192, agentctx.WithThreshold(0.8))

	// システムプロンプト + ツール定義の予約
	systemPrompt := "You are a helpful coding assistant. You can read files and execute shell commands."
	toolDefs := `## Available Tools\n\n### read_file\nRead file content\n### shell\nExecute shell command`
	reserved := agentctx.EstimateTextTokens(systemPrompt) + agentctx.EstimateTextTokens(toolDefs)
	mgr.SetReserved(reserved)

	t.Logf("Reserved tokens: %d (system+tools)", reserved)

	compactionCount := 0
	cfg := agentctx.CompactionConfig{
		BudgetMaxChars: 2000,
		KeepLast:       6,
		TargetRatio:    0.6,
	}

	// 20ターンの対話をシミュレート
	for turn := 0; turn < 20; turn++ {
		// ユーザー質問
		mgr.Add(llm.Message{
			Role:    "user",
			Content: llm.StringPtr(fmt.Sprintf("Turn %d: Can you help me with this code? %s", turn, strings.Repeat("detail ", 20))),
		})

		// 偶数ターンではツール実行を含む
		if turn%2 == 0 {
			callID := fmt.Sprintf("call_%d", turn)
			mgr.Add(llm.Message{
				Role: "assistant",
				ToolCalls: []llm.ToolCall{
					{ID: callID, Type: "function", Function: llm.FunctionCall{
						Name:      "read_file",
						Arguments: json.RawMessage(fmt.Sprintf(`{"path":"file_%d.go"}`, turn)),
					}},
				},
			})
			// ツール結果（500〜2000文字のファイル内容）
			fileSize := 500 + (turn * 150)
			mgr.Add(llm.Message{
				Role:       "tool",
				Content:    llm.StringPtr(strings.Repeat(fmt.Sprintf("// file_%d.go line\n", turn), fileSize/20)),
				ToolCallID: callID,
			})
		}

		// アシスタント応答
		mgr.Add(llm.Message{
			Role:    "assistant",
			Content: llm.StringPtr(fmt.Sprintf("Here's my analysis for turn %d: %s", turn, strings.Repeat("explanation ", 15))),
		})

		// 閾値超過時に縮約
		if mgr.UsageRatio() >= mgr.Threshold() {
			err := mgr.Compact(context.Background(), cfg)
			if err != nil {
				t.Fatalf("Compact failed at turn %d: %v", turn, err)
			}
			compactionCount++
			t.Logf("Turn %02d: compaction #%d → %d msgs, %d/%d tokens (%.0f%%)",
				turn, compactionCount, mgr.Len(), mgr.TokenCount(), mgr.TokenLimit(), mgr.UsageRatio()*100)
		}
	}

	t.Logf("\n=== Final State ===")
	t.Logf("Messages: %d", mgr.Len())
	t.Logf("Tokens: %d / %d (%.0f%%)", mgr.TokenCount(), mgr.TokenLimit(), mgr.UsageRatio()*100)
	t.Logf("Compactions triggered: %d", compactionCount)

	// 検証
	if mgr.UsageRatio() > 1.0 {
		t.Errorf("context overflow after 20 turns: ratio=%.2f", mgr.UsageRatio())
	}
	if compactionCount == 0 {
		t.Error("no compaction was triggered during 20 turns (expected at least 1)")
	}

	// 直近メッセージの保持
	msgs := mgr.Messages()
	lastMsg := msgs[len(msgs)-1]
	if !strings.Contains(lastMsg.ContentString(), "turn 19") {
		t.Errorf("most recent message lost: %q", lastMsg.ContentString())
	}

	// ToolCall ペア整合性
	callIDs := make(map[string]bool)
	resultIDs := make(map[string]bool)
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			callIDs[tc.ID] = true
		}
		if m.ToolCallID != "" {
			resultIDs[m.ToolCallID] = true
		}
	}
	for id := range callIDs {
		if !resultIDs[id] {
			t.Errorf("orphaned tool call after simulation: %s", id)
		}
	}
	for id := range resultIDs {
		if !callIDs[id] {
			t.Errorf("orphaned tool result after simulation: %s", id)
		}
	}
	t.Logf("ToolCall pairs: %d (all consistent)", len(callIDs))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
