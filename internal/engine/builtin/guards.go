// Package builtin はハーネスの組み込みガード/Verifier/Summarizerを提供する。
// 名前選択でラッパーから利用できるよう、各カテゴリにレジストリを公開する。
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"ai-agent/internal/engine"
)

// --- InputGuard ---

// PromptInjectionGuard はプロンプトインジェクション攻撃の典型パターンを検出する。
type PromptInjectionGuard struct{}

// Name は識別名を返す。
func (g *PromptInjectionGuard) Name() string { return "prompt_injection" }

var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?|context)`),
	regexp.MustCompile(`(?i)disregard\s+(\w+\s+)?(previous|prior|above)`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+a\s+`),
	regexp.MustCompile(`(?i)^\s*system\s*[:：]\s*`),
	regexp.MustCompile(`(?i)reveal\s+(your\s+)?system\s+prompt`),
}

// CheckInput はユーザー入力をパターンマッチで検証する。
func (g *PromptInjectionGuard) CheckInput(ctx context.Context, input string) (*engine.GuardResult, error) {
	for _, p := range injectionPatterns {
		if p.MatchString(input) {
			return &engine.GuardResult{
				Decision: engine.GuardDeny,
				Reason:   "potential prompt injection pattern detected",
				Details:  []string{p.String()},
			}, nil
		}
	}
	return &engine.GuardResult{Decision: engine.GuardAllow}, nil
}

// MaxLengthGuard は入力長の上限を強制する。
type MaxLengthGuard struct {
	Max int
}

// Name は識別名を返す。
func (g *MaxLengthGuard) Name() string { return "max_length" }

// CheckInput は入力の長さを検証する。
func (g *MaxLengthGuard) CheckInput(ctx context.Context, input string) (*engine.GuardResult, error) {
	if g.Max > 0 && len(input) > g.Max {
		return &engine.GuardResult{
			Decision: engine.GuardDeny,
			Reason:   fmt.Sprintf("input exceeds max length (%d > %d)", len(input), g.Max),
		}, nil
	}
	return &engine.GuardResult{Decision: engine.GuardAllow}, nil
}

// --- ToolCallGuard ---

// DangerousShellGuard は shell 系ツール呼び出しに含まれる破壊的コマンドを検出する。
type DangerousShellGuard struct{}

// Name は識別名を返す。
func (g *DangerousShellGuard) Name() string { return "dangerous_shell" }

var dangerousShellPatterns = []*regexp.Regexp{
	regexp.MustCompile(`rm\s+-rf?\s+(/|~|\$HOME|\*)`),
	regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}`), // fork bomb
	regexp.MustCompile(`\bmkfs\.`),
	regexp.MustCompile(`\bdd\s+if=.*of=/dev/`),
	regexp.MustCompile(`chmod\s+-R\s+777\s+/`),
	regexp.MustCompile(`>\s*/dev/sd[a-z]`),
	regexp.MustCompile(`\bshutdown\b`),
	regexp.MustCompile(`\breboot\b`),
}

func looksLikeShellTool(name string) bool {
	n := strings.ToLower(name)
	return n == "bash" || n == "sh" || strings.Contains(n, "shell") || strings.Contains(n, "exec")
}

// CheckToolCall は shell 系ツールの引数を検査する。
func (g *DangerousShellGuard) CheckToolCall(ctx context.Context, toolName string, args json.RawMessage) (*engine.GuardResult, error) {
	if !looksLikeShellTool(toolName) {
		return &engine.GuardResult{Decision: engine.GuardAllow}, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(args, &parsed); err != nil {
		return &engine.GuardResult{Decision: engine.GuardAllow}, nil
	}
	cmd := pickStringField(parsed, "command", "cmd", "script")
	for _, p := range dangerousShellPatterns {
		if p.MatchString(cmd) {
			return &engine.GuardResult{
				Decision: engine.GuardDeny,
				Reason:   "dangerous shell pattern detected",
				Details:  []string{p.String()},
			}, nil
		}
	}
	return &engine.GuardResult{Decision: engine.GuardAllow}, nil
}

func pickStringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok {
			return v
		}
	}
	return ""
}

// --- OutputGuard ---

// SecretLeakGuard は出力に含まれる代表的な API キー/トークンの形式を検出する。
type SecretLeakGuard struct{}

// Name は識別名を返す。
func (g *SecretLeakGuard) Name() string { return "secret_leak" }

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),          // OpenAI 形式
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`),   // Anthropic
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),             // AWS access key
	regexp.MustCompile(`ghp_[A-Za-z0-9]{30,}`),         // GitHub PAT
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`), // Slack
	regexp.MustCompile(`-----BEGIN (RSA|OPENSSH|EC|PGP|DSA) PRIVATE KEY-----`),
}

// CheckOutput は最終出力を検査する。
func (g *SecretLeakGuard) CheckOutput(ctx context.Context, output string) (*engine.GuardResult, error) {
	var matched []string
	for _, p := range secretPatterns {
		if p.MatchString(output) {
			matched = append(matched, p.String())
		}
	}
	if len(matched) > 0 {
		return &engine.GuardResult{
			Decision: engine.GuardDeny,
			Reason:   "potential secret leak detected",
			Details:  matched,
		}, nil
	}
	return &engine.GuardResult{Decision: engine.GuardAllow}, nil
}
