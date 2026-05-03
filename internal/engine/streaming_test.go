package engine

import (
	"context"
	"strings"
	"sync"
	"testing"

	"ai-agent/internal/llm"
)

// streamingMockCompleter は StreamingCompleter を満たすテスト用 Completer。
// ChatCompletionStream の各呼び出しで chunkSets[calls] のチャンク列を返す。
type streamingMockCompleter struct {
	mu        sync.Mutex
	chunkSets [][]llm.StreamEvent
	calls     int
	// 非ストリーミング呼び出しに対するフォールバック応答
	fallback []*llm.ChatResponse
	fbCalls  int
}

func (m *streamingMockCompleter) ChatCompletion(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.fbCalls
	m.fbCalls++
	if i < len(m.fallback) {
		return m.fallback[i], nil
	}
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{Message: llm.Message{Role: "assistant", Content: llm.StringPtr("fallback")}, FinishReason: "stop"},
		},
	}, nil
}

func (m *streamingMockCompleter) ChatCompletionStream(ctx context.Context, _ *llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	m.mu.Lock()
	i := m.calls
	m.calls++
	m.mu.Unlock()
	out := make(chan llm.StreamEvent, 8)
	go func() {
		defer close(out)
		if i >= len(m.chunkSets) {
			return
		}
		for _, evt := range m.chunkSets[i] {
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func TestStreaming_DeltaCallbackInOrder(t *testing.T) {
	mock := &streamingMockCompleter{
		chunkSets: [][]llm.StreamEvent{
			{
				{Delta: "Hel"},
				{Delta: "lo, "},
				{Delta: "world"},
				{Delta: "!"},
				{FinishReason: "stop"},
			},
		},
	}

	var (
		mu     sync.Mutex
		deltas []string
		turns  []int
	)
	cb := func(delta string, turn int) {
		mu.Lock()
		defer mu.Unlock()
		deltas = append(deltas, delta)
		turns = append(turns, turn)
	}

	eng := mustNew(mock,
		WithStreaming(true),
		WithStreamCallback(cb),
	)

	res, err := eng.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Response != "Hello, world!" {
		t.Errorf("Response = %q, want %q", res.Response, "Hello, world!")
	}

	mu.Lock()
	defer mu.Unlock()
	got := strings.Join(deltas, "")
	if got != "Hello, world!" {
		t.Errorf("joined deltas = %q, want Hello, world!", got)
	}
	if len(deltas) != 4 {
		t.Errorf("delta count = %d, want 4", len(deltas))
	}
	for _, tn := range turns {
		if tn != 1 {
			t.Errorf("turn = %d, want 1", tn)
		}
	}
}

func TestStreaming_DisabledFallsBackToChatCompletion(t *testing.T) {
	mock := &streamingMockCompleter{
		fallback: []*llm.ChatResponse{
			{Choices: []llm.Choice{
				{Message: llm.Message{Role: "assistant", Content: llm.StringPtr("hi")}, FinishReason: "stop"},
			}},
		},
	}
	called := false
	cb := func(string, int) { called = true }
	eng := mustNew(mock,
		// WithStreaming は呼ばない（デフォルト false）
		WithStreamCallback(cb),
	)
	if _, err := eng.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called {
		t.Error("stream callback should not be called when streaming disabled")
	}
}

func TestStreaming_FallbackWhenCompleterNotStreaming(t *testing.T) {
	// streaming は有効でも、Completer が StreamingCompleter を実装していなければ通常呼び出し。
	mock := &mockCompleter{
		responses: []*llm.ChatResponse{
			makeResponse("ok", llm.Usage{}),
		},
	}
	called := false
	cb := func(string, int) { called = true }
	eng := mustNew(mock,
		WithStreaming(true),
		WithStreamCallback(cb),
	)
	res, err := eng.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Response != "ok" {
		t.Errorf("Response = %q, want ok", res.Response)
	}
	if called {
		t.Error("stream callback should not be called for non-streaming completer")
	}
}

func TestStreaming_ContextStatusCallback(t *testing.T) {
	mock := &streamingMockCompleter{
		chunkSets: [][]llm.StreamEvent{
			{
				{Delta: "hello"},
				{FinishReason: "stop"},
			},
		},
	}

	var (
		mu     sync.Mutex
		ratios []float64
		counts []int
		limits []int
	)
	cb := func(ratio float64, count, limit int) {
		mu.Lock()
		defer mu.Unlock()
		ratios = append(ratios, ratio)
		counts = append(counts, count)
		limits = append(limits, limit)
	}

	eng := mustNew(mock,
		WithStreaming(true),
		WithStreamCallback(func(string, int) {}),
		WithContextStatusCallback(cb),
		WithTokenLimit(8192),
	)

	if _, err := eng.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(ratios) < 2 {
		t.Fatalf("expected at least 2 ContextStatus events (post-input + turn-start), got %d", len(ratios))
	}
	for i, lim := range limits {
		if lim != 8192 {
			t.Errorf("limit[%d] = %d, want 8192", i, lim)
		}
	}
	for i, r := range ratios {
		if r < 0 || r > 1.5 {
			t.Errorf("ratio[%d] = %f out of expected range", i, r)
		}
		if counts[i] <= 0 {
			t.Errorf("count[%d] = %d, want > 0", i, counts[i])
		}
	}
}

// streaming + tools 経由 (router → chat) のフロー
func TestStreaming_RouterAndChatSteps(t *testing.T) {
	mock := &streamingMockCompleter{
		chunkSets: [][]llm.StreamEvent{
			// 1ターン目: router (JSON)
			{
				{Delta: `{"tool":"none","arguments":{},"reasoning":"direct"}`},
				{FinishReason: "stop"},
			},
			// chatStep (router=none → 通常チャット)
			{
				{Delta: "final "},
				{Delta: "answer"},
				{FinishReason: "stop"},
			},
		},
	}

	var deltas []string
	cb := func(d string, _ int) {
		deltas = append(deltas, d)
	}
	eng := mustNew(mock,
		WithTools(newMockTool("noop", "noop")),
		WithStreaming(true),
		WithStreamCallback(cb),
	)
	res, err := eng.Run(context.Background(), "do something")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Response != "final answer" {
		t.Errorf("response = %q, want 'final answer'", res.Response)
	}
	// 少なくとも router の delta と chat の delta の両方が来ている
	if len(deltas) < 3 {
		t.Errorf("expected >=3 deltas (router + chat), got %d: %v", len(deltas), deltas)
	}
}
