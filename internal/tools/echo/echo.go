package echo

import (
	"context"
	"encoding/json"

	"ai-agent/pkg/tool"
)

// Tool はテスト・デバッグ用のechoツール。
type Tool struct{}

// New は Echo ツールを生成する。
func New() *Tool { return &Tool{} }

func (t *Tool) Name() string        { return "echo" }
func (t *Tool) Description() string { return "Echoes back the provided message. Useful for testing." }
func (t *Tool) IsReadOnly() bool    { return true }

func (t *Tool) Parameters() json.RawMessage {
	return json.RawMessage(`{
	"type": "object",
	"properties": {
		"message": {
			"type": "string",
			"description": "The message to echo back"
		}
	},
	"required": ["message"]
}`)
}

type echoArgs struct {
	Message string `json:"message"`
}

func (t *Tool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a echoArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Errorf("invalid arguments: %v", err), nil
	}
	return tool.OK(a.Message), nil
}
