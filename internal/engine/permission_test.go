package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// mockUserApprover はテスト用の UserApprover。
type mockUserApprover struct {
	responses []bool
	calls     int
	err       error
}

func (m *mockUserApprover) Approve(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	i := m.calls
	m.calls++
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return false, nil
}

func TestPermissionChecker_Check(t *testing.T) {
	ctx := context.Background()
	args := json.RawMessage(`{"path":"test.go"}`)

	tests := []struct {
		name     string
		policy   PermissionPolicy
		approver UserApprover
		tool     *mockTool
		want     PermissionDecision
	}{
		{
			name: "deny rule matches exact tool name",
			policy: PermissionPolicy{
				DenyRules: []PermissionRule{{ToolName: "shell", Reason: "blocked"}},
			},
			tool: &mockTool{name: "shell"},
			want: PermDenied,
		},
		{
			name: "deny rule wildcard matches all tools",
			policy: PermissionPolicy{
				DenyRules: []PermissionRule{{ToolName: "*", Reason: "all blocked"}},
			},
			tool: &mockTool{name: "read_file", readOnly: true},
			want: PermDenied,
		},
		{
			name: "deny rule does not match different tool",
			policy: PermissionPolicy{
				DenyRules:  []PermissionRule{{ToolName: "shell", Reason: "blocked"}},
				AllowRules: []PermissionRule{{ToolName: "read_file", Reason: "allowed"}},
			},
			tool: &mockTool{name: "read_file", readOnly: true},
			want: PermAllowed,
		},
		{
			name: "allow rule matches exact tool name",
			policy: PermissionPolicy{
				AllowRules: []PermissionRule{{ToolName: "echo", Reason: "safe"}},
			},
			tool: &mockTool{name: "echo"},
			want: PermAllowed,
		},
		{
			name: "allow rule wildcard matches all tools",
			policy: PermissionPolicy{
				AllowRules: []PermissionRule{{ToolName: "*", Reason: "all allowed"}},
			},
			tool: &mockTool{name: "shell"},
			want: PermAllowed,
		},
		{
			name: "deny takes priority over allow for same tool",
			policy: PermissionPolicy{
				DenyRules:  []PermissionRule{{ToolName: "shell", Reason: "blocked"}},
				AllowRules: []PermissionRule{{ToolName: "shell", Reason: "allowed"}},
			},
			tool: &mockTool{name: "shell"},
			want: PermDenied,
		},
		{
			name:   "read only tool auto-approved when no rules match",
			policy: PermissionPolicy{},
			tool:   &mockTool{name: "read_file", readOnly: true},
			want:   PermAllowed,
		},
		{
			name:     "user approver approves",
			policy:   PermissionPolicy{},
			approver: &mockUserApprover{responses: []bool{true}},
			tool:     &mockTool{name: "shell"},
			want:     PermAllowed,
		},
		{
			name:     "user approver rejects",
			policy:   PermissionPolicy{},
			approver: &mockUserApprover{responses: []bool{false}},
			tool:     &mockTool{name: "shell"},
			want:     PermDenied,
		},
		{
			name:     "user approver error falls back to denied",
			policy:   PermissionPolicy{},
			approver: &mockUserApprover{err: errors.New("stdin closed")},
			tool:     &mockTool{name: "shell"},
			want:     PermDenied,
		},
		{
			name:   "fail-closed when no approver and no rules match for non-readonly tool",
			policy: PermissionPolicy{},
			tool:   &mockTool{name: "shell"},
			want:   PermDenied,
		},
		{
			name: "deny checked before allow",
			policy: PermissionPolicy{
				DenyRules:  []PermissionRule{{ToolName: "*", Reason: "deny all"}},
				AllowRules: []PermissionRule{{ToolName: "*", Reason: "allow all"}},
			},
			tool: &mockTool{name: "shell"},
			want: PermDenied,
		},
		{
			name: "allow checked before read-only auto-approve",
			policy: PermissionPolicy{
				AllowRules: []PermissionRule{{ToolName: "write_file", Reason: "explicitly allowed"}},
			},
			tool: &mockTool{name: "write_file", readOnly: false},
			want: PermAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audit := NewAuditLogger(nil) // テストではログ出力を抑制
			pc := NewPermissionChecker(tt.policy, tt.approver, audit)
			got := pc.Check(ctx, tt.tool, args)
			if got != tt.want {
				t.Errorf("Check() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPermissionChecker_AuditLogging(t *testing.T) {
	ctx := context.Background()
	args := json.RawMessage(`{}`)

	// AuditLogger が呼ばれることを確認（パニックしないことの確認）
	audit := NewAuditLogger(nil)
	pc := NewPermissionChecker(PermissionPolicy{}, nil, audit)
	got := pc.Check(ctx, &mockTool{name: "shell"}, args)
	if got != PermDenied {
		t.Errorf("expected PermDenied for fail-closed, got %d", got)
	}
}

func TestPermissionChecker_Policy(t *testing.T) {
	policy := PermissionPolicy{
		DenyRules:  []PermissionRule{{ToolName: "shell", Reason: "blocked"}},
		AllowRules: []PermissionRule{{ToolName: "echo", Reason: "safe"}},
	}
	pc := NewPermissionChecker(policy, nil, NewAuditLogger(nil))
	got := pc.Policy()

	if len(got.DenyRules) != 1 || got.DenyRules[0].ToolName != "shell" {
		t.Errorf("Policy().DenyRules = %v, want [{shell blocked}]", got.DenyRules)
	}
	if len(got.AllowRules) != 1 || got.AllowRules[0].ToolName != "echo" {
		t.Errorf("Policy().AllowRules = %v, want [{echo safe}]", got.AllowRules)
	}
}
