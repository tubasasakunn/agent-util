package context

import (
	stdctx "context"
	"fmt"
	"strings"
	"testing"

	"ai-agent/internal/llm"
)

func makeEntry(role, content string) entry {
	msg := llm.Message{Role: role, Content: llm.StringPtr(content)}
	return entry{Message: msg, Tokens: EstimateTokens(msg)}
}

func makeToolEntry(callID, content string) entry {
	msg := llm.Message{Role: "tool", Content: llm.StringPtr(content), ToolCallID: callID}
	return entry{Message: msg, Tokens: EstimateTokens(msg)}
}

func makeToolCallEntry(callID, toolName string) entry {
	msg := llm.Message{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{
			{ID: callID, Type: "function", Function: llm.FunctionCall{Name: toolName}},
		},
	}
	return entry{Message: msg, Tokens: EstimateTokens(msg)}
}

// --- totalTokens ---

func TestTotalTokens(t *testing.T) {
	entries := []entry{
		makeEntry("user", "hello"),
		makeEntry("assistant", "hi there"),
	}
	got := totalTokens(entries)
	if got <= 0 {
		t.Errorf("totalTokens = %d, want > 0", got)
	}
}

// --- BudgetTrim ---

func TestBudgetTrim_NoTruncation(t *testing.T) {
	entries := []entry{
		makeEntry("user", "hello"),
		makeToolEntry("call_1", "short result"),
	}
	result := budgetTrim(entries, 2000)

	if result[1].Message.ContentString() != "short result" {
		t.Errorf("content changed unexpectedly: %q", result[1].Message.ContentString())
	}
}

func TestBudgetTrim_TruncatesLargeResult(t *testing.T) {
	largeContent := strings.Repeat("x", 3000)
	entries := []entry{
		makeEntry("user", "read this file"),
		makeToolEntry("call_1", largeContent),
	}
	result := budgetTrim(entries, 2000)

	content := result[1].Message.ContentString()
	if len(content) >= len(largeContent) {
		t.Errorf("content not truncated: len=%d", len(content))
	}
	if !strings.Contains(content, "[... truncated") {
		t.Error("truncation marker not found")
	}
	if !strings.Contains(content, "3000 bytes") {
		t.Error("original size not in marker")
	}
}

func TestBudgetTrim_NonToolUnchanged(t *testing.T) {
	largeContent := strings.Repeat("x", 3000)
	entries := []entry{
		makeEntry("user", largeContent),
		makeEntry("assistant", largeContent),
	}
	result := budgetTrim(entries, 2000)

	if result[0].Message.ContentString() != largeContent {
		t.Error("user message was truncated")
	}
	if result[1].Message.ContentString() != largeContent {
		t.Error("assistant message was truncated")
	}
}

func TestBudgetTrim_TokensRecalculated(t *testing.T) {
	largeContent := strings.Repeat("x", 3000)
	entries := []entry{
		makeToolEntry("call_1", largeContent),
	}
	originalTokens := entries[0].Tokens
	result := budgetTrim(entries, 2000)

	if result[0].Tokens >= originalTokens {
		t.Errorf("tokens not reduced: %d >= %d", result[0].Tokens, originalTokens)
	}
}

func TestBudgetTrim_Idempotent(t *testing.T) {
	largeContent := strings.Repeat("x", 3000)
	entries := []entry{
		makeToolEntry("call_1", largeContent),
	}
	first := budgetTrim(entries, 2000)
	second := budgetTrim(first, 2000)

	if first[0].Message.ContentString() != second[0].Message.ContentString() {
		t.Error("budgetTrim is not idempotent")
	}
	if first[0].Tokens != second[0].Tokens {
		t.Errorf("tokens differ on second pass: %d vs %d", first[0].Tokens, second[0].Tokens)
	}
}

func TestBudgetTrim_DoesNotMutateOriginal(t *testing.T) {
	original := "original content"
	entries := []entry{
		makeToolEntry("call_1", strings.Repeat("x", 3000)),
		makeEntry("user", original),
	}
	budgetTrim(entries, 2000)

	// 元のスライスが変更されていないことを確認
	if entries[1].Message.ContentString() != original {
		t.Error("original entries mutated")
	}
}

func TestBudgetTrim_ZeroMaxChars(t *testing.T) {
	entries := []entry{
		makeToolEntry("call_1", "some content"),
	}
	result := budgetTrim(entries, 0)

	if result[0].Message.ContentString() != "some content" {
		t.Error("content changed with maxChars=0")
	}
}

// --- ObservationMask ---

func TestObservationMask_MasksOldTools(t *testing.T) {
	entries := []entry{
		makeEntry("user", "read file"),
		makeToolCallEntry("call_1", "read_file"),
		makeToolEntry("call_1", "file content here..."),
		makeEntry("user", "now read another"),
		makeToolCallEntry("call_2", "read_file"),
		makeToolEntry("call_2", "another file content"),
		makeEntry("assistant", "done"),
	}
	result := observationMask(entries, 3)

	// 古い tool (index 2) はマスクされるべき
	if result[2].Message.ContentString() != "[observation masked]" {
		t.Errorf("old tool not masked: %q", result[2].Message.ContentString())
	}
	// 直近の tool (index 5) は保護されるべき
	if result[5].Message.ContentString() != "another file content" {
		t.Errorf("recent tool was masked: %q", result[5].Message.ContentString())
	}
}

func TestObservationMask_KeepsToolCalls(t *testing.T) {
	entries := []entry{
		makeToolCallEntry("call_1", "read_file"),
		makeToolEntry("call_1", "file content"),
		makeEntry("assistant", "done"),
	}
	result := observationMask(entries, 1)

	// assistant の ToolCalls は変更されないこと
	if len(result[0].Message.ToolCalls) != 1 {
		t.Error("ToolCalls were modified")
	}
	if result[0].Message.ToolCalls[0].Function.Name != "read_file" {
		t.Error("ToolCall name changed")
	}
}

func TestObservationMask_Idempotent(t *testing.T) {
	entries := []entry{
		makeToolEntry("call_1", "file content"),
		makeEntry("assistant", "done"),
	}
	first := observationMask(entries, 1)
	second := observationMask(first, 1)

	if first[0].Message.ContentString() != second[0].Message.ContentString() {
		t.Error("observationMask is not idempotent")
	}
	if first[0].Tokens != second[0].Tokens {
		t.Errorf("tokens differ: %d vs %d", first[0].Tokens, second[0].Tokens)
	}
}

func TestObservationMask_KeepLastExceedsLen(t *testing.T) {
	entries := []entry{
		makeToolEntry("call_1", "file content"),
	}
	result := observationMask(entries, 10)

	if result[0].Message.ContentString() != "file content" {
		t.Error("content changed when keepLast > len")
	}
}

func TestObservationMask_NonToolUnchanged(t *testing.T) {
	entries := []entry{
		makeEntry("user", "hello"),
		makeEntry("assistant", "world"),
		makeEntry("user", "done"),
	}
	result := observationMask(entries, 1)

	if result[0].Message.ContentString() != "hello" {
		t.Error("user message was masked")
	}
	if result[1].Message.ContentString() != "world" {
		t.Error("assistant message was masked")
	}
}

func TestObservationMask_TokensReduced(t *testing.T) {
	largeContent := strings.Repeat("x", 1000)
	entries := []entry{
		makeToolEntry("call_1", largeContent),
		makeEntry("assistant", "done"),
	}
	originalTokens := entries[0].Tokens
	result := observationMask(entries, 1)

	if result[0].Tokens >= originalTokens {
		t.Errorf("tokens not reduced: %d >= %d", result[0].Tokens, originalTokens)
	}
}

// --- Snip ---

func TestSnip_RemovesOldMessages(t *testing.T) {
	entries := []entry{
		makeEntry("user", "old message 1"),
		makeEntry("assistant", "old response 1"),
		makeEntry("user", "old message 2"),
		makeEntry("assistant", "old response 2"),
		makeEntry("user", "recent message"),
		makeEntry("assistant", "recent response"),
	}
	result := snip(entries, 2)

	// マーカー + 保護対象2件 = 3件
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	if !strings.Contains(result[0].Message.ContentString(), "4 earlier messages snipped") {
		t.Errorf("marker not found: %q", result[0].Message.ContentString())
	}
	if result[1].Message.ContentString() != "recent message" {
		t.Errorf("recent message lost: %q", result[1].Message.ContentString())
	}
}

func TestSnip_InsertsMarker(t *testing.T) {
	entries := []entry{
		makeEntry("user", "old"),
		makeEntry("assistant", "old response"),
		makeEntry("user", "new"),
	}
	result := snip(entries, 1)

	if result[0].Message.Role != "user" {
		t.Errorf("marker role = %q, want user", result[0].Message.Role)
	}
	if !strings.Contains(result[0].Message.ContentString(), "snipped") {
		t.Error("marker text not found")
	}
}

func TestSnip_ToolCallPairIntegrity(t *testing.T) {
	entries := []entry{
		makeEntry("user", "old"),
		makeToolCallEntry("call_1", "read_file"),  // index 1
		makeToolEntry("call_1", "file content"),    // index 2
		makeEntry("user", "new question"),           // index 3
		makeEntry("assistant", "answer"),             // index 4
	}
	// keepLast=2 → 保護開始: index 3
	// tool (index 2) と assistant(call_1) (index 1) は両方保護外 → ペアごと削除
	result := snip(entries, 2)

	for _, e := range result {
		if e.Message.ToolCallID == "call_1" {
			t.Error("orphaned tool result found")
		}
		for _, tc := range e.Message.ToolCalls {
			if tc.ID == "call_1" {
				t.Error("orphaned tool call found")
			}
		}
	}
}

func TestSnip_BoundaryAdjustment(t *testing.T) {
	entries := []entry{
		makeEntry("user", "old question"),            // index 0
		makeEntry("assistant", "old answer"),          // index 1
		makeToolCallEntry("call_1", "read_file"),     // index 2
		makeToolEntry("call_1", "file content"),      // index 3
		makeEntry("user", "new question"),             // index 4
		makeEntry("assistant", "new answer"),          // index 5
	}
	// keepLast=3 → 初期保護開始: index 3 (tool)
	// index 3 は tool で対応 assistant は index 2
	// → 保護境界を index 2 に拡張
	result := snip(entries, 3)

	// index 2, 3, 4, 5 が保護 + マーカー = 5件
	if len(result) != 5 {
		t.Fatalf("len = %d, want 5", len(result))
	}
	hasToolCall := false
	hasToolResult := false
	for _, e := range result {
		for _, tc := range e.Message.ToolCalls {
			if tc.ID == "call_1" {
				hasToolCall = true
			}
		}
		if e.Message.ToolCallID == "call_1" {
			hasToolResult = true
		}
	}
	if !hasToolCall || !hasToolResult {
		t.Errorf("ToolCall pair broken: call=%v, result=%v", hasToolCall, hasToolResult)
	}
}

func TestSnip_KeepLastExceedsLen(t *testing.T) {
	entries := []entry{
		makeEntry("user", "hello"),
		makeEntry("assistant", "world"),
	}
	result := snip(entries, 10)

	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}
}

func TestSnip_NoOldMessages(t *testing.T) {
	entries := []entry{
		makeEntry("user", "hello"),
	}
	result := snip(entries, 1)

	if len(result) != 1 {
		t.Errorf("len = %d, want 1", len(result))
	}
}

// --- Compact ---

func TestCompact_NilSummarizer(t *testing.T) {
	entries := []entry{
		makeEntry("user", "old"),
		makeEntry("assistant", "old response"),
		makeEntry("user", "new"),
	}
	result, err := compact(stdctx.Background(), entries, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Errorf("len = %d, want 3 (unchanged)", len(result))
	}
}

func TestCompact_SummarizesOldMessages(t *testing.T) {
	entries := []entry{
		makeEntry("user", "old question 1"),
		makeEntry("assistant", "old answer 1"),
		makeEntry("user", "old question 2"),
		makeEntry("assistant", "old answer 2"),
		makeEntry("user", "new question"),
		makeEntry("assistant", "new answer"),
	}
	summarizer := func(_ stdctx.Context, msgs []llm.Message) (string, error) {
		return "User asked two questions and got answers.", nil
	}
	result, err := compact(stdctx.Background(), entries, 2, summarizer)
	if err != nil {
		t.Fatal(err)
	}

	// マーカー + 保護対象2件 = 3件
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	if !strings.Contains(result[0].Message.ContentString(), "Summary of 4 earlier messages") {
		t.Errorf("summary marker not found: %q", result[0].Message.ContentString())
	}
	if !strings.Contains(result[0].Message.ContentString(), "User asked two questions") {
		t.Error("summary content not found")
	}
}

func TestCompact_SummarizerError(t *testing.T) {
	entries := []entry{
		makeEntry("user", "old"),
		makeEntry("user", "new"),
	}
	summarizer := func(_ stdctx.Context, _ []llm.Message) (string, error) {
		return "", fmt.Errorf("LLM error")
	}
	_, err := compact(stdctx.Background(), entries, 1, summarizer)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestCompact_KeepsRecentMessages(t *testing.T) {
	entries := []entry{
		makeEntry("user", "old"),
		makeEntry("assistant", "old response"),
		makeEntry("user", "new question"),
		makeEntry("assistant", "new answer"),
	}
	summarizer := func(_ stdctx.Context, _ []llm.Message) (string, error) {
		return "summary", nil
	}
	result, err := compact(stdctx.Background(), entries, 2, summarizer)
	if err != nil {
		t.Fatal(err)
	}

	if result[1].Message.ContentString() != "new question" {
		t.Errorf("recent message lost: %q", result[1].Message.ContentString())
	}
	if result[2].Message.ContentString() != "new answer" {
		t.Errorf("recent message lost: %q", result[2].Message.ContentString())
	}
}

// --- runCompaction カスケード ---

func TestRunCompaction_BudgetAloneSufficient(t *testing.T) {
	// 巨大なツール結果1件 + 小さなメッセージ → BudgetTrimだけで目標到達
	largeContent := strings.Repeat("x", 5000)
	entries := []entry{
		makeEntry("user", "read file"),
		makeToolCallEntry("call_1", "read_file"),
		makeToolEntry("call_1", largeContent),
		makeEntry("user", "done"),
	}
	cfg := CompactionConfig{
		BudgetMaxChars: 500,
		KeepLast:       6,
		TargetRatio:    0.6,
	}
	// tokenLimit を大きめに設定して BudgetTrim だけで足りるようにする
	result, err := runCompaction(stdctx.Background(), entries, 0, 10000, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// ツール結果が切り詰められている
	if !strings.Contains(result[2].Message.ContentString(), "[... truncated") {
		t.Error("tool result not truncated")
	}
	// メッセージ数は変わらない（Snipは実行されていない）
	if len(result) != 4 {
		t.Errorf("len = %d, want 4", len(result))
	}
}

func TestRunCompaction_NeedsMask(t *testing.T) {
	// BudgetTrimだけでは不十分 → ObservationMask まで実行
	entries := []entry{
		makeEntry("user", "old question"),
		makeToolCallEntry("call_1", "read_file"),
		makeToolEntry("call_1", strings.Repeat("x", 800)),
		makeEntry("user", "new question"),
		makeEntry("assistant", "answer"),
	}
	cfg := CompactionConfig{
		BudgetMaxChars: 2000, // 800文字は上限以下 → BudgetTrimでは変化なし
		KeepLast:       2,
		TargetRatio:    0.3, // 低い目標で ObservationMask まで必要に
	}
	result, err := runCompaction(stdctx.Background(), entries, 0, 1000, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// 古い tool 結果がマスクされている
	for _, e := range result {
		if e.Message.Role == "tool" && e.Message.ContentString() != "[observation masked]" {
			// 保護対象外の tool がマスクされていない
			t.Error("old tool result not masked")
		}
	}
}

func TestRunCompaction_NeedsSnip(t *testing.T) {
	// ObservationMaskでも不十分 → Snip まで実行
	// user/assistant のみ（tool なし）→ BudgetTrimとObservationMaskは効果なし
	entries := []entry{
		makeEntry("user", strings.Repeat("x", 500)),
		makeEntry("assistant", strings.Repeat("y", 500)),
		makeEntry("user", strings.Repeat("z", 500)),
		makeEntry("assistant", strings.Repeat("w", 500)),
		makeEntry("user", "new"),
		makeEntry("assistant", "answer"),
	}
	cfg := CompactionConfig{
		BudgetMaxChars: 2000,
		KeepLast:       2,
		TargetRatio:    0.1,
	}
	// tokenLimit を小さくして使用率を閾値以上にする
	result, err := runCompaction(stdctx.Background(), entries, 0, 800, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Snip が実行されマーカーが挿入されている
	if !strings.Contains(result[0].Message.ContentString(), "snipped") {
		t.Errorf("snip marker not found: %q", result[0].Message.ContentString())
	}
}

func TestRunCompaction_ToolCallPairIntegrity(t *testing.T) {
	entries := []entry{
		makeEntry("user", "old"),
		makeToolCallEntry("call_1", "read_file"),
		makeToolEntry("call_1", strings.Repeat("x", 500)),
		makeToolCallEntry("call_2", "echo"),
		makeToolEntry("call_2", "hello"),
		makeEntry("user", "new"),
		makeEntry("assistant", "answer"),
	}
	cfg := CompactionConfig{
		BudgetMaxChars: 2000,
		KeepLast:       2,
		TargetRatio:    0.01,
	}
	result, err := runCompaction(stdctx.Background(), entries, 0, 10000, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// ToolCall と ToolResult がペアで存在するか検証
	callIDs := make(map[string]bool)
	resultIDs := make(map[string]bool)
	for _, e := range result {
		for _, tc := range e.Message.ToolCalls {
			callIDs[tc.ID] = true
		}
		if e.Message.ToolCallID != "" {
			resultIDs[e.Message.ToolCallID] = true
		}
	}
	for id := range callIDs {
		if !resultIDs[id] {
			t.Errorf("orphaned tool call: %s", id)
		}
	}
	for id := range resultIDs {
		if !callIDs[id] {
			t.Errorf("orphaned tool result: %s", id)
		}
	}
}
