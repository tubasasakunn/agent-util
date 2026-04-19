package context

import (
	"encoding/json"
	"testing"

	"ai-agent/internal/llm"
)

func TestEstimateTextTokens(t *testing.T) {
	tests := []struct {
		name string
		text string
		min  int // 最低期待値
		max  int // 最大期待値
	}{
		{
			name: "empty",
			text: "",
			min:  0,
			max:  0,
		},
		{
			name: "single ascii char",
			text: "a",
			min:  1,
			max:  1,
		},
		{
			name: "english sentence",
			text: "Hello, world! This is a test.",
			min:  5,
			max:  15,
		},
		{
			name: "japanese text",
			text: "こんにちは世界",
			min:  7,  // 7文字 × 1.5 = 10.5
			max:  15,
		},
		{
			name: "mixed text",
			text: "Hello こんにちは world",
			min:  10,
			max:  20,
		},
		{
			name: "long english",
			text: "The quick brown fox jumps over the lazy dog. This sentence contains many words to test the estimation.",
			min:  25,
			max:  50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTextTokens(tt.text)
			if got < tt.min || got > tt.max {
				t.Errorf("estimateTextTokens(%q) = %d, want [%d, %d]", tt.text, got, tt.min, tt.max)
			}
		})
	}
}

func TestEstimateTokens_UserMessage(t *testing.T) {
	msg := llm.Message{
		Role:    "user",
		Content: llm.StringPtr("Hello"),
	}
	got := EstimateTokens(msg)
	// "Hello" ≈ 1-2 tokens + overhead 4 = 5-6
	if got < 5 {
		t.Errorf("EstimateTokens(user) = %d, want >= 5", got)
	}
}

func TestEstimateTokens_EmptyContent(t *testing.T) {
	msg := llm.Message{
		Role:    "assistant",
		Content: nil,
	}
	got := EstimateTokens(msg)
	// nil content → 0 + overhead 4
	if got != messageOverhead {
		t.Errorf("EstimateTokens(nil content) = %d, want %d", got, messageOverhead)
	}
}

func TestEstimateTokens_ToolCallMessage(t *testing.T) {
	msg := llm.Message{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "read_file",
					Arguments: json.RawMessage(`{"path":"/tmp/test.txt"}`),
				},
			},
		},
	}
	got := EstimateTokens(msg)
	// overhead(4) + toolcall(name + args + overhead)
	if got < 10 {
		t.Errorf("EstimateTokens(tool_call) = %d, want >= 10", got)
	}
}

func TestEstimateTokens_ToolResultMessage(t *testing.T) {
	msg := llm.Message{
		Role:       "tool",
		Content:    llm.StringPtr("file contents here"),
		ToolCallID: "call_123",
	}
	got := EstimateTokens(msg)
	// overhead(4) + content + toolCallID
	if got < 8 {
		t.Errorf("EstimateTokens(tool_result) = %d, want >= 8", got)
	}
}

func TestEstimateTokens_JapaneseMessage(t *testing.T) {
	msg := llm.Message{
		Role:    "user",
		Content: llm.StringPtr("ファイルを読んでください"),
	}
	got := EstimateTokens(msg)
	// 11文字 × 1.5 = 16.5 + overhead 4 = 20
	if got < 15 {
		t.Errorf("EstimateTokens(japanese) = %d, want >= 15", got)
	}
}

func TestIsCJK(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{'あ', true},   // ひらがな
		{'ア', true},   // カタカナ
		{'漢', true},   // 漢字
		{'한', true},   // ハングル
		{'A', false},   // ASCII
		{'1', false},   // 数字
		{' ', false},   // スペース
		{'é', false},   // ラテン拡張
	}
	for _, tt := range tests {
		t.Run(string(tt.r), func(t *testing.T) {
			if got := isCJK(tt.r); got != tt.want {
				t.Errorf("isCJK(%q) = %v, want %v", tt.r, got, tt.want)
			}
		})
	}
}
