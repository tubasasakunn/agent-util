package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// mockInputGuard はテスト用の InputGuard。
type mockInputGuard struct {
	name    string
	results []*GuardResult
	err     error
	calls   int
}

func (m *mockInputGuard) Name() string { return m.name }
func (m *mockInputGuard) CheckInput(_ context.Context, _ string) (*GuardResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	i := m.calls
	m.calls++
	if i < len(m.results) {
		return m.results[i], nil
	}
	return &GuardResult{Decision: GuardAllow, Reason: "default pass"}, nil
}

// mockToolCallGuard はテスト用の ToolCallGuard。
type mockToolCallGuard struct {
	name    string
	results []*GuardResult
	err     error
	calls   int
}

func (m *mockToolCallGuard) Name() string { return m.name }
func (m *mockToolCallGuard) CheckToolCall(_ context.Context, _ string, _ json.RawMessage) (*GuardResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	i := m.calls
	m.calls++
	if i < len(m.results) {
		return m.results[i], nil
	}
	return &GuardResult{Decision: GuardAllow, Reason: "default pass"}, nil
}

// mockOutputGuard はテスト用の OutputGuard。
type mockOutputGuard struct {
	name    string
	results []*GuardResult
	err     error
	calls   int
}

func (m *mockOutputGuard) Name() string { return m.name }
func (m *mockOutputGuard) CheckOutput(_ context.Context, _ string) (*GuardResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	i := m.calls
	m.calls++
	if i < len(m.results) {
		return m.results[i], nil
	}
	return &GuardResult{Decision: GuardAllow, Reason: "default pass"}, nil
}

func TestGuardRegistry_RunInput(t *testing.T) {
	tests := []struct {
		name     string
		guards   []*mockInputGuard
		wantDec  GuardDecision
		wantSub  string
	}{
		{
			name:    "no guards returns allow",
			guards:  nil,
			wantDec: GuardAllow,
		},
		{
			name: "single guard allows",
			guards: []*mockInputGuard{
				{name: "g1", results: []*GuardResult{{Decision: GuardAllow, Reason: "ok"}}},
			},
			wantDec: GuardAllow,
		},
		{
			name: "single guard denies",
			guards: []*mockInputGuard{
				{name: "g1", results: []*GuardResult{{Decision: GuardDeny, Reason: "bad input"}}},
			},
			wantDec: GuardDeny,
			wantSub: "bad input",
		},
		{
			name: "single guard tripwire",
			guards: []*mockInputGuard{
				{name: "g1", results: []*GuardResult{{Decision: GuardTripwire, Reason: "injection detected"}}},
			},
			wantDec: GuardTripwire,
			wantSub: "injection detected",
		},
		{
			name: "multiple guards all allow",
			guards: []*mockInputGuard{
				{name: "g1", results: []*GuardResult{{Decision: GuardAllow}}},
				{name: "g2", results: []*GuardResult{{Decision: GuardAllow}}},
			},
			wantDec: GuardAllow,
		},
		{
			name: "first deny stops execution",
			guards: []*mockInputGuard{
				{name: "g1", results: []*GuardResult{{Decision: GuardAllow}}},
				{name: "g2", results: []*GuardResult{{Decision: GuardDeny, Reason: "blocked"}}},
			},
			wantDec: GuardDeny,
			wantSub: "blocked",
		},
		{
			name: "error skipped and continues",
			guards: []*mockInputGuard{
				{name: "g1", err: errors.New("broken guard")},
				{name: "g2", results: []*GuardResult{{Decision: GuardAllow}}},
			},
			wantDec: GuardAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewGuardRegistry()
			for _, g := range tt.guards {
				reg.AddInput(g)
			}

			result := reg.RunInput(context.Background(), "test input", nopLogf)
			if result.Decision != tt.wantDec {
				t.Errorf("Decision = %d, want %d", result.Decision, tt.wantDec)
			}
			if tt.wantSub != "" && result.Reason != tt.wantSub {
				t.Errorf("Reason = %q, want %q", result.Reason, tt.wantSub)
			}
		})
	}
}

func TestGuardRegistry_RunToolCall(t *testing.T) {
	tests := []struct {
		name    string
		guards  []*mockToolCallGuard
		wantDec GuardDecision
	}{
		{
			name:    "no guards returns allow",
			guards:  nil,
			wantDec: GuardAllow,
		},
		{
			name: "deny stops execution",
			guards: []*mockToolCallGuard{
				{name: "g1", results: []*GuardResult{{Decision: GuardDeny, Reason: "unsafe args"}}},
			},
			wantDec: GuardDeny,
		},
		{
			name: "tripwire stops execution",
			guards: []*mockToolCallGuard{
				{name: "g1", results: []*GuardResult{{Decision: GuardTripwire, Reason: "dangerous"}}},
			},
			wantDec: GuardTripwire,
		},
		{
			name: "error skipped",
			guards: []*mockToolCallGuard{
				{name: "g1", err: errors.New("broken")},
			},
			wantDec: GuardAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewGuardRegistry()
			for _, g := range tt.guards {
				reg.AddToolCall(g)
			}

			result := reg.RunToolCall(context.Background(), "shell", json.RawMessage(`{}`), nopLogf)
			if result.Decision != tt.wantDec {
				t.Errorf("Decision = %d, want %d", result.Decision, tt.wantDec)
			}
		})
	}
}

func TestGuardRegistry_RunOutput(t *testing.T) {
	tests := []struct {
		name    string
		guards  []*mockOutputGuard
		wantDec GuardDecision
	}{
		{
			name:    "no guards returns allow",
			guards:  nil,
			wantDec: GuardAllow,
		},
		{
			name: "deny blocks output",
			guards: []*mockOutputGuard{
				{name: "g1", results: []*GuardResult{{Decision: GuardDeny, Reason: "sensitive content"}}},
			},
			wantDec: GuardDeny,
		},
		{
			name: "tripwire on output",
			guards: []*mockOutputGuard{
				{name: "g1", results: []*GuardResult{{Decision: GuardTripwire, Reason: "data leak"}}},
			},
			wantDec: GuardTripwire,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewGuardRegistry()
			for _, g := range tt.guards {
				reg.AddOutput(g)
			}

			result := reg.RunOutput(context.Background(), "test output", nopLogf)
			if result.Decision != tt.wantDec {
				t.Errorf("Decision = %d, want %d", result.Decision, tt.wantDec)
			}
		})
	}
}

func TestGuardRegistry_HasGuards(t *testing.T) {
	reg := NewGuardRegistry()
	if reg.HasGuards() {
		t.Error("empty registry should return false")
	}

	reg.AddInput(&mockInputGuard{name: "g1"})
	if !reg.HasGuards() {
		t.Error("registry with input guard should return true")
	}
}

func TestGuardRegistry_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reg := NewGuardRegistry()
	reg.AddInput(&mockInputGuard{name: "g1", results: []*GuardResult{{Decision: GuardAllow}}})

	result := reg.RunInput(ctx, "test", nopLogf)
	if result.Decision != GuardDeny {
		t.Errorf("canceled context should return GuardDeny, got %d", result.Decision)
	}
}

func TestGuardRegistry_Accessors(t *testing.T) {
	reg := NewGuardRegistry()
	ig := &mockInputGuard{name: "ig1"}
	tg := &mockToolCallGuard{name: "tg1"}
	og := &mockOutputGuard{name: "og1"}

	reg.AddInput(ig)
	reg.AddToolCall(tg)
	reg.AddOutput(og)

	if len(reg.InputGuards()) != 1 || reg.InputGuards()[0].Name() != "ig1" {
		t.Error("InputGuards() mismatch")
	}
	if len(reg.ToolCallGuards()) != 1 || reg.ToolCallGuards()[0].Name() != "tg1" {
		t.Error("ToolCallGuards() mismatch")
	}
	if len(reg.OutputGuards()) != 1 || reg.OutputGuards()[0].Name() != "og1" {
		t.Error("OutputGuards() mismatch")
	}
}
