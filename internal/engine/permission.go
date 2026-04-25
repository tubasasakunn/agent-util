package engine

import (
	"context"
	"encoding/json"
	"time"

	"ai-agent/pkg/tool"
)

// PermissionDecision はパーミッション判定の結果を表す。
type PermissionDecision int

const (
	// PermDenied は静的Denyルールに合致、またはfail-closedによる拒否。
	PermDenied PermissionDecision = iota
	// PermAllowed は静的Allowルールに合致、またはReadOnly自動承認。
	PermAllowed
	// PermAsk はユーザー確認が必要。
	PermAsk
)

// PermissionRule はパーミッションパイプラインの静的ルール。
type PermissionRule struct {
	ToolName string // ツール名。"*" で全ツールに合致する。
	Reason   string // 人間向けの説明。
}

// PermissionPolicy はdeny→allowの静的ルールセット。
type PermissionPolicy struct {
	DenyRules  []PermissionRule
	AllowRules []PermissionRule
}

// UserApprover はユーザーへの承認問い合わせのインターフェース。
// REPLモードではstdin/stdoutで確認、ワンショットモードでは自動拒否（nil）。
type UserApprover interface {
	Approve(ctx context.Context, toolName string, args json.RawMessage) (bool, error)
}

// PermissionChecker はパーミッションパイプラインを実行する。
// deny→allow→readOnly→ask→fail-closed の順に判定する。
type PermissionChecker struct {
	policy   PermissionPolicy
	approver UserApprover // nil の場合はask→deny（fail-closed）
	audit    *AuditLogger
}

// NewPermissionChecker は PermissionChecker を生成する。
func NewPermissionChecker(policy PermissionPolicy, approver UserApprover, audit *AuditLogger) *PermissionChecker {
	return &PermissionChecker{
		policy:   policy,
		approver: approver,
		audit:    audit,
	}
}

// Policy は現在のポリシーを返す。Fork() での継承用。
func (pc *PermissionChecker) Policy() PermissionPolicy {
	return pc.policy
}

// Check はツール実行の権限を判定する。
// パイプライン: deny→allow→readOnly→ask→fail-closed
func (pc *PermissionChecker) Check(ctx context.Context, t tool.Tool, args json.RawMessage) PermissionDecision {
	toolName := t.Name()
	readOnly := t.IsReadOnly()

	// 1. Denyリスト照合 → 早期拒否
	for _, rule := range pc.policy.DenyRules {
		if rule.ToolName == toolName || rule.ToolName == "*" {
			pc.logAudit(toolName, args, "denied", rule.Reason, readOnly)
			return PermDenied
		}
	}

	// 2. Allowリスト照合 → 即許可
	for _, rule := range pc.policy.AllowRules {
		if rule.ToolName == toolName || rule.ToolName == "*" {
			pc.logAudit(toolName, args, "allowed", rule.Reason, readOnly)
			return PermAllowed
		}
	}

	// 3. ReadOnlyツール → 自動承認
	if readOnly {
		pc.logAudit(toolName, args, "allowed", "read_only auto-approve", readOnly)
		return PermAllowed
	}

	// 4. UserApprover に委譲
	if pc.approver != nil {
		approved, err := pc.approver.Approve(ctx, toolName, args)
		if err != nil {
			pc.logAudit(toolName, args, "denied", "approver error: "+err.Error(), readOnly)
			return PermDenied
		}
		if approved {
			pc.logAudit(toolName, args, "user_approved", "user confirmed", readOnly)
			return PermAllowed
		}
		pc.logAudit(toolName, args, "user_rejected", "user rejected", readOnly)
		return PermDenied
	}

	// 5. fail-closed: 明示許可なしは拒否
	pc.logAudit(toolName, args, "denied", "fail-closed (no approver)", readOnly)
	return PermDenied
}

// logAudit は監査ログを記録する。
func (pc *PermissionChecker) logAudit(toolName string, args json.RawMessage, decision, reason string, readOnly bool) {
	pc.audit.Log(AuditEntry{
		Timestamp:  time.Now(),
		ToolName:   toolName,
		Args:       args,
		Decision:   decision,
		Reason:     reason,
		IsReadOnly: readOnly,
	})
}
