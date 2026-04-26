package context

import (
	stdctx "context"
	"strings"
	"sync"
	"testing"

	"ai-agent/internal/llm"
)

func userMsg(content string) llm.Message {
	return llm.Message{Role: "user", Content: llm.StringPtr(content)}
}

func assistantMsg(content string) llm.Message {
	return llm.Message{Role: "assistant", Content: llm.StringPtr(content)}
}

func TestManager_AddAndMessages(t *testing.T) {
	mgr := NewManager(8192)

	mgr.Add(userMsg("hello"))
	mgr.Add(assistantMsg("hi"))

	msgs := mgr.Messages()
	if len(msgs) != 2 {
		t.Fatalf("Len() = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("msgs[0].Role = %q, want %q", msgs[0].Role, "user")
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1].Role = %q, want %q", msgs[1].Role, "assistant")
	}
}

func TestManager_Len(t *testing.T) {
	mgr := NewManager(8192)
	if mgr.Len() != 0 {
		t.Errorf("Len() = %d, want 0", mgr.Len())
	}

	mgr.Add(userMsg("a"))
	mgr.Add(userMsg("b"))
	if mgr.Len() != 2 {
		t.Errorf("Len() = %d, want 2", mgr.Len())
	}
}

func TestManager_TokenCount(t *testing.T) {
	mgr := NewManager(8192)

	mgr.Add(userMsg("hello"))
	count := mgr.TokenCount()
	if count <= 0 {
		t.Errorf("TokenCount() = %d, want > 0", count)
	}

	// 2つ目のメッセージ追加でトークン数が増える
	countBefore := count
	mgr.Add(assistantMsg("world"))
	countAfter := mgr.TokenCount()
	if countAfter <= countBefore {
		t.Errorf("TokenCount() did not increase: before=%d, after=%d", countBefore, countAfter)
	}
}

func TestManager_TokenCountWithReserved(t *testing.T) {
	mgr := NewManager(8192)
	mgr.SetReserved(500)

	mgr.Add(userMsg("hello"))
	count := mgr.TokenCount()
	if count <= 500 {
		t.Errorf("TokenCount() = %d, want > 500 (reserved=500 + message tokens)", count)
	}
}

func TestManager_UsageRatio(t *testing.T) {
	mgr := NewManager(100)
	mgr.SetReserved(50) // 50%を予約

	ratio := mgr.UsageRatio()
	if ratio < 0.49 || ratio > 0.51 {
		t.Errorf("UsageRatio() = %f, want ~0.5", ratio)
	}
}

func TestManager_UsageRatio_ZeroLimit(t *testing.T) {
	mgr := NewManager(0)
	ratio := mgr.UsageRatio()
	if ratio != 0 {
		t.Errorf("UsageRatio() with zero limit = %f, want 0", ratio)
	}
}

func TestManager_TokenLimit(t *testing.T) {
	mgr := NewManager(4096)
	if mgr.TokenLimit() != 4096 {
		t.Errorf("TokenLimit() = %d, want 4096", mgr.TokenLimit())
	}
}

func TestManager_ThresholdExceeded(t *testing.T) {
	mgr := NewManager(100, WithThreshold(0.5))

	var events []Event
	mgr.OnThreshold(func(e Event) {
		events = append(events, e)
	})

	// 予約で閾値超過
	mgr.SetReserved(60)

	if len(events) != 1 {
		t.Fatalf("events count = %d, want 1", len(events))
	}
	if events[0].Kind != ThresholdExceeded {
		t.Errorf("event kind = %v, want ThresholdExceeded", events[0].Kind)
	}
	if events[0].TokenCount != 60 {
		t.Errorf("event token count = %d, want 60", events[0].TokenCount)
	}
	if events[0].TokenLimit != 100 {
		t.Errorf("event token limit = %d, want 100", events[0].TokenLimit)
	}
}

func TestManager_ThresholdNoDuplicateFire(t *testing.T) {
	mgr := NewManager(100, WithThreshold(0.5))

	var count int
	mgr.OnThreshold(func(e Event) {
		count++
	})

	mgr.SetReserved(60)   // 超過: 発火
	mgr.Add(userMsg("x")) // まだ超過: 発火しない

	if count != 1 {
		t.Errorf("threshold fired %d times, want 1 (no duplicate)", count)
	}
}

func TestManager_ThresholdRecovered(t *testing.T) {
	// 大きなlimitで閾値を低く設定し、予約トークンで制御する
	mgr := NewManager(100, WithThreshold(0.5))

	var events []Event
	mgr.OnThreshold(func(e Event) {
		events = append(events, e)
	})

	mgr.SetReserved(60) // 超過
	mgr.SetReserved(10) // 回復

	if len(events) != 2 {
		t.Fatalf("events count = %d, want 2", len(events))
	}
	if events[0].Kind != ThresholdExceeded {
		t.Errorf("events[0].Kind = %v, want ThresholdExceeded", events[0].Kind)
	}
	if events[1].Kind != ThresholdRecovered {
		t.Errorf("events[1].Kind = %v, want ThresholdRecovered", events[1].Kind)
	}
}

func TestManager_NoObservers(t *testing.T) {
	// Observer未登録でもpanicしない
	mgr := NewManager(100, WithThreshold(0.5))
	mgr.SetReserved(60) // 超過しても問題ない
	mgr.Add(userMsg("hello"))
}

func TestManager_MessagesReturnsCopy(t *testing.T) {
	mgr := NewManager(8192)
	mgr.Add(userMsg("hello"))

	msgs := mgr.Messages()
	msgs[0].Role = "modified"

	// 内部状態が変更されていないことを確認
	original := mgr.Messages()
	if original[0].Role != "user" {
		t.Error("Messages() did not return a copy; internal state was modified")
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	mgr := NewManager(100000)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.Add(userMsg("concurrent"))
			_ = mgr.Messages()
			_ = mgr.TokenCount()
			_ = mgr.UsageRatio()
			_ = mgr.Len()
		}()
	}
	wg.Wait()

	if mgr.Len() != 100 {
		t.Errorf("Len() = %d, want 100 after concurrent adds", mgr.Len())
	}
}

func TestManager_DefaultThreshold(t *testing.T) {
	mgr := NewManager(100) // デフォルト threshold = 0.8

	var events []Event
	mgr.OnThreshold(func(e Event) {
		events = append(events, e)
	})

	mgr.SetReserved(70) // 70% → 発火しない
	if len(events) != 0 {
		t.Errorf("events count = %d, want 0 at 70%%", len(events))
	}

	mgr.SetReserved(80) // 80% → 発火
	if len(events) != 1 {
		t.Errorf("events count = %d, want 1 at 80%%", len(events))
	}
}

// --- Compact ---

func TestManager_Threshold(t *testing.T) {
	mgr := NewManager(100, WithThreshold(0.9))
	if mgr.Threshold() != 0.9 {
		t.Errorf("Threshold() = %f, want 0.9", mgr.Threshold())
	}
}

func TestManagerCompact_BelowThreshold(t *testing.T) {
	mgr := NewManager(10000) // 大きな上限 → 使用率が低い
	mgr.Add(llm.Message{Role: "user", Content: llm.StringPtr("hello")})

	before := mgr.TokenCount()
	err := mgr.Compact(stdctx.Background(), DefaultCompactionConfig())
	if err != nil {
		t.Fatal(err)
	}
	if mgr.TokenCount() != before {
		t.Error("compact should not change tokens when below threshold")
	}
}

func TestManagerCompact_ReducesTokenCount(t *testing.T) {
	mgr := NewManager(200, WithThreshold(0.5))
	// 閾値を超えるようにメッセージを追加
	for i := 0; i < 10; i++ {
		mgr.Add(llm.Message{Role: "user", Content: llm.StringPtr(strings.Repeat("x", 100))})
		mgr.Add(llm.Message{Role: "assistant", Content: llm.StringPtr(strings.Repeat("y", 100))})
	}

	before := mgr.TokenCount()
	cfg := CompactionConfig{
		BudgetMaxChars: 2000,
		KeepLast:       4,
		TargetRatio:    0.3,
	}
	err := mgr.Compact(stdctx.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	after := mgr.TokenCount()
	if after >= before {
		t.Errorf("tokens not reduced: %d >= %d", after, before)
	}
}

func TestManagerCompact_ThresholdRecoveryEvent(t *testing.T) {
	mgr := NewManager(200, WithThreshold(0.5))

	var events []Event
	mgr.OnThreshold(func(e Event) {
		events = append(events, e)
	})

	// 閾値を超える
	for i := 0; i < 10; i++ {
		mgr.Add(llm.Message{Role: "user", Content: llm.StringPtr(strings.Repeat("x", 100))})
	}
	// ThresholdExceeded が発火しているはず
	exceeded := false
	for _, e := range events {
		if e.Kind == ThresholdExceeded {
			exceeded = true
		}
	}
	if !exceeded {
		t.Fatal("ThresholdExceeded not fired")
	}

	// 縮約実行
	cfg := CompactionConfig{
		BudgetMaxChars: 2000,
		KeepLast:       2,
		TargetRatio:    0.1,
	}
	err := mgr.Compact(stdctx.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	// ThresholdRecovered が発火しているはず
	recovered := false
	for _, e := range events {
		if e.Kind == ThresholdRecovered {
			recovered = true
		}
	}
	if !recovered {
		t.Error("ThresholdRecovered not fired after compaction")
	}
}

func TestManagerCompact_MessagesConsistency(t *testing.T) {
	mgr := NewManager(200, WithThreshold(0.5))
	for i := 0; i < 10; i++ {
		mgr.Add(llm.Message{Role: "user", Content: llm.StringPtr(strings.Repeat("x", 50))})
		mgr.Add(llm.Message{Role: "assistant", Content: llm.StringPtr(strings.Repeat("y", 50))})
	}

	cfg := CompactionConfig{
		BudgetMaxChars: 2000,
		KeepLast:       4,
		TargetRatio:    0.3,
	}
	err := mgr.Compact(stdctx.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	msgs := mgr.Messages()
	if len(msgs) == 0 {
		t.Fatal("Messages() returned empty after compaction")
	}

	// トークン数とメッセージ数の整合性
	tokenSum := 0
	for _, m := range msgs {
		tokenSum += EstimateTokens(m)
	}
	// Managerの内部トークン数と推定値の合計が一致するはず
	if mgr.TokenCount() != tokenSum {
		t.Errorf("token mismatch: manager=%d, calculated=%d", mgr.TokenCount(), tokenSum)
	}
}
