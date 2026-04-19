package echo

import (
	"context"
	"encoding/json"
	"testing"
)

func TestEcho_Execute(t *testing.T) {
	tool := New()
	args := json.RawMessage(`{"message":"hello world"}`)

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "hello world" {
		t.Errorf("Content = %q, want %q", result.Content, "hello world")
	}
	if result.IsError {
		t.Error("IsError should be false")
	}
}

func TestEcho_Execute_InvalidArgs(t *testing.T) {
	tool := New()
	args := json.RawMessage(`{invalid}`)

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("IsError should be true for invalid args")
	}
}

func TestEcho_Metadata(t *testing.T) {
	tool := New()

	if tool.Name() != "echo" {
		t.Errorf("Name = %q", tool.Name())
	}
	if !tool.IsReadOnly() {
		t.Error("should be read-only")
	}
	if !tool.IsConcurrencySafe() {
		t.Error("should be concurrency-safe")
	}
}
