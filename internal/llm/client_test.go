package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestChatCompletion_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// リクエストの検証
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %s, want Bearer test-key", r.Header.Get("Authorization"))
		}

		body, _ := io.ReadAll(r.Body)
		var req ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.Model != "test-model" {
			t.Errorf("model = %s, want test-model", req.Model)
		}
		if len(req.Messages) != 1 || req.Messages[0].ContentString() != "hello" {
			t.Errorf("unexpected messages: %+v", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			ID:    "chatcmpl-001",
			Model: "test-model",
			Choices: []Choice{
				{
					Index:        0,
					Message:      Message{Role: "assistant", Content: StringPtr("こんにちは！")},
					FinishReason: "stop",
				},
			},
			Usage: Usage{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15},
		})
	}))
	defer server.Close()

	client := NewClient(
		WithEndpoint(server.URL),
		WithModel("test-model"),
		WithAPIKey("test-key"),
		WithMaxRetries(0),
	)

	resp, err := client.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{
			{Role: "user", Content: StringPtr("hello")},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("choices length = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.ContentString() != "こんにちは！" {
		t.Errorf("content = %q, want こんにちは！", resp.Choices[0].Message.ContentString())
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", resp.Choices[0].FinishReason)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("total_tokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestChatCompletion_JSONMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ChatRequest
		json.Unmarshal(body, &req)

		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Errorf("response_format = %+v, want json_object", req.ResponseFormat)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{
				{
					Message:      Message{Role: "assistant", Content: StringPtr(`{"name":"Tokyo Tower","height_m":333}`)},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(WithEndpoint(server.URL), WithMaxRetries(0))

	resp, err := client.ChatCompletion(context.Background(), &ChatRequest{
		Messages:       []Message{{Role: "user", Content: StringPtr("test")}},
		ResponseFormat: &ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	var result map[string]any
	if err := ParseContent(resp, &result); err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}
	if result["name"] != "Tokyo Tower" {
		t.Errorf("name = %v, want Tokyo Tower", result["name"])
	}
}

func TestChatCompletion_Retry(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{
				{Message: Message{Role: "assistant", Content: StringPtr("success")}},
			},
		})
	}))
	defer server.Close()

	client := NewClient(
		WithEndpoint(server.URL),
		WithMaxRetries(3),
		WithHTTPClient(&http.Client{}), // タイムアウトなし（テスト用）
	)

	resp, err := client.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: StringPtr("test")}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	if resp.Choices[0].Message.ContentString() != "success" {
		t.Errorf("content = %q, want success", resp.Choices[0].Message.ContentString())
	}
	if attempts.Load() != 3 {
		t.Errorf("attempts = %d, want 3", attempts.Load())
	}
}

func TestChatCompletion_NonRetryableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer server.Close()

	client := NewClient(
		WithEndpoint(server.URL),
		WithMaxRetries(3),
	)

	_, err := client.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: StringPtr("test")}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("status code = %d, want 401", apiErr.StatusCode)
	}
}

func TestChatCompletion_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// このハンドラは呼ばれないはず
		t.Error("handler should not be called")
	}))
	defer server.Close()

	client := NewClient(WithEndpoint(server.URL))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 即キャンセル

	_, err := client.ChatCompletion(ctx, &ChatRequest{
		Messages: []Message{{Role: "user", Content: StringPtr("test")}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestChatCompletion_ModelDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ChatRequest
		json.Unmarshal(body, &req)

		if req.Model != "my-model" {
			t.Errorf("model = %s, want my-model", req.Model)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{
				{Message: Message{Role: "assistant", Content: StringPtr("ok")}},
			},
		})
	}))
	defer server.Close()

	client := NewClient(
		WithEndpoint(server.URL),
		WithModel("my-model"),
		WithMaxRetries(0),
	)

	// Model 未指定でリクエスト → Client のデフォルトが使われる
	_, err := client.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: StringPtr("test")}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
}

func TestParseContent_FixJSON(t *testing.T) {
	// SLMが壊れたJSONを返したケースをシミュレート
	resp := &ChatResponse{
		Choices: []Choice{
			{
				Message: Message{
					Role:    "assistant",
					Content: StringPtr(`{"tool": "none", "arguments": {null}, "reasoning": "不要"}`),
				},
			},
		},
	}

	var result map[string]any
	if err := ParseContent(resp, &result); err != nil {
		t.Fatalf("ParseContent error: %v", err)
	}
	if result["tool"] != "none" {
		t.Errorf("tool = %v, want none", result["tool"])
	}
	if result["arguments"] != nil {
		t.Errorf("arguments = %v, want nil", result["arguments"])
	}
}

func TestParseContent_EmptyChoices(t *testing.T) {
	resp := &ChatResponse{Choices: []Choice{}}
	err := ParseContent(resp, &map[string]any{})
	if !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("expected ErrEmptyResponse, got: %v", err)
	}
}
