// テスト用 MCP サーバー: filesystem ツール（read_file, list_directory）を提供する。
// MCP (Model Context Protocol) 準拠の JSON-RPC 2.0 over stdio サーバー。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      *int            `json:"id,omitempty"`
}

type response struct {
	JSONRPC string `json:"jsonrpc"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
	ID      *int   `json:"id"`
}

func writeJSON(v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintf(os.Stdout, "%s\n", data)
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		// 通知は無視
		if req.ID == nil {
			continue
		}

		switch req.Method {
		case "initialize":
			writeJSON(response{
				JSONRPC: "2.0",
				Result: map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]string{"name": "test-filesystem", "version": "1.0.0"},
				},
				ID: req.ID,
			})

		case "tools/list":
			writeJSON(response{
				JSONRPC: "2.0",
				Result: map[string]any{
					"tools": []map[string]any{
						{
							"name":        "read_file",
							"description": "Read the complete contents of a file from the file system. Only works within allowed directories.",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"path": map[string]any{
										"type":        "string",
										"description": "Absolute path to the file to read",
									},
								},
								"required": []string{"path"},
							},
						},
						{
							"name":        "list_directory",
							"description": "Get a detailed listing of all files and directories in a specified path.",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"path": map[string]any{
										"type":        "string",
										"description": "Absolute path to the directory to list",
									},
								},
								"required": []string{"path"},
							},
						},
					},
				},
				ID: req.ID,
			})

		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			json.Unmarshal(req.Params, &params)

			content, isErr := executeTool(params.Name, params.Arguments)
			writeJSON(response{
				JSONRPC: "2.0",
				Result: map[string]any{
					"content": []map[string]string{{"type": "text", "text": content}},
					"isError": isErr,
				},
				ID: req.ID,
			})

		default:
			writeJSON(response{
				JSONRPC: "2.0",
				Error:   map[string]any{"code": -32601, "message": "Method not found: " + req.Method},
				ID:      req.ID,
			})
		}
	}
}

func executeTool(name string, argsJSON json.RawMessage) (string, bool) {
	var args struct {
		Path string `json:"path"`
	}
	json.Unmarshal(argsJSON, &args)

	switch name {
	case "read_file":
		data, err := os.ReadFile(args.Path)
		if err != nil {
			return fmt.Sprintf("Error reading file: %s", err), true
		}
		content := string(data)
		if len(content) > 3000 {
			content = content[:3000] + "\n... (truncated)"
		}
		return content, false

	case "list_directory":
		entries, err := os.ReadDir(args.Path)
		if err != nil {
			return fmt.Sprintf("Error listing directory: %s", err), true
		}
		var sb strings.Builder
		for _, e := range entries {
			if e.IsDir() {
				sb.WriteString("[DIR]  " + e.Name() + "\n")
			} else {
				info, _ := e.Info()
				size := int64(0)
				if info != nil {
					size = info.Size()
				}
				sb.WriteString(fmt.Sprintf("[FILE] %s (%d bytes)\n", e.Name(), size))
			}
		}
		return sb.String(), false

	default:
		return fmt.Sprintf("Unknown tool: %s", name), true
	}
}
