package engine

import (
	"context"
	"encoding/json"
)

// GuardDecision はガードレールの判定結果を表す。
type GuardDecision int

const (
	// GuardAllow は許可。
	GuardAllow GuardDecision = iota
	// GuardDeny は拒否。
	GuardDeny
	// GuardTripwire はトリップワイヤ（エージェントループの即時停止）。
	GuardTripwire
)

// GuardResult はガードレールの判定結果。
type GuardResult struct {
	Decision GuardDecision
	Reason   string   // 人間向けの説明
	Details  []string // 個別チェック項目の結果
}

// InputGuard はユーザー入力を検証するガードレール。
type InputGuard interface {
	Name() string
	CheckInput(ctx context.Context, input string) (*GuardResult, error)
}

// ToolCallGuard はツール呼び出しの事前チェックを行うガードレール。
// パーミッションパイプラインとは独立した補助的チェック（引数の安全性検証等）。
type ToolCallGuard interface {
	Name() string
	CheckToolCall(ctx context.Context, toolName string, args json.RawMessage) (*GuardResult, error)
}

// OutputGuard は最終出力の安全性を検証するガードレール。
type OutputGuard interface {
	Name() string
	CheckOutput(ctx context.Context, output string) (*GuardResult, error)
}
