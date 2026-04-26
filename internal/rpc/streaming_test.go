package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"ai-agent/internal/llm"
	"ai-agent/pkg/protocol"
)

// streamingTestCompleter は StreamingCompleter を満たすテスト用 Completer。
type streamingTestCompleter struct {
	mu        sync.Mutex
	chunkSets [][]llm.StreamEvent
	calls     int
}

func (m *streamingTestCompleter) ChatCompletion(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{Message: llm.Message{Role: "assistant", Content: llm.StringPtr("nostream")}, FinishReason: "stop"},
		},
	}, nil
}

func (m *streamingTestCompleter) ChatCompletionStream(ctx context.Context, _ *llm.ChatRequest) (<-chan llm.StreamEvent, error) {
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

// readNotifications は buf の中身を1行ずつ JSON-RPC リクエスト/通知としてパースして返す。
func readNotifications(t *testing.T, buf *bytes.Buffer) []protocol.Request {
	t.Helper()
	var msgs []protocol.Request
	for {
		line, err := buf.ReadBytes('\n')
		if len(line) > 0 {
			var req protocol.Request
			if err := json.Unmarshal(bytes.TrimSpace(line), &req); err == nil && req.Method != "" {
				msgs = append(msgs, req)
			}
		}
		if err != nil {
			break
		}
	}
	return msgs
}

func TestStreaming_HandlerEmitsStreamDelta(t *testing.T) {
	mock := &streamingTestCompleter{
		chunkSets: [][]llm.StreamEvent{
			{
				{Delta: "He"},
				{Delta: "llo"},
				{Delta: "!"},
				{FinishReason: "stop"},
			},
		},
	}

	var buf bytes.Buffer
	srv := New(io.NopCloser(bytes.NewReader(nil)), &buf)
	h := newTestHandlersWithServer(t, mock, srv)
	h.RegisterAll()

	cfg := mustJSON(t, protocol.AgentConfigureParams{
		Streaming: &protocol.StreamingConfig{Enabled: boolp(true)},
	})
	if _, rpcErr := h.handleAgentConfigure(context.Background(), cfg); rpcErr != nil {
		t.Fatalf("configure: %+v", rpcErr)
	}

	runParams := mustJSON(t, protocol.AgentRunParams{Prompt: "hi"})
	res, rpcErr := h.handleAgentRun(context.Background(), runParams)
	if rpcErr != nil {
		t.Fatalf("run: %+v", rpcErr)
	}
	ar := res.(protocol.AgentRunResult)
	if ar.Response != "Hello!" {
		t.Errorf("Response = %q, want Hello!", ar.Response)
	}

	// stdout に流れた通知を解析
	notifs := readNotifications(t, &buf)
	var deltas []string
	var endCount int
	for _, n := range notifs {
		switch n.Method {
		case protocol.MethodStreamDelta:
			var p protocol.StreamDeltaParams
			if err := json.Unmarshal(n.Params, &p); err != nil {
				t.Fatalf("unmarshal delta: %v", err)
			}
			deltas = append(deltas, p.Text)
		case protocol.MethodStreamEnd:
			endCount++
		}
	}
	if len(deltas) != 3 {
		t.Errorf("delta notifications = %d, want 3 (got %v)", len(deltas), deltas)
	}
	if joined := strings.Join(deltas, ""); joined != "Hello!" {
		t.Errorf("joined deltas = %q, want Hello!", joined)
	}
	if endCount == 0 {
		t.Error("expected at least 1 stream.end notification")
	}
}

func TestStreaming_HandlerEmitsContextStatus(t *testing.T) {
	mock := &streamingTestCompleter{
		chunkSets: [][]llm.StreamEvent{
			{
				{Delta: "ok"},
				{FinishReason: "stop"},
			},
		},
	}

	var buf bytes.Buffer
	srv := New(io.NopCloser(bytes.NewReader(nil)), &buf)
	h := newTestHandlersWithServer(t, mock, srv)
	h.RegisterAll()

	cfg := mustJSON(t, protocol.AgentConfigureParams{
		Streaming: &protocol.StreamingConfig{
			Enabled:       boolp(true),
			ContextStatus: boolp(true),
		},
	})
	if _, rpcErr := h.handleAgentConfigure(context.Background(), cfg); rpcErr != nil {
		t.Fatalf("configure: %+v", rpcErr)
	}

	runParams := mustJSON(t, protocol.AgentRunParams{Prompt: "hi"})
	if _, rpcErr := h.handleAgentRun(context.Background(), runParams); rpcErr != nil {
		t.Fatalf("run: %+v", rpcErr)
	}

	notifs := readNotifications(t, &buf)
	var statusCount int
	for _, n := range notifs {
		if n.Method == protocol.MethodContextStatus {
			statusCount++
			var p protocol.ContextStatusParams
			if err := json.Unmarshal(n.Params, &p); err != nil {
				t.Fatalf("unmarshal status: %v", err)
			}
			if p.TokenLimit <= 0 {
				t.Errorf("TokenLimit = %d, want > 0", p.TokenLimit)
			}
		}
	}
	if statusCount < 2 {
		t.Errorf("context.status notifications = %d, want >= 2", statusCount)
	}
}

func TestStreaming_DisabledNoDeltaNotifications(t *testing.T) {
	mock := &streamingTestCompleter{
		chunkSets: [][]llm.StreamEvent{
			{{Delta: "ok"}, {FinishReason: "stop"}},
		},
	}

	var buf bytes.Buffer
	srv := New(io.NopCloser(bytes.NewReader(nil)), &buf)
	h := newTestHandlersWithServer(t, mock, srv)
	h.RegisterAll()

	// Streaming は configure しない → 通常パスで ChatCompletion が使われる
	runParams := mustJSON(t, protocol.AgentRunParams{Prompt: "hi"})
	if _, rpcErr := h.handleAgentRun(context.Background(), runParams); rpcErr != nil {
		t.Fatalf("run: %+v", rpcErr)
	}

	notifs := readNotifications(t, &buf)
	for _, n := range notifs {
		if n.Method == protocol.MethodStreamDelta {
			t.Errorf("unexpected stream.delta notification: %s", string(n.Params))
		}
	}
}
