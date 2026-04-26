package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"ai-agent/internal/engine"
	"ai-agent/pkg/protocol"
)

// インターフェース実装のコンパイル時検証。
var (
	_ engine.InputGuard    = (*RemoteInputGuard)(nil)
	_ engine.ToolCallGuard = (*RemoteToolCallGuard)(nil)
	_ engine.OutputGuard   = (*RemoteOutputGuard)(nil)
)

// stubSender はテスト用のリモート呼び出しスタブ。
type stubSender struct {
	resp     *protocol.Response
	err      error
	called   int
	method   string
	params   protocol.GuardExecuteParams
	verifier protocol.VerifierExecuteParams
	delay    time.Duration
}

func (s *stubSender) SendRequest(ctx context.Context, method string, params any) (*protocol.Response, error) {
	s.called++
	s.method = method
	switch p := params.(type) {
	case protocol.GuardExecuteParams:
		s.params = p
	case protocol.VerifierExecuteParams:
		s.verifier = p
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.resp, s.err
}

func encodeResult(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}

func TestRemoteInputGuard_Allow(t *testing.T) {
	stub := &stubSender{
		resp: &protocol.Response{
			Result: encodeResult(t, protocol.GuardExecuteResult{Decision: protocol.GuardDecisionAllow}),
		},
	}
	g := NewRemoteInputGuard("my_input", stub)

	res, err := g.CheckInput(context.Background(), "hello")
	if err != nil {
		t.Fatalf("CheckInput: %v", err)
	}
	if res.Decision != engine.GuardAllow {
		t.Errorf("Decision = %v, want allow", res.Decision)
	}
	if stub.method != protocol.MethodGuardExecute {
		t.Errorf("method = %q", stub.method)
	}
	if stub.params.Stage != protocol.GuardStageInput {
		t.Errorf("stage = %q", stub.params.Stage)
	}
	if stub.params.Input != "hello" {
		t.Errorf("input = %q", stub.params.Input)
	}
}

func TestRemoteInputGuard_Deny(t *testing.T) {
	stub := &stubSender{
		resp: &protocol.Response{
			Result: encodeResult(t, protocol.GuardExecuteResult{
				Decision: protocol.GuardDecisionDeny,
				Reason:   "blocked by ml classifier",
				Details:  []string{"score=0.95"},
			}),
		},
	}
	g := NewRemoteInputGuard("ml_classifier", stub)

	res, err := g.CheckInput(context.Background(), "evil prompt")
	if err != nil {
		t.Fatalf("CheckInput: %v", err)
	}
	if res.Decision != engine.GuardDeny {
		t.Errorf("Decision = %v, want deny", res.Decision)
	}
	if res.Reason != "blocked by ml classifier" {
		t.Errorf("Reason = %q", res.Reason)
	}
	if len(res.Details) != 1 || res.Details[0] != "score=0.95" {
		t.Errorf("Details = %v", res.Details)
	}
}

func TestRemoteInputGuard_Tripwire(t *testing.T) {
	stub := &stubSender{
		resp: &protocol.Response{
			Result: encodeResult(t, protocol.GuardExecuteResult{
				Decision: protocol.GuardDecisionTripwire,
				Reason:   "policy violation",
			}),
		},
	}
	g := NewRemoteInputGuard("policy", stub)
	res, _ := g.CheckInput(context.Background(), "anything")
	if res.Decision != engine.GuardTripwire {
		t.Errorf("Decision = %v, want tripwire", res.Decision)
	}
}

func TestRemoteInputGuard_FailClosedOnTransportError(t *testing.T) {
	stub := &stubSender{err: errors.New("pipe broken")}
	g := NewRemoteInputGuard("network", stub)

	res, err := g.CheckInput(context.Background(), "x")
	if err != nil {
		t.Fatalf("expected nil error (fail-closed), got %v", err)
	}
	if res.Decision != engine.GuardDeny {
		t.Errorf("Decision = %v, want deny (fail-closed)", res.Decision)
	}
	if res.Reason == "" {
		t.Errorf("Reason should describe failure")
	}
}

func TestRemoteInputGuard_FailClosedOnRPCError(t *testing.T) {
	stub := &stubSender{
		resp: &protocol.Response{
			Error: &protocol.RPCError{Code: -32000, Message: "wrapper crashed"},
		},
	}
	g := NewRemoteInputGuard("wrapper", stub)

	res, _ := g.CheckInput(context.Background(), "x")
	if res.Decision != engine.GuardDeny {
		t.Errorf("Decision = %v, want deny", res.Decision)
	}
}

func TestRemoteInputGuard_FailClosedOnUnknownDecision(t *testing.T) {
	stub := &stubSender{
		resp: &protocol.Response{
			Result: encodeResult(t, protocol.GuardExecuteResult{Decision: "maybe"}),
		},
	}
	g := NewRemoteInputGuard("buggy", stub)

	res, _ := g.CheckInput(context.Background(), "x")
	if res.Decision != engine.GuardDeny {
		t.Errorf("Decision = %v, want deny", res.Decision)
	}
}

func TestRemoteInputGuard_FailClosedOnTimeout(t *testing.T) {
	stub := &stubSender{
		delay: 200 * time.Millisecond,
		resp: &protocol.Response{
			Result: encodeResult(t, protocol.GuardExecuteResult{Decision: protocol.GuardDecisionAllow}),
		},
	}
	g := NewRemoteInputGuard("slow", stub)
	g.timeout = 30 * time.Millisecond

	res, err := g.CheckInput(context.Background(), "x")
	if err != nil {
		t.Fatalf("expected nil error (fail-closed), got %v", err)
	}
	if res.Decision != engine.GuardDeny {
		t.Errorf("Decision = %v, want deny (timeout)", res.Decision)
	}
}

func TestRemoteToolCallGuard_PassesArgs(t *testing.T) {
	stub := &stubSender{
		resp: &protocol.Response{
			Result: encodeResult(t, protocol.GuardExecuteResult{Decision: protocol.GuardDecisionAllow}),
		},
	}
	g := NewRemoteToolCallGuard("policy", stub)

	args := json.RawMessage(`{"path":"/etc/passwd"}`)
	if _, err := g.CheckToolCall(context.Background(), "read_file", args); err != nil {
		t.Fatalf("CheckToolCall: %v", err)
	}
	if stub.params.Stage != protocol.GuardStageToolCall {
		t.Errorf("stage = %q", stub.params.Stage)
	}
	if stub.params.ToolName != "read_file" {
		t.Errorf("tool_name = %q", stub.params.ToolName)
	}
	if string(stub.params.Args) != string(args) {
		t.Errorf("args = %s", stub.params.Args)
	}
}

func TestRemoteOutputGuard_PassesOutput(t *testing.T) {
	stub := &stubSender{
		resp: &protocol.Response{
			Result: encodeResult(t, protocol.GuardExecuteResult{Decision: protocol.GuardDecisionDeny, Reason: "PII"}),
		},
	}
	g := NewRemoteOutputGuard("pii", stub)

	res, _ := g.CheckOutput(context.Background(), "ssn=123-45-6789")
	if res.Decision != engine.GuardDeny {
		t.Errorf("Decision = %v, want deny", res.Decision)
	}
	if stub.params.Stage != protocol.GuardStageOutput {
		t.Errorf("stage = %q", stub.params.Stage)
	}
	if stub.params.Output != "ssn=123-45-6789" {
		t.Errorf("output = %q", stub.params.Output)
	}
}

func TestHandleGuardRegister_AllStages(t *testing.T) {
	h := newTestHandlers(t, &testCompleter{})

	params := mustJSON(t, protocol.GuardRegisterParams{
		Guards: []protocol.GuardDefinition{
			{Name: "in1", Stage: protocol.GuardStageInput},
			{Name: "tc1", Stage: protocol.GuardStageToolCall},
			{Name: "out1", Stage: protocol.GuardStageOutput},
		},
	})
	res, rpcErr := h.handleGuardRegister(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("register: %+v", rpcErr)
	}
	r := res.(protocol.GuardRegisterResult)
	if r.Registered != 3 {
		t.Errorf("Registered = %d, want 3", r.Registered)
	}

	if _, ok := h.RemoteRegistry().LookupInputGuard("in1"); !ok {
		t.Error("in1 not in registry")
	}
	if _, ok := h.RemoteRegistry().LookupToolCallGuard("tc1"); !ok {
		t.Error("tc1 not in registry")
	}
	if _, ok := h.RemoteRegistry().LookupOutputGuard("out1"); !ok {
		t.Error("out1 not in registry")
	}
}

func TestHandleGuardRegister_UnknownStage(t *testing.T) {
	h := newTestHandlers(t, &testCompleter{})
	params := mustJSON(t, protocol.GuardRegisterParams{
		Guards: []protocol.GuardDefinition{
			{Name: "x", Stage: "bogus"},
		},
	})
	_, rpcErr := h.handleGuardRegister(context.Background(), params)
	if rpcErr == nil {
		t.Fatal("expected error for unknown stage")
	}
	if rpcErr.Code != protocol.ErrCodeInvalidParams {
		t.Errorf("code = %d", rpcErr.Code)
	}
}

func TestHandleGuardRegister_EmptyName(t *testing.T) {
	h := newTestHandlers(t, &testCompleter{})
	params := mustJSON(t, protocol.GuardRegisterParams{
		Guards: []protocol.GuardDefinition{
			{Name: "", Stage: protocol.GuardStageInput},
		},
	})
	_, rpcErr := h.handleGuardRegister(context.Background(), params)
	if rpcErr == nil {
		t.Fatal("expected error for empty name")
	}
}
