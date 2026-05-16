package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ai-agent/pkg/protocol"
	"ai-agent/pkg/tool"
)

// DefaultToolTimeout はツール実行のデフォルトタイムアウト。
const DefaultToolTimeout = 30 * time.Second

// RemoteTool はラッパー側で実装されたツールのプロキシ。
// tool.Tool インターフェースを実装する。
type RemoteTool struct {
	def     tool.Definition
	server  *Server
	timeout time.Duration
}

// NewRemoteTool は RemoteTool を生成する。
func NewRemoteTool(def tool.Definition, server *Server) *RemoteTool {
	return &RemoteTool{
		def:     def,
		server:  server,
		timeout: DefaultToolTimeout,
	}
}

func (t *RemoteTool) Name() string                { return t.def.Name }
func (t *RemoteTool) Description() string         { return t.def.Description }
func (t *RemoteTool) Parameters() json.RawMessage { return t.def.Parameters }
func (t *RemoteTool) IsReadOnly() bool            { return t.def.ReadOnly }

// Execute はラッパーに tool.execute リクエストを送信し、結果を待つ。
func (t *RemoteTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	execCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	params := protocol.ToolExecuteParams{
		Name: t.def.Name,
		Args: args,
	}

	resp, err := t.server.SendRequest(execCtx, protocol.MethodToolExecute, params)
	if err != nil {
		return tool.Result{}, fmt.Errorf("remote tool %q: %w", t.def.Name, err)
	}

	if resp.Error != nil {
		return tool.Result{
			Content: resp.Error.Message,
			IsError: true,
		}, nil
	}

	var result protocol.ToolExecuteResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return tool.Result{}, fmt.Errorf("remote tool %q: unmarshal result: %w", t.def.Name, err)
	}

	return tool.Result{
		Content:  result.Content,
		IsError:  result.IsError,
		Metadata: result.Metadata,
	}, nil
}
