package context

import (
	"unicode"

	"ai-agent/internal/llm"
)

// messageOverhead はメッセージ1件あたりのオーバーヘッドトークン数。
// ロール名 + フォーマット用トークン（<|start_of_turn|> 等）に相当する。
const messageOverhead = 4

// EstimateTokens はメッセージのトークン数を推定する。
// 厳密なtokenizerは使わず、文字種に応じた係数で推定する。
func EstimateTokens(msg llm.Message) int {
	tokens := messageOverhead

	// Content
	tokens += estimateTextTokens(msg.ContentString())

	// ToolCalls（assistant のツール呼び出し）
	for _, tc := range msg.ToolCalls {
		tokens += estimateTextTokens(tc.Function.Name)
		tokens += estimateTextTokens(string(tc.Function.Arguments))
		tokens += 4 // ToolCall構造のオーバーヘッド（id, type等）
	}

	// ToolCallID（tool ロールのレスポンス）
	if msg.ToolCallID != "" {
		tokens += estimateTextTokens(msg.ToolCallID)
	}

	return tokens
}

// EstimateTextTokens はテキストのトークン数を推定する。
// CJK文字は1文字あたり約1.5トークン、ASCII文字は1文字あたり約0.35トークンとして計算する。
func EstimateTextTokens(text string) int {
	return estimateTextTokens(text)
}

func estimateTextTokens(text string) int {
	if text == "" {
		return 0
	}

	var total float64
	for _, r := range text {
		if isCJK(r) {
			total += 1.5
		} else {
			total += 0.35
		}
	}

	// 最低1トークン（空でないテキストは少なくとも1トークン）
	result := int(total)
	if result == 0 {
		return 1
	}
	return result
}

// isCJK は文字がCJK（日本語・中国語・韓国語）文字かどうかを判定する。
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || // 漢字
		unicode.Is(unicode.Hiragana, r) || // ひらがな
		unicode.Is(unicode.Katakana, r) || // カタカナ
		unicode.Is(unicode.Hangul, r) // ハングル
}
