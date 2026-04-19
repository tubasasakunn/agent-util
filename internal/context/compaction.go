package context

import (
	"context"
	"fmt"
	"strings"

	"ai-agent/internal/llm"
)

// Summarizer はメッセージ列を要約するコールバック。
// nil の場合、Compact ステージはスキップされる。
type Summarizer func(ctx context.Context, msgs []llm.Message) (string, error)

// CompactionConfig は縮約カスケードのパラメータ。
type CompactionConfig struct {
	// BudgetMaxChars はツール結果の最大文字数。超過分は切り詰められる。
	BudgetMaxChars int
	// KeepLast は縮約対象外とする直近メッセージ数。
	KeepLast int
	// TargetRatio は縮約の目標使用率（0.0〜1.0）。
	TargetRatio float64
	// Summarizer はCompactステージで使用する要約関数。nil ならスキップ。
	Summarizer Summarizer
}

// DefaultCompactionConfig はデフォルトの縮約設定を返す。
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		BudgetMaxChars: 2000,
		KeepLast:       6,
		TargetRatio:    0.6,
	}
}

// totalTokens はエントリのトークン合計を返す。
func totalTokens(entries []entry) int {
	total := 0
	for _, e := range entries {
		total += e.Tokens
	}
	return total
}

// usageRatio は使用率を計算する。
func usageRatio(entries []entry, reserved, tokenLimit int) float64 {
	if tokenLimit == 0 {
		return 0
	}
	return float64(reserved+totalTokens(entries)) / float64(tokenLimit)
}

// budgetTrim は全ツール結果の Content を maxChars 文字に切り詰める。
// 切り詰めた場合は末尾にマーカーを付加し、トークン数を再推定する。
// 保護対象（keepLast）に関わらず全件に適用する。
func budgetTrim(entries []entry, maxChars int) []entry {
	if maxChars <= 0 {
		return entries
	}

	result := make([]entry, len(entries))
	copy(result, entries)

	for i := range result {
		if result[i].Message.Role != "tool" {
			continue
		}
		content := result[i].Message.ContentString()
		if len(content) <= maxChars || strings.Contains(content, "\n\n[... truncated") {
			continue
		}

		truncated := content[:maxChars] + fmt.Sprintf("\n\n[... truncated, original: %d bytes ...]", len(content))
		msg := result[i].Message
		msg.Content = llm.StringPtr(truncated)
		result[i] = entry{
			Message: msg,
			Tokens:  EstimateTokens(msg),
		}
	}
	return result
}

const maskedContent = "[observation masked]"

// observationMask は保護対象外の tool ロールメッセージの Content をマスクする。
// assistant の ToolCalls は保持し、推論トレースを維持する。
// 既にマスク済みのメッセージはスキップする（冪等）。
func observationMask(entries []entry, keepLast int) []entry {
	if keepLast >= len(entries) {
		return entries
	}

	result := make([]entry, len(entries))
	copy(result, entries)

	cutIdx := len(entries) - keepLast
	for i := 0; i < cutIdx; i++ {
		if result[i].Message.Role != "tool" {
			continue
		}
		if result[i].Message.ContentString() == maskedContent {
			continue
		}

		msg := result[i].Message
		msg.Content = llm.StringPtr(maskedContent)
		result[i] = entry{
			Message: msg,
			Tokens:  EstimateTokens(msg),
		}
	}
	return result
}

// adjustKeepBoundary は保護境界が ToolCall-ToolResult ペアを分断しないよう調整する。
// 返り値は保護開始インデックス（この位置以降が保護対象）。
func adjustKeepBoundary(entries []entry, keepLast int) int {
	if keepLast >= len(entries) {
		return 0
	}
	cutIdx := len(entries) - keepLast

	// 保護領域内の tool メッセージに対応する assistant(tool_calls) が
	// 保護領域外にある場合、保護領域を拡張する
	for i := cutIdx; i < len(entries); i++ {
		if entries[i].Message.Role != "tool" || entries[i].Message.ToolCallID == "" {
			continue
		}
		assistantIdx := findToolCallAssistant(entries, entries[i].Message.ToolCallID, i)
		if assistantIdx >= 0 && assistantIdx < cutIdx {
			cutIdx = assistantIdx
		}
	}
	return cutIdx
}

// findToolCallAssistant は tool メッセージの ToolCallID に対応する
// assistant(tool_calls) メッセージのインデックスを返す。見つからない場合は -1。
func findToolCallAssistant(entries []entry, toolCallID string, beforeIdx int) int {
	for i := beforeIdx - 1; i >= 0; i-- {
		for _, tc := range entries[i].Message.ToolCalls {
			if tc.ID == toolCallID {
				return i
			}
		}
	}
	return -1
}

// snip は保護対象外のメッセージを削除し、マーカーメッセージを先頭に挿入する。
// ToolCall-ToolResult のペア整合性を維持するため、保護境界を調整する。
func snip(entries []entry, keepLast int) []entry {
	if keepLast >= len(entries) {
		return entries
	}

	cutIdx := adjustKeepBoundary(entries, keepLast)
	if cutIdx == 0 {
		return entries
	}

	removed := cutIdx
	marker := makeMarkerEntry(fmt.Sprintf("[%d earlier messages snipped]", removed))
	result := make([]entry, 0, 1+len(entries)-cutIdx)
	result = append(result, marker)
	result = append(result, entries[cutIdx:]...)
	return result
}

// makeMarkerEntry はマーカーメッセージのエントリを作成する。
func makeMarkerEntry(content string) entry {
	msg := llm.Message{Role: "user", Content: llm.StringPtr(content)}
	return entry{Message: msg, Tokens: EstimateTokens(msg)}
}

// compact は保護対象外のメッセージを Summarizer で要約し、
// 1件の user ロールメッセージに置換する。
// Summarizer が nil の場合は何もしない（entries をそのまま返す）。
func compact(ctx context.Context, entries []entry, keepLast int, summarizer Summarizer) ([]entry, error) {
	if summarizer == nil {
		return entries, nil
	}
	if keepLast >= len(entries) {
		return entries, nil
	}

	cutIdx := adjustKeepBoundary(entries, keepLast)
	if cutIdx == 0 {
		return entries, nil
	}

	// 要約対象のメッセージを抽出
	oldMsgs := make([]llm.Message, cutIdx)
	for i := 0; i < cutIdx; i++ {
		oldMsgs[i] = entries[i].Message
	}

	summary, err := summarizer(ctx, oldMsgs)
	if err != nil {
		return nil, fmt.Errorf("compact summarizer: %w", err)
	}

	marker := makeMarkerEntry(fmt.Sprintf("[Summary of %d earlier messages]\n%s", cutIdx, summary))
	result := make([]entry, 0, 1+len(entries)-cutIdx)
	result = append(result, marker)
	result = append(result, entries[cutIdx:]...)
	return result, nil
}

// runCompaction は4段階の縮約カスケードを順に実行する。
// 各段階の実行後に使用率を確認し、目標以下に達したら残りをスキップする。
func runCompaction(ctx context.Context, entries []entry, reserved, tokenLimit int, cfg CompactionConfig) ([]entry, error) {
	// Stage 1: BudgetTrim
	entries = budgetTrim(entries, cfg.BudgetMaxChars)
	if usageRatio(entries, reserved, tokenLimit) < cfg.TargetRatio {
		return entries, nil
	}

	// Stage 2: ObservationMask
	entries = observationMask(entries, cfg.KeepLast)
	if usageRatio(entries, reserved, tokenLimit) < cfg.TargetRatio {
		return entries, nil
	}

	// Stage 3: Snip
	entries = snip(entries, cfg.KeepLast)
	if usageRatio(entries, reserved, tokenLimit) < cfg.TargetRatio {
		return entries, nil
	}

	// Stage 4: Compact
	return compact(ctx, entries, cfg.KeepLast, cfg.Summarizer)
}
