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

// RunInput は入力ガードレールを順次実行する。
// 最初の Deny または Tripwire で早期リターンする。
// ガードレール自体のエラーはログしてスキップする（ガードの障害で本体を止めない）。
func (gr *GuardRegistry) RunInput(ctx context.Context, input string, logf func(string, ...any)) *GuardResult {
	for _, g := range gr.inputGuards {
		select {
		case <-ctx.Done():
			return &GuardResult{Decision: GuardDeny, Reason: "guard canceled"}
		default:
		}

		result, err := g.CheckInput(ctx, input)
		if err != nil {
			logf("[guard] %s error (skipped): %s", g.Name(), err)
			continue
		}
		if result.Decision != GuardAllow {
			return result
		}
	}
	return &GuardResult{Decision: GuardAllow, Reason: "all input guards passed"}
}

// RunToolCall はツール呼び出しガードレールを順次実行する。
// 最初の Deny または Tripwire で早期リターンする。
func (gr *GuardRegistry) RunToolCall(ctx context.Context, toolName string, args json.RawMessage, logf func(string, ...any)) *GuardResult {
	for _, g := range gr.toolCallGuards {
		select {
		case <-ctx.Done():
			return &GuardResult{Decision: GuardDeny, Reason: "guard canceled"}
		default:
		}

		result, err := g.CheckToolCall(ctx, toolName, args)
		if err != nil {
			logf("[guard] %s error (skipped): %s", g.Name(), err)
			continue
		}
		if result.Decision != GuardAllow {
			return result
		}
	}
	return &GuardResult{Decision: GuardAllow, Reason: "all tool call guards passed"}
}

// RunOutput は出力ガードレールを順次実行する。
// 最初の Deny または Tripwire で早期リターンする。
func (gr *GuardRegistry) RunOutput(ctx context.Context, output string, logf func(string, ...any)) *GuardResult {
	for _, g := range gr.outputGuards {
		select {
		case <-ctx.Done():
			return &GuardResult{Decision: GuardDeny, Reason: "guard canceled"}
		default:
		}

		result, err := g.CheckOutput(ctx, output)
		if err != nil {
			logf("[guard] %s error (skipped): %s", g.Name(), err)
			continue
		}
		if result.Decision != GuardAllow {
			return result
		}
	}
	return &GuardResult{Decision: GuardAllow, Reason: "all output guards passed"}
}
