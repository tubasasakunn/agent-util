package engine

import (
	"context"
	"strings"
	"testing"

	"ai-agent/internal/llm"
)

func TestRouterSystemPrompt_ContainsToolList(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockTool("echo", "Echoes a message"))
	reg.Register(newMockTool("read_file", "Reads a file"))

	eng := &Engine{
		systemPrompt: "You are a helpful assistant.",
		registry:     reg,
	}

	prompt := eng.routerSystemPrompt()

	if !strings.Contains(prompt, "You are a helpful assistant.") {
		t.Error("missing base system prompt")
	}
	if !strings.Contains(prompt, "### echo") {
		t.Error("missing echo tool")
	}
	if !strings.Contains(prompt, "### read_file") {
		t.Error("missing read_file tool")
	}
	if !strings.Contains(prompt, `"tool"`) {
		t.Error("missing output schema instruction")
	}
}

func TestRouterSystemPrompt_EmptySystemPrompt(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockTool("echo", "Echoes"))

	eng := &Engine{
		systemPrompt: "",
		registry:     reg,
	}

	prompt := eng.routerSystemPrompt()

	if strings.HasPrefix(prompt, "\n") {
		t.Error("should not start with newline when systemPrompt is empty")
	}
	if !strings.Contains(prompt, "### echo") {
		t.Error("missing echo tool")
	}
}

func TestRouterStep_SelectsTool(t *testing.T) {
	routerJSON := `{"tool":"read_file","arguments":{"path":"test.txt"},"reasoning":"user wants to read a file"}`
	mc := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(routerJSON),
		},
	}

	reg := NewRegistry()
	reg.Register(newMockTool("read_file", "Reads a file"))

	eng := &Engine{
		completer: mc,
		registry:  reg,
		messages:  []llm.Message{UserMessage("read test.txt")},
	}

	rr, usage, err := eng.routerStep(context.Background())
	if err != nil {
		t.Fatalf("routerStep error: %v", err)
	}
	if rr.Tool != "read_file" {
		t.Errorf("Tool = %q, want %q", rr.Tool, "read_file")
	}
	if string(rr.Arguments) != `{"path":"test.txt"}` {
		t.Errorf("Arguments = %s, want %s", rr.Arguments, `{"path":"test.txt"}`)
	}
	if rr.Reasoning == "" {
		t.Error("Reasoning is empty")
	}
	if usage == nil {
		t.Error("Usage is nil")
	}
}

func TestRouterStep_SelectsNone(t *testing.T) {
	routerJSON := `{"tool":"none","arguments":{},"reasoning":"simple math, no tool needed"}`
	mc := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(routerJSON),
		},
	}

	eng := &Engine{
		completer: mc,
		registry:  NewRegistry(),
		messages:  []llm.Message{UserMessage("1+1は?")},
	}

	rr, _, err := eng.routerStep(context.Background())
	if err != nil {
		t.Fatalf("routerStep error: %v", err)
	}
	if rr.Tool != "none" {
		t.Errorf("Tool = %q, want %q", rr.Tool, "none")
	}
}

func TestRouterStep_EmptyToolFallsBackToNone(t *testing.T) {
	routerJSON := `{"tool":"","arguments":{},"reasoning":"no tool"}`
	mc := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(routerJSON),
		},
	}

	eng := &Engine{
		completer: mc,
		registry:  NewRegistry(),
		messages:  []llm.Message{UserMessage("hello")},
	}

	rr, _, err := eng.routerStep(context.Background())
	if err != nil {
		t.Fatalf("routerStep error: %v", err)
	}
	if rr.Tool != "none" {
		t.Errorf("Tool = %q, want %q", rr.Tool, "none")
	}
}

func TestRouterStep_APIError(t *testing.T) {
	mc := &mockCompleter{
		err: &llm.APIError{StatusCode: 500, Body: "internal server error"},
	}

	eng := &Engine{
		completer: mc,
		registry:  NewRegistry(),
		messages:  []llm.Message{UserMessage("test")},
	}

	_, _, err := eng.routerStep(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "router step") {
		t.Errorf("error = %q, should contain 'router step'", err.Error())
	}
}

func TestBuildRouterMessages(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockTool("echo", "Echoes"))

	eng := &Engine{
		systemPrompt: "base prompt",
		registry:     reg,
		messages: []llm.Message{
			UserMessage("hello"),
			AssistantMessage("hi there"),
		},
	}

	msgs := eng.buildRouterMessages()

	// system + 2 conversation messages
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("msgs[0].Role = %q, want %q", msgs[0].Role, "system")
	}
	if msgs[1].Role != "user" {
		t.Errorf("msgs[1].Role = %q, want %q", msgs[1].Role, "user")
	}
	if msgs[2].Role != "assistant" {
		t.Errorf("msgs[2].Role = %q, want %q", msgs[2].Role, "assistant")
	}
}

func TestRouterStep_ResponseFormatIsJSON(t *testing.T) {
	mc := &mockCompleter{
		responses: []*llm.ChatResponse{
			chatResponse(`{"tool":"none","arguments":{},"reasoning":"test"}`),
		},
	}

	eng := &Engine{
		completer: mc,
		registry:  NewRegistry(),
		messages:  []llm.Message{UserMessage("test")},
	}

	eng.routerStep(context.Background())

	if len(mc.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(mc.requests))
	}
	req := mc.requests[0]
	if req.ResponseFormat == nil {
		t.Fatal("ResponseFormat is nil")
	}
	if req.ResponseFormat.Type != "json_object" {
		t.Errorf("ResponseFormat.Type = %q, want %q", req.ResponseFormat.Type, "json_object")
	}
}
