package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ai-agent/internal/engine"
	"ai-agent/pkg/protocol"
)

// DefaultVerifierTimeout はリモート Verifier 呼び出しのデフォルトタイムアウト。
const DefaultVerifierTimeout = 30 * time.Second

// RemoteVerifier はラッパー側で実装された Verifier のプロキシ。
// engine.Verifier インターフェースを実装する。
type RemoteVerifier struct {
	name    string
	server  remoteSender
	timeout time.Duration
}

// NewRemoteVerifier は RemoteVerifier を生成する。
func NewRemoteVerifier(name string, server remoteSender) *RemoteVerifier {
	return &RemoteVerifier{name: name, server: server, timeout: DefaultVerifierTimeout}
}

// Name は識別名を返す。
func (v *RemoteVerifier) Name() string { return v.name }

// Verify はラッパーへ verifier.execute を送り、応答を engine.VerifyResult に変換する。
// fail-closed: エラー時は Passed=false を返す（error は呼び出し元で握りつぶさず nil で返す）。
func (v *RemoteVerifier) Verify(ctx context.Context, toolName string, args []byte, result string) (*engine.VerifyResult, error) {
	if v.server == nil {
		return failClosedVerify(v.name, "remote server unavailable"), nil
	}

	execCtx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	params := protocol.VerifierExecuteParams{
		Name:     v.name,
		ToolName: toolName,
		Args:     json.RawMessage(args),
		Result:   result,
	}

	resp, err := v.server.SendRequest(execCtx, protocol.MethodVerifierExecute, params)
	if err != nil {
		return failClosedVerify(v.name, fmt.Sprintf("transport: %s", err)), nil
	}
	if resp == nil {
		return failClosedVerify(v.name, "empty response"), nil
	}
	if resp.Error != nil {
		return failClosedVerify(v.name, fmt.Sprintf("rpc error: %s", resp.Error.Message)), nil
	}

	var out protocol.VerifierExecuteResult
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return failClosedVerify(v.name, fmt.Sprintf("decode result: %s", err)), nil
	}

	return &engine.VerifyResult{
		Passed:  out.Passed,
		Summary: out.Summary,
		Details: out.Details,
	}, nil
}

func failClosedVerify(name, reason string) *engine.VerifyResult {
	return &engine.VerifyResult{
		Passed:  false,
		Summary: fmt.Sprintf("remote verifier %q error: %s", name, reason),
	}
}
