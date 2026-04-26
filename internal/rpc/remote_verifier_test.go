package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"ai-agent/internal/engine"
	"ai-agent/pkg/protocol"
)

var _ engine.Verifier = (*RemoteVerifier)(nil)

func TestRemoteVerifier_Pass(t *testing.T) {
	stub := &stubSender{
		resp: &protocol.Response{
			Result: encodeResult(t, protocol.VerifierExecuteResult{
				Passed:  true,
				Summary: "all good",
				Details: []string{"check1: ok"},
			}),
		},
	}
	v := NewRemoteVerifier("custom_lint", stub)

	res, err := v.Verify(context.Background(), "write_file", []byte(`{"path":"a.go"}`), "package main")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Passed {
		t.Errorf("Passed = false")
	}
	if res.Summary != "all good" {
		t.Errorf("Summary = %q", res.Summary)
	}
	if stub.method != protocol.MethodVerifierExecute {
		t.Errorf("method = %q", stub.method)
	}
	if stub.verifier.ToolName != "write_file" {
		t.Errorf("tool_name = %q", stub.verifier.ToolName)
	}
	if stub.verifier.Result != "package main" {
		t.Errorf("result = %q", stub.verifier.Result)
	}
}

func TestRemoteVerifier_Fail(t *testing.T) {
	stub := &stubSender{
		resp: &protocol.Response{
			Result: encodeResult(t, protocol.VerifierExecuteResult{
				Passed:  false,
				Summary: "missing import",
			}),
		},
	}
	v := NewRemoteVerifier("custom_lint", stub)

	res, _ := v.Verify(context.Background(), "write_file", []byte(`{}`), "")
	if res.Passed {
		t.Errorf("Passed = true, want false")
	}
	if res.Summary != "missing import" {
		t.Errorf("Summary = %q", res.Summary)
	}
}

func TestRemoteVerifier_FailClosedOnTransportError(t *testing.T) {
	stub := &stubSender{err: errors.New("broken")}
	v := NewRemoteVerifier("custom", stub)

	res, err := v.Verify(context.Background(), "x", nil, "")
	if err != nil {
		t.Fatalf("expected nil error (fail-closed), got %v", err)
	}
	if res.Passed {
		t.Errorf("Passed = true, want false (fail-closed)")
	}
}

func TestRemoteVerifier_FailClosedOnRPCError(t *testing.T) {
	stub := &stubSender{
		resp: &protocol.Response{
			Error: &protocol.RPCError{Code: -32000, Message: "wrapper crashed"},
		},
	}
	v := NewRemoteVerifier("custom", stub)
	res, _ := v.Verify(context.Background(), "x", nil, "")
	if res.Passed {
		t.Errorf("Passed = true, want false")
	}
}

func TestRemoteVerifier_FailClosedOnTimeout(t *testing.T) {
	stub := &stubSender{
		delay: 200 * time.Millisecond,
		resp: &protocol.Response{
			Result: encodeResult(t, protocol.VerifierExecuteResult{Passed: true}),
		},
	}
	v := NewRemoteVerifier("slow", stub)
	v.timeout = 30 * time.Millisecond

	res, err := v.Verify(context.Background(), "x", nil, "")
	if err != nil {
		t.Fatalf("expected nil error (fail-closed), got %v", err)
	}
	if res.Passed {
		t.Errorf("Passed = true, want false (timeout)")
	}
}

func TestHandleVerifierRegister(t *testing.T) {
	h := newTestHandlers(t, &testCompleter{})

	params := mustJSON(t, protocol.VerifierRegisterParams{
		Verifiers: []protocol.VerifierDefinition{
			{Name: "lint"},
			{Name: "test_runner"},
		},
	})
	res, rpcErr := h.handleVerifierRegister(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("register: %+v", rpcErr)
	}
	r := res.(protocol.VerifierRegisterResult)
	if r.Registered != 2 {
		t.Errorf("Registered = %d, want 2", r.Registered)
	}
	if _, ok := h.RemoteRegistry().LookupVerifier("lint"); !ok {
		t.Error("lint not registered")
	}
	if _, ok := h.RemoteRegistry().LookupVerifier("test_runner"); !ok {
		t.Error("test_runner not registered")
	}
}

func TestHandleVerifierRegister_EmptyName(t *testing.T) {
	h := newTestHandlers(t, &testCompleter{})
	params := mustJSON(t, protocol.VerifierRegisterParams{
		Verifiers: []protocol.VerifierDefinition{{Name: ""}},
	})
	_, rpcErr := h.handleVerifierRegister(context.Background(), params)
	if rpcErr == nil {
		t.Fatal("expected error for empty name")
	}
}
