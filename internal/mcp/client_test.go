package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"ai-agent/pkg/tool"
)

// MCPTool が tool.Tool インターフェースを満たすことをコンパイル時に検証。
var _ tool.Tool = (*MCPTool)(nil)

func testMCPServerPath() string {
	return "/tmp/test_mcp_server"
}

func skipIfNoMCPServer(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(testMCPServerPath()); err != nil {
		t.Skipf("test MCP server not found at %s (build it first)", testMCPServerPath())
	}
}

func TestMCPClient_InitializeAndListTools(t *testing.T) {
	skipIfNoMCPServer(t)

	ctx := context.Background()
	client, err := Connect(ctx, ServerConfig{Command: testMCPServerPath()})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}

	names := make(map[string]bool)
	for _, ti := range tools {
		names[ti.Name] = true
	}
	if !names["read_file"] {
		t.Error("read_file tool not found")
	}
	if !names["list_directory"] {
		t.Error("list_directory tool not found")
	}
}

func TestMCPClient_CallTool(t *testing.T) {
	skipIfNoMCPServer(t)

	ctx := context.Background()
	client, err := Connect(ctx, ServerConfig{Command: testMCPServerPath()})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	cwd, _ := os.Getwd()
	result, err := client.CallTool(ctx, "list_directory", json.RawMessage(`{"path":"`+cwd+`"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("empty content")
	}
	if !strings.Contains(result.Content[0].Text, "client.go") {
		t.Errorf("expected client.go in listing, got: %s", result.Content[0].Text)
	}
}

func TestRegisterMCPServer(t *testing.T) {
	skipIfNoMCPServer(t)

	ctx := context.Background()
	client, tools, err := RegisterMCPServer(ctx, ServerConfig{Command: testMCPServerPath()})
	if err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}
	defer client.Close()

	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}

	var readFileTool tool.Tool
	for _, ti := range tools {
		if ti.Name() == "read_file" {
			readFileTool = ti
		}
	}
	if readFileTool == nil {
		t.Fatal("read_file tool not found")
	}

	dir, _ := os.Getwd()
	result, err := readFileTool.Execute(ctx, json.RawMessage(`{"path":"`+dir+`/client.go"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "package mcp") {
		t.Errorf("expected 'package mcp' in content, got: %.100s...", result.Content)
	}
}

func TestConnect_UnknownTransport(t *testing.T) {
	_, err := Connect(context.Background(), ServerConfig{Transport: "grpc"})
	if err == nil {
		t.Fatal("expected error for unknown transport")
	}
	if !strings.Contains(err.Error(), "unknown transport") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConnect_NoConfig(t *testing.T) {
	_, err := Connect(context.Background(), ServerConfig{})
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}
