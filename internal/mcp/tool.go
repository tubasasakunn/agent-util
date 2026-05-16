package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ai-agent/pkg/tool"
)

// MCPTool は MCP サーバーのツールを tool.Tool インターフェースに適合させるアダプタ。
type MCPTool struct {
	info   ToolInfo
	client *Client
}

// NewMCPTool は MCPTool を生成する。
func NewMCPTool(info ToolInfo, client *Client) *MCPTool {
	return &MCPTool{info: info, client: client}
}

func (t *MCPTool) Name() string        { return t.info.Name }
func (t *MCPTool) Description() string { return t.info.Description }
func (t *MCPTool) IsReadOnly() bool    { return false }

// Parameters は MCP の inputSchema を JSON Schema として返す。
func (t *MCPTool) Parameters() json.RawMessage {
	if len(t.info.InputSchema) > 0 {
		return t.info.InputSchema
	}
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

// Execute は MCP サーバーに tools/call リクエストを送信し、結果を返す。
func (t *MCPTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	result, err := t.client.CallTool(ctx, t.info.Name, args)
	if err != nil {
		return tool.Result{}, fmt.Errorf("mcp tool %q: %w", t.info.Name, err)
	}

	var sb strings.Builder
	for i, block := range result.Content {
		if i > 0 {
			sb.WriteString("\n")
		}
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}

	return tool.Result{
		Content: sb.String(),
		IsError: result.IsError,
	}, nil
}

// RegisterMCPServer は MCP サーバーに接続し、ツールを発見して返す。
func RegisterMCPServer(ctx context.Context, cfg ServerConfig) (*Client, []tool.Tool, error) {
	client, err := Connect(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("connect mcp server: %w", err)
	}

	if err := client.Initialize(ctx); err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("initialize mcp server: %w", err)
	}

	infos, err := client.ListTools(ctx)
	if err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("list mcp tools: %w", err)
	}

	tools := make([]tool.Tool, len(infos))
	for i, info := range infos {
		tools[i] = NewMCPTool(info, client)
	}

	return client, tools, nil
}
