package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// mockVerifier はテスト用の Verifier。
type mockVerifier struct {
	name    string
	results []*VerifyResult
	err     error
	calls   int
}

func (v *mockVerifier) Name() string { return v.name }

func (v *mockVerifier) Verify(_ context.Context, _ string, _ json.RawMessage, _ string) (*VerifyResult, error) {
	if v.err != nil {
		return nil, v.err
	}
	i := v.calls
	v.calls++
	if i < len(v.results) {
		return v.results[i], nil
	}
	return &VerifyResult{Passed: true, Summary: "default pass"}, nil
}

func nopLogf(string, ...any) {}

func TestVerifierRegistry_AllPass(t *testing.T) {
	v1 := &mockVerifier{
		name:    "checker1",
		results: []*VerifyResult{{Passed: true, Summary: "ok"}},
	}
	v2 := &mockVerifier{
		name:    "checker2",
		results: []*VerifyResult{{Passed: true, Summary: "ok"}},
	}
	reg := NewVerifierRegistry(v1, v2)

	result := reg.RunAll(context.Background(), "echo", nil, "output", nopLogf)
	if !result.Passed {
		t.Errorf("expected Passed=true, got false")
	}
	if len(result.Details) != 2 {
		t.Errorf("details count = %d, want 2", len(result.Details))
	}
}

func TestVerifierRegistry_OneFails(t *testing.T) {
	v1 := &mockVerifier{
		name:    "good",
		results: []*VerifyResult{{Passed: true, Summary: "ok"}},
	}
	v2 := &mockVerifier{
		name:    "bad",
		results: []*VerifyResult{{Passed: false, Summary: "syntax error in output"}},
	}
	reg := NewVerifierRegistry(v1, v2)

	result := reg.RunAll(context.Background(), "shell", nil, "output", nopLogf)
	if result.Passed {
		t.Error("expected Passed=false when one verifier fails")
	}
	if result.Summary == "" {
		t.Error("summary should not be empty on failure")
	}
}

func TestVerifierRegistry_ErrorInVerifier(t *testing.T) {
	v1 := &mockVerifier{
		name: "broken",
		err:  errors.New("verifier crashed"),
	}
	v2 := &mockVerifier{
		name:    "working",
		results: []*VerifyResult{{Passed: true, Summary: "ok"}},
	}
	reg := NewVerifierRegistry(v1, v2)

	// 検証器自体のエラーはスキップされ、残りは続行される
	result := reg.RunAll(context.Background(), "echo", nil, "output", nopLogf)
	if !result.Passed {
		t.Error("expected Passed=true when only errored verifier is present (skipped)")
	}
	if len(result.Details) != 2 {
		t.Errorf("details count = %d, want 2 (error + pass)", len(result.Details))
	}
}

func TestVerifierRegistry_Empty(t *testing.T) {
	reg := NewVerifierRegistry()

	result := reg.RunAll(context.Background(), "echo", nil, "output", nopLogf)
	if !result.Passed {
		t.Error("empty registry should return Passed=true")
	}
}

func TestVerifierRegistry_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 即座にキャンセル

	v := &mockVerifier{
		name:    "should_not_run",
		results: []*VerifyResult{{Passed: true, Summary: "ok"}},
	}
	reg := NewVerifierRegistry(v)

	result := reg.RunAll(ctx, "echo", nil, "output", nopLogf)
	if result.Passed {
		t.Error("expected Passed=false when context is canceled")
	}
}
