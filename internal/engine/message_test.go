package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolCallMessage(t *testing.T) {
	args := json.RawMessage(`{"path":"test.txt"}`)
	msg := ToolCallMessage("call_abc123", "read_file", args)

	if msg.Role != "assistant" {
		t.Errorf("Role = %q, want %q", msg.Role, "assistant")
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("ID = %q, want %q", tc.ID, "call_abc123")
	}
	if tc.Type != "function" {
		t.Errorf("Type = %q, want %q", tc.Type, "function")
	}
	if tc.Function.Name != "read_file" {
		t.Errorf("Function.Name = %q, want %q", tc.Function.Name, "read_file")
	}

	// Arguments はOpenAI互換のJSON文字列形式であること
	// 例: "{\"path\":\"test.txt\"}" (JSON string)
	var argsStr string
	if err := json.Unmarshal(tc.Function.Arguments, &argsStr); err != nil {
		t.Fatalf("Arguments should be a JSON string, got: %s", tc.Function.Arguments)
	}
	if argsStr != `{"path":"test.txt"}` {
		t.Errorf("Arguments string = %q, want %q", argsStr, `{"path":"test.txt"}`)
	}
}

func TestToolResultMessage(t *testing.T) {
	msg := ToolResultMessage("call_abc123", "file contents here")

	if msg.Role != "tool" {
		t.Errorf("Role = %q, want %q", msg.Role, "tool")
	}
	if msg.ContentString() != "file contents here" {
		t.Errorf("Content = %q, want %q", msg.ContentString(), "file contents here")
	}
	if msg.ToolCallID != "call_abc123" {
		t.Errorf("ToolCallID = %q, want %q", msg.ToolCallID, "call_abc123")
	}
}

func TestGenerateCallID(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateCallID()
		if !strings.HasPrefix(id, "call_") {
			t.Errorf("ID %q missing call_ prefix", id)
		}
		if seen[id] {
			t.Errorf("duplicate ID: %q", id)
		}
		seen[id] = true
	}
}
