package engine

import (
	"context"
	"encoding/json"
)

// GuardRegistry は3層ガードレールを管理する。
type GuardRegistry struct {
	inputGuards    []InputGuard
	toolCallGuards []ToolCallGuard
	outputGuards   []OutputGuard
}

// NewGuardRegistry は GuardRegistry を生成する。
func NewGuardRegistry() *GuardRegistry {
	return &GuardRegistry{}
}

// AddInput は入力ガードレールを追加する。
func (gr *GuardRegistry) AddInput(g InputGuard) {
	gr.inputGuards = append(gr.inputGuards, g)
}

// AddToolCall はツール呼び出しガードレールを追加する。
func (gr *GuardRegistry) AddToolCall(g ToolCallGuard) {
	gr.toolCallGuards = append(gr.toolCallGuards, g)
}

// AddOutput は出力ガードレールを追加する。
func (gr *GuardRegistry) AddOutput(g OutputGuard) {
	gr.outputGuards = append(gr.outputGuards, g)
}

// InputGuards は登録された入力ガードレールを返す。Fork() での継承用。
func (gr *GuardRegistry) InputGuards() []InputGuard {
	return gr.inputGuards
}

// ToolCallGuards は登録されたツール呼び出しガードレールを返す。Fork() での継承用。
func (gr *GuardRegistry) ToolCallGuards() []ToolCallGuard {
	return gr.toolCallGuards
}

// OutputGuards は登録された出力ガードレールを返す。Fork() での継承用。
func (gr *GuardRegistry) OutputGuards() []OutputGuard {
	return gr.outputGuards
}

// HasGuards はいずれかのガードレールが登録されているかを返す。
func (gr *GuardRegistry) HasGuards() bool {
	return len(gr.inputGuards) > 0 || len(gr.toolCallGuards) > 0 || len(gr.outputGuards) > 0
}

// guardWithName はガード名を返すメソッドを持つ型制約。
type guardWithName interface {
	Name() string
}

// runGuards はガードリストを順次実行する汎用ヘルパー。
// 最初の Deny または Tripwire で早期リターンする。
// ガード自体のエラーはログしてスキップする（ガードの障害で本体を止めない）。
func runGuards[G guardWithName](
	ctx context.Context,
	guards []G,
	check func(G) (*GuardResult, error),
	logf func(string, ...any),
	allPassedMsg string,
) *GuardResult {
	for _, g := range guards {
		select {
		case <-ctx.Done():
			return &GuardResult{Decision: GuardDeny, Reason: "guard canceled"}
		default:
		}

		result, err := check(g)
		if err != nil {
			logf("[guard] %s error (skipped): %s", g.Name(), err)
			continue
		}
		if result.Decision != GuardAllow {
			return result
		}
	}
	return &GuardResult{Decision: GuardAllow, Reason: allPassedMsg}
}

// RunInput は入力ガードレールを順次実行する。
func (gr *GuardRegistry) RunInput(ctx context.Context, input string, logf func(string, ...any)) *GuardResult {
	return runGuards(ctx, gr.inputGuards, func(g InputGuard) (*GuardResult, error) {
		return g.CheckInput(ctx, input)
	}, logf, "all input guards passed")
}

// RunToolCall はツール呼び出しガードレールを順次実行する。
func (gr *GuardRegistry) RunToolCall(ctx context.Context, toolName string, args json.RawMessage, logf func(string, ...any)) *GuardResult {
	return runGuards(ctx, gr.toolCallGuards, func(g ToolCallGuard) (*GuardResult, error) {
		return g.CheckToolCall(ctx, toolName, args)
	}, logf, "all tool call guards passed")
}

// RunOutput は出力ガードレールを順次実行する。
func (gr *GuardRegistry) RunOutput(ctx context.Context, output string, logf func(string, ...any)) *GuardResult {
	return runGuards(ctx, gr.outputGuards, func(g OutputGuard) (*GuardResult, error) {
		return g.CheckOutput(ctx, output)
	}, logf, "all output guards passed")
}
