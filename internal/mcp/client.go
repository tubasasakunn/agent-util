package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Client は MCP サーバーと JSON-RPC 2.0 で通信するクライアント。
// Transport インターフェースを介して stdio / SSE 等の通信方式に対応する。
type Client struct {
	transport Transport
	mu        sync.Mutex
	nextID    int
}

// ServerConfig は MCP サーバーの接続設定。
type ServerConfig struct {
	// Transport は通信方式。"stdio"（デフォルト）または "sse"。
	Transport string `json:"transport,omitempty"`

	// stdio 用: サブプロセスとして起動
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// sse 用: HTTP SSE エンドポイントに接続
	URL string `json:"url,omitempty"`
}

// rpcRequest は JSON-RPC 2.0 リクエスト。
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      *int            `json:"id,omitempty"`
}

// rpcResponse は JSON-RPC 2.0 レスポンス。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      *int            `json:"id"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolInfo は MCP サーバーが公開するツール情報。
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ToolResult は MCP ツール実行結果。
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock は MCP のコンテンツブロック。
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Connect は ServerConfig に基づいて MCP サーバーに接続する。
func Connect(ctx context.Context, cfg ServerConfig) (*Client, error) {
	transport := cfg.Transport
	if transport == "" {
		// command があれば stdio、url があれば sse
		if cfg.Command != "" {
			transport = "stdio"
		} else if cfg.URL != "" {
			transport = "sse"
		} else {
			return nil, fmt.Errorf("either command or url must be specified")
		}
	}

	var tr Transport
	var err error

	switch transport {
	case "stdio":
		if cfg.Command == "" {
			return nil, fmt.Errorf("command is required for stdio transport")
		}
		tr, err = NewStdioTransport(ctx, cfg.Command, cfg.Args, cfg.Env)
	case "sse":
		if cfg.URL == "" {
			return nil, fmt.Errorf("url is required for sse transport")
		}
		tr, err = NewSSETransport(ctx, cfg.URL)
	default:
		return nil, fmt.Errorf("unknown transport: %q (supported: stdio, sse)", transport)
	}

	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", transport, err)
	}

	return &Client{transport: tr}, nil
}

// Initialize は MCP の初期化ハンドシェイクを実行する。
func (c *Client) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "ai-agent",
			"version": "1.0.0",
		},
	}

	var result json.RawMessage
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	return c.notify("notifications/initialized", nil)
}

// ListTools は MCP サーバーのツール一覧を取得する。
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	var result struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", nil, &result); err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	return result.Tools, nil
}

// CallTool は MCP ツールを実行する。
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}

	var result ToolResult
	if err := c.call(ctx, "tools/call", params, &result); err != nil {
		return nil, fmt.Errorf("tools/call %q: %w", name, err)
	}
	return &result, nil
}

// Close は MCP サーバーとの接続を閉じる。
func (c *Client) Close() error {
	return c.transport.Close()
}

// call は JSON-RPC リクエストを送信し、レスポンスを待つ。
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	id := c.nextRequestID()

	var paramsJSON json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		paramsJSON = data
	}

	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
		ID:      &id,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	if err := c.transport.Send(data); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := c.transport.Receive()
		if err != nil {
			return fmt.Errorf("receive: %w", err)
		}

		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}

		if resp.ID == nil {
			continue
		}

		if *resp.ID != id {
			continue
		}

		if resp.Error != nil {
			return fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		if result != nil {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	}
}

// notify は通知（ID なし）を送信する。
func (c *Client) notify(method string, params any) error {
	var paramsJSON json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		paramsJSON = data
	}

	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return c.transport.Send(data)
}

// nextRequestID は次のリクエスト ID を採番する。
func (c *Client) nextRequestID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return c.nextID
}
