package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ai-agent/internal/engine"
	"ai-agent/pkg/protocol"
)

// DefaultGuardTimeout はリモートガード呼び出しのデフォルトタイムアウト。
const DefaultGuardTimeout = 30 * time.Second

// remoteSender はリモート呼び出しに必要な最小インターフェース。
// テストで Server を差し替えやすくするための境界。
type remoteSender interface {
	SendRequest(ctx context.Context, method string, params any) (*protocol.Response, error)
}

// RemoteInputGuard はラッパー側で実装された InputGuard のプロキシ。
// Engine の InputGuard インターフェースを実装する。
type RemoteInputGuard struct {
	name    string
	server  remoteSender
	timeout time.Duration
}

// NewRemoteInputGuard は RemoteInputGuard を生成する。
func NewRemoteInputGuard(name string, server remoteSender) *RemoteInputGuard {
	return &RemoteInputGuard{name: name, server: server, timeout: DefaultGuardTimeout}
}

// Name は識別名を返す。
func (g *RemoteInputGuard) Name() string { return g.name }

// CheckInput はラッパーへ guard.execute を送り、応答を engine.GuardResult に変換する。
func (g *RemoteInputGuard) CheckInput(ctx context.Context, input string) (*engine.GuardResult, error) {
	params := protocol.GuardExecuteParams{
		Name:  g.name,
		Stage: protocol.GuardStageInput,
		Input: input,
	}
	return g.execute(ctx, params)
}

func (g *RemoteInputGuard) execute(ctx context.Context, params protocol.GuardExecuteParams) (*engine.GuardResult, error) {
	return executeRemoteGuard(ctx, g.server, g.name, g.timeout, params)
}

// RemoteToolCallGuard はラッパー側で実装された ToolCallGuard のプロキシ。
type RemoteToolCallGuard struct {
	name    string
	server  remoteSender
	timeout time.Duration
}

// NewRemoteToolCallGuard は RemoteToolCallGuard を生成する。
func NewRemoteToolCallGuard(name string, server remoteSender) *RemoteToolCallGuard {
	return &RemoteToolCallGuard{name: name, server: server, timeout: DefaultGuardTimeout}
}

// Name は識別名を返す。
func (g *RemoteToolCallGuard) Name() string { return g.name }

// CheckToolCall はラッパーへ guard.execute を送る。
func (g *RemoteToolCallGuard) CheckToolCall(ctx context.Context, toolName string, args json.RawMessage) (*engine.GuardResult, error) {
	params := protocol.GuardExecuteParams{
		Name:     g.name,
		Stage:    protocol.GuardStageToolCall,
		ToolName: toolName,
		Args:     args,
	}
	return executeRemoteGuard(ctx, g.server, g.name, g.timeout, params)
}

// RemoteOutputGuard はラッパー側で実装された OutputGuard のプロキシ。
type RemoteOutputGuard struct {
	name    string
	server  remoteSender
	timeout time.Duration
}

// NewRemoteOutputGuard は RemoteOutputGuard を生成する。
func NewRemoteOutputGuard(name string, server remoteSender) *RemoteOutputGuard {
	return &RemoteOutputGuard{name: name, server: server, timeout: DefaultGuardTimeout}
}

// Name は識別名を返す。
func (g *RemoteOutputGuard) Name() string { return g.name }

// CheckOutput はラッパーへ guard.execute を送る。
func (g *RemoteOutputGuard) CheckOutput(ctx context.Context, output string) (*engine.GuardResult, error) {
	params := protocol.GuardExecuteParams{
		Name:   g.name,
		Stage:  protocol.GuardStageOutput,
		Output: output,
	}
	return executeRemoteGuard(ctx, g.server, g.name, g.timeout, params)
}

// executeRemoteGuard はリモートガード呼び出しの共通処理。
// fail-closed: エラー / 不正な応答は GuardDeny を返す（CheckInput 等の error は nil のまま）。
func executeRemoteGuard(ctx context.Context, server remoteSender, name string, timeout time.Duration, params protocol.GuardExecuteParams) (*engine.GuardResult, error) {
	if server == nil {
		return failClosedGuard(name, "remote server unavailable"), nil
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := server.SendRequest(execCtx, protocol.MethodGuardExecute, params)
	if err != nil {
		return failClosedGuard(name, fmt.Sprintf("transport: %s", err)), nil
	}
	if resp == nil {
		return failClosedGuard(name, "empty response"), nil
	}
	if resp.Error != nil {
		return failClosedGuard(name, fmt.Sprintf("rpc error: %s", resp.Error.Message)), nil
	}

	var result protocol.GuardExecuteResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return failClosedGuard(name, fmt.Sprintf("decode result: %s", err)), nil
	}

	decision, err := decisionFromString(result.Decision)
	if err != nil {
		return failClosedGuard(name, err.Error()), nil
	}

	return &engine.GuardResult{
		Decision: decision,
		Reason:   result.Reason,
		Details:  result.Details,
	}, nil
}

// failClosedGuard はリモートエラー時に返す deny 結果を生成する。
func failClosedGuard(name, reason string) *engine.GuardResult {
	return &engine.GuardResult{
		Decision: engine.GuardDeny,
		Reason:   fmt.Sprintf("remote guard %q error: %s", name, reason),
	}
}

func decisionFromString(s string) (engine.GuardDecision, error) {
	switch s {
	case protocol.GuardDecisionAllow:
		return engine.GuardAllow, nil
	case protocol.GuardDecisionDeny:
		return engine.GuardDeny, nil
	case protocol.GuardDecisionTripwire:
		return engine.GuardTripwire, nil
	default:
		return engine.GuardDeny, fmt.Errorf("unknown decision %q", s)
	}
}
