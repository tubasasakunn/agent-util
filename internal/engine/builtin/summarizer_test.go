package builtin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ai-agent/internal/llm"
)

type stubCompleter struct {
	gotPrompt string
	resp      string
	err       error
	noChoices bool
}

func (s *stubCompleter) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(req.Messages) > 0 {
		s.gotPrompt = req.Messages[0].ContentString()
	}
	if s.noChoices {
		return &llm.ChatResponse{Choices: nil}, nil
	}
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{Message: llm.Message{Role: "assistant", Content: llm.StringPtr(s.resp)}},
		},
	}, nil
}

func TestLLMSummarizer_BuildsPromptAndReturnsContent(t *testing.T) {
	stub := &stubCompleter{resp: "  要約結果  "}
	sum := NewLLMSummarizer(stub, "test-model")

	got, err := sum(context.Background(), []llm.Message{
		{Role: "user", Content: llm.StringPtr("hello")},
		{Role: "assistant", Content: llm.StringPtr("hi")},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "要約結果" {
		t.Errorf("expected trimmed result, got %q", got)
	}
	if !strings.Contains(stub.gotPrompt, "[user] hello") {
		t.Errorf("prompt should include user history, got: %s", stub.gotPrompt)
	}
	if !strings.Contains(stub.gotPrompt, "[assistant] hi") {
		t.Errorf("prompt should include assistant history, got: %s", stub.gotPrompt)
	}
}

func TestLLMSummarizer_PropagatesError(t *testing.T) {
	stub := &stubCompleter{err: errors.New("boom")}
	sum := NewLLMSummarizer(stub, "")
	_, err := sum(context.Background(), []llm.Message{
		{Role: "user", Content: llm.StringPtr("hello")},
	})
	if err == nil || !strings.Contains(err.Error(), "llm summarizer") {
		t.Errorf("expected wrapped error, got %v", err)
	}
}

func TestLLMSummarizer_EmptyChoicesError(t *testing.T) {
	stub := &stubCompleter{noChoices: true}
	sum := NewLLMSummarizer(stub, "")
	if _, err := sum(context.Background(), []llm.Message{
		{Role: "user", Content: llm.StringPtr("hi")},
	}); err == nil {
		t.Error("expected error when choices empty")
	}
}
