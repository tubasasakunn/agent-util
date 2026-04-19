package readfile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"ai-agent/pkg/tool"
)

// Tool はファイルの内容を読み取るツール。
type Tool struct{}

// New は ReadFile ツールを生成する。
func New() *Tool { return &Tool{} }

func (t *Tool) Name() string        { return "read_file" }
func (t *Tool) Description() string  { return "Reads the contents of a file at the specified path." }
func (t *Tool) IsReadOnly() bool     { return true }
func (t *Tool) IsConcurrencySafe() bool { return true }

func (t *Tool) Parameters() json.RawMessage {
	return json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "The file path to read"
		}
	},
	"required": ["path"]
}`)
}

type readFileArgs struct {
	Path string `json:"path"`
}

func (t *Tool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a readFileArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if a.Path == "" {
		return tool.Result{Content: "path is required", IsError: true}, nil
	}

	data, err := os.ReadFile(a.Path)
	if err != nil {
		return tool.Result{
			Content: fmt.Sprintf("failed to read file: %s", err.Error()),
			IsError: true,
		}, nil
	}

	return tool.Result{
		Content: string(data),
		Metadata: map[string]any{
			"path":  a.Path,
			"bytes": len(data),
		},
	}, nil
}
