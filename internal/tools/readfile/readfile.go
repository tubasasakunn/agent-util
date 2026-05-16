package readfile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"ai-agent/pkg/tool"
)

// Tool はファイルの内容を読み取るツール。
type Tool struct{}

// New は ReadFile ツールを生成する。
func New() *Tool { return &Tool{} }

func (t *Tool) Name() string        { return "read_file" }
func (t *Tool) Description() string { return "Reads the contents of a file at the specified path." }
func (t *Tool) IsReadOnly() bool    { return true }

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

func (t *Tool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	var a readFileArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Errorf("invalid arguments: %v", err), nil
	}
	if a.Path == "" {
		return tool.Errorf("path is required"), nil
	}

	// workDir が設定されていて相対パスの場合、workDir を基準に解決する
	path := a.Path
	if workDir := tool.WorkDirFromContext(ctx); workDir != "" && !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return tool.Errorf("failed to read file: %s", err), nil
	}

	return tool.Result{
		Content: string(data),
		Metadata: map[string]any{
			"path":  path,
			"bytes": len(data),
		},
	}, nil
}
