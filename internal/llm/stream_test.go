package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// streamSSE は OpenAI 互換 SSE 応答を返すテスト用ハンドラを生成する。
func streamSSE(chunks []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
	}
}

func TestChatCompletionStream_DeltasInOrder(t *testing.T) {
	chunks := []string{
		`{"choices":[{"delta":{"role":"assistant"},"index":0}]}`,
		`{"choices":[{"delta":{"content":"Hello"},"index":0}]}`,
		`{"choices":[{"delta":{"content":", "},"index":0}]}`,
		`{"choices":[{"delta":{"content":"world"},"index":0}]}`,
		`{"choices":[{"delta":{"content":"!"},"finish_reason":"stop","index":0}]}`,
		`[DONE]`,
	}
	server := httptest.NewServer(streamSSE(chunks))
	defer server.Close()

	client := NewClient(WithEndpoint(server.URL), WithMaxRetries(0))
	ch, err := client.ChatCompletionStream(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: StringPtr("hi")}},
	})
	if err != nil {
		t.Fatalf("ChatCompletionStream: %v", err)
	}

	var deltas []string
	var finish string
	for evt := range ch {
		if evt.Err != nil {
			t.Fatalf("stream error: %v", evt.Err)
		}
		if evt.Delta != "" {
			deltas = append(deltas, evt.Delta)
		}
		if evt.FinishReason != "" {
			finish = evt.FinishReason
		}
	}
	got := strings.Join(deltas, "")
	if got != "Hello, world!" {
		t.Errorf("joined deltas = %q, want %q", got, "Hello, world!")
	}
	if finish != "stop" {
		t.Errorf("finish = %q, want stop", finish)
	}
	if len(deltas) != 4 {
		t.Errorf("delta count = %d, want 4", len(deltas))
	}
}

func TestChatCompletionStream_RequestSetsStream(t *testing.T) {
	var received atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":true`) {
			t.Errorf("body should contain stream:true, got %s", string(body))
		}
		received.Store(true)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n",
			`{"choices":[{"delta":{"content":"x"},"finish_reason":"stop","index":0}]}`)
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(WithEndpoint(server.URL), WithMaxRetries(0))
	ch, err := client.ChatCompletionStream(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: StringPtr("x")}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range ch {
	}
	if !received.Load() {
		t.Error("server did not receive request")
	}
}

func TestChatCompletionStream_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	client := NewClient(WithEndpoint(server.URL), WithMaxRetries(0))
	_, err := client.ChatCompletionStream(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: StringPtr("x")}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("status = %d, want 401", apiErr.StatusCode)
	}
}

func TestChatCompletionStream_InvalidChunk(t *testing.T) {
	chunks := []string{
		`{"choices":[{"delta":{"content":"ok"},"index":0}]}`,
		`{not json`,
	}
	server := httptest.NewServer(streamSSE(chunks))
	defer server.Close()

	client := NewClient(WithEndpoint(server.URL), WithMaxRetries(0))
	ch, err := client.ChatCompletionStream(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: StringPtr("x")}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var sawError bool
	var sawDelta bool
	for evt := range ch {
		if evt.Err != nil {
			sawError = true
		}
		if evt.Delta != "" {
			sawDelta = true
		}
	}
	if !sawDelta {
		t.Error("expected to receive delta before error")
	}
	if !sawError {
		t.Error("expected to receive error event")
	}
}

func TestChatCompletionStream_ContextCanceled(t *testing.T) {
	// サーバはチャンクを少しずつ送る。クライアント側でキャンセルする。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := 0; i < 50; i++ {
			fmt.Fprintf(w, "data: %s\n\n",
				`{"choices":[{"delta":{"content":"x"},"index":0}]}`)
			flusher.Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer server.Close()

	client := NewClient(WithEndpoint(server.URL), WithMaxRetries(0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := client.ChatCompletionStream(ctx, &ChatRequest{
		Messages: []Message{{Role: "user", Content: StringPtr("x")}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	// 最初のチャンクを受け取ってキャンセル
	got := 0
	for evt := range ch {
		_ = evt
		got++
		if got == 1 {
			cancel()
		}
	}
	if got < 1 {
		t.Errorf("expected to receive at least 1 event, got %d", got)
	}
}

// StreamingCompleter インターフェースを Client が満たすことのコンパイル時チェック
var _ StreamingCompleter = (*Client)(nil)
