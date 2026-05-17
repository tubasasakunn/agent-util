package rpc

import (
	"fmt"
	"time"

	agentctx "ai-agent/internal/context"
	"ai-agent/internal/engine"
	"ai-agent/internal/engine/builtin"
	"ai-agent/internal/llm"
	"ai-agent/pkg/protocol"
)

// rebuildEngine は現在の Engine を agent.configure の差分パラメータで再構築する。
// 既存の tools / 履歴 / Completer / LogWriter は引き継がれる。
// notifier 経由のストリーミング設定もここで反映する。
// remote が非 nil の場合、ガード/Verifier 名はまず builtin で解決し、見つからなければ
// remote レジストリ（guard.register / verifier.register で登録された名前）からフォールバックする。
// server は llm.mode="remote" 指定時に RemoteCompleter を構築するために使う (nil 不可)。
// 設定済みフィールドは applied に追加される。
func rebuildEngine(prev *engine.Engine, p *protocol.AgentConfigureParams, notifier *Notifier, remote *RemoteRegistry, server *Server) (*engine.Engine, []string, error) {
	var applied []string
	opts := []engine.Option{
		engine.WithLogWriter(prev.LogWriter()),
	}

	if p.MaxTurns != nil {
		opts = append(opts, engine.WithMaxTurns(*p.MaxTurns))
		applied = append(applied, "max_turns")
	}
	if p.SystemPrompt != nil {
		opts = append(opts, engine.WithSystemPrompt(*p.SystemPrompt))
		applied = append(applied, "system_prompt")
	}
	if p.TokenLimit != nil {
		opts = append(opts, engine.WithTokenLimit(*p.TokenLimit))
		applied = append(applied, "token_limit")
	}
	if p.WorkDir != nil {
		opts = append(opts, engine.WithWorkDir(*p.WorkDir))
		applied = append(applied, "work_dir")
	}

	if p.Delegate != nil {
		if p.Delegate.Enabled != nil {
			opts = append(opts, engine.WithDelegateEnabled(*p.Delegate.Enabled))
		}
		if p.Delegate.MaxChars != nil {
			opts = append(opts, engine.WithDelegateMaxChars(*p.Delegate.MaxChars))
		}
		applied = append(applied, "delegate")
	}

	if p.Coordinator != nil {
		if p.Coordinator.Enabled != nil {
			opts = append(opts, engine.WithCoordinatorEnabled(*p.Coordinator.Enabled))
		}
		if p.Coordinator.MaxChars != nil {
			opts = append(opts, engine.WithCoordinateMaxChars(*p.Coordinator.MaxChars))
		}
		applied = append(applied, "coordinator")
	}

	if p.Compaction != nil {
		enabled := true
		if p.Compaction.Enabled != nil {
			enabled = *p.Compaction.Enabled
		}
		if enabled {
			cfg := agentctx.DefaultCompactionConfig()
			if p.Compaction.BudgetMaxChars != nil {
				cfg.BudgetMaxChars = *p.Compaction.BudgetMaxChars
			}
			if p.Compaction.KeepLast != nil {
				cfg.KeepLast = *p.Compaction.KeepLast
			}
			if p.Compaction.TargetRatio != nil {
				cfg.TargetRatio = *p.Compaction.TargetRatio
			}
			switch p.Compaction.Summarizer {
			case "":
				// Summarizer なし: Stage4 はスキップされる
			case "llm":
				cfg.Summarizer = builtin.NewLLMSummarizer(prev.Completer(), "")
			default:
				return nil, nil, fmt.Errorf("compaction.summarizer: unknown name %q (available: llm)", p.Compaction.Summarizer)
			}
			opts = append(opts, engine.WithCompaction(cfg))
		}
		applied = append(applied, "compaction")
	}

	if p.Permission != nil {
		enabled := true
		if p.Permission.Enabled != nil {
			enabled = *p.Permission.Enabled
		}
		if enabled {
			policy := engine.PermissionPolicy{}
			for _, name := range p.Permission.Deny {
				policy.DenyRules = append(policy.DenyRules, engine.PermissionRule{ToolName: name})
			}
			for _, name := range p.Permission.Allow {
				policy.AllowRules = append(policy.AllowRules, engine.PermissionRule{ToolName: name})
			}
			opts = append(opts, engine.WithPermissionPolicy(policy))
		}
		applied = append(applied, "permission")
	}

	if p.Guards != nil {
		for _, name := range p.Guards.Input {
			g, err := resolveInputGuard(name, remote)
			if err != nil {
				return nil, nil, fmt.Errorf("guards.input: %w", err)
			}
			opts = append(opts, engine.WithInputGuards(g))
		}
		for _, name := range p.Guards.ToolCall {
			g, err := resolveToolCallGuard(name, remote)
			if err != nil {
				return nil, nil, fmt.Errorf("guards.tool_call: %w", err)
			}
			opts = append(opts, engine.WithToolCallGuards(g))
		}
		for _, name := range p.Guards.Output {
			g, err := resolveOutputGuard(name, remote)
			if err != nil {
				return nil, nil, fmt.Errorf("guards.output: %w", err)
			}
			opts = append(opts, engine.WithOutputGuards(g))
		}
		applied = append(applied, "guards")
	}

	if p.Verify != nil {
		for _, name := range p.Verify.Verifiers {
			v, err := resolveVerifier(name, remote)
			if err != nil {
				return nil, nil, fmt.Errorf("verify.verifiers: %w", err)
			}
			opts = append(opts, engine.WithVerifiers(v))
		}
		if p.Verify.MaxStepRetries != nil {
			opts = append(opts, engine.WithMaxStepRetries(*p.Verify.MaxStepRetries))
		}
		if p.Verify.MaxConsecutiveFailures != nil {
			opts = append(opts, engine.WithMaxConsecutiveFailures(*p.Verify.MaxConsecutiveFailures))
		}
		applied = append(applied, "verify")
	}

	if p.ToolScope != nil {
		scope := engine.ToolScope{
			IncludeAlways: make(map[string]bool),
		}
		if p.ToolScope.MaxTools != nil {
			scope.MaxTools = *p.ToolScope.MaxTools
		}
		for _, n := range p.ToolScope.IncludeAlways {
			scope.IncludeAlways[n] = true
		}
		// A1/A4: tool_budget を ToolScope に伝播
		if len(p.ToolScope.ToolBudget) > 0 {
			scope.ToolBudget = make(map[string]int, len(p.ToolScope.ToolBudget))
			for k, v := range p.ToolScope.ToolBudget {
				scope.ToolBudget[k] = v
			}
		}
		opts = append(opts, engine.WithToolScope(scope))
		applied = append(applied, "tool_scope")
	}

	if p.Reminder != nil {
		if p.Reminder.Threshold != nil {
			opts = append(opts, engine.WithReminderThreshold(*p.Reminder.Threshold))
		}
		if p.Reminder.Content != "" {
			opts = append(opts, engine.WithDynamicSection(engine.Section{
				Key:      "reminder",
				Priority: engine.PriorityReminder,
				Scope:    engine.ScopeManual,
				Content:  p.Reminder.Content,
			}))
		}
		applied = append(applied, "reminder")
	}

	if p.Streaming != nil {
		streamEnabled := false
		if p.Streaming.Enabled != nil {
			streamEnabled = *p.Streaming.Enabled
		}
		opts = append(opts, engine.WithStreaming(streamEnabled))
		if streamEnabled && notifier != nil {
			opts = append(opts, engine.WithStreamCallback(func(delta string, turn int) {
				_ = notifier.StreamDelta(delta, turn)
			}))
		}
		statusEnabled := false
		if p.Streaming.ContextStatus != nil {
			statusEnabled = *p.Streaming.ContextStatus
		}
		if statusEnabled && notifier != nil {
			opts = append(opts, engine.WithContextStatusCallback(func(ratio float64, count, limit int, event, lastRole string, compactionDelta int) {
				_ = notifier.ContextStatusWithEvent(ratio, count, limit, event, lastRole, compactionDelta)
			}))
		}
		applied = append(applied, "streaming")
	}

	if p.Loop != nil && p.Loop.Type != "" {
		lt, ok := engine.LoopTypeFromString(p.Loop.Type)
		if !ok {
			return nil, nil, fmt.Errorf("loop.type: unknown value %q (expected react|reaf)", p.Loop.Type)
		}
		opts = append(opts, engine.WithLoopType(lt))
		applied = append(applied, "loop")
	}

	if p.Router != nil && p.Router.Endpoint != "" {
		routerClient := llm.NewClient(
			llm.WithEndpoint(p.Router.Endpoint),
			llm.WithModel(p.Router.Model),
			llm.WithAPIKey(p.Router.APIKey),
		)
		opts = append(opts, engine.WithRouterCompleter(routerClient))
		applied = append(applied, "router")
	}

	if p.Judge != nil {
		switch {
		case p.Judge.Name != "":
			j, ok := remote.LookupGoalJudge(p.Judge.Name)
			if !ok {
				return nil, nil, fmt.Errorf("judge.name: %q not registered (call judge.register first)", p.Judge.Name)
			}
			opts = append(opts, engine.WithGoalJudge(j))
			applied = append(applied, "judge")
		case p.Judge.Builtin != "":
			// A2: 内蔵判定器を組み立てる。例: "min_length:30"
			j, jerr := engine.BuildBuiltinGoalJudge(p.Judge.Builtin)
			if jerr != nil {
				return nil, nil, fmt.Errorf("judge.builtin: %w", jerr)
			}
			opts = append(opts, engine.WithGoalJudge(j))
			applied = append(applied, "judge")
		}
	}

	// routerCompleter / goalJudge / loopType を引き継ぐ（configure で未指定の場合）
	if p.Router == nil && prev.RouterCompleter() != nil {
		opts = append(opts, engine.WithRouterCompleter(prev.RouterCompleter()))
	}
	if p.Judge == nil && prev.GoalJudge() != nil {
		opts = append(opts, engine.WithGoalJudge(prev.GoalJudge()))
	}
	if p.Loop == nil {
		opts = append(opts, engine.WithLoopType(prev.LoopType()))
	}

	tools := prev.Tools()
	if len(tools) > 0 {
		opts = append(opts, engine.WithTools(tools...))
	}

	// LLM ドライバの切替: llm.mode="remote" のとき完了者をラッパー委譲版に差し替える。
	// 未指定 / mode="http" / mode="" のときは既存 Completer をそのまま使う。
	completer := prev.Completer()
	if p.LLM != nil {
		switch p.LLM.Mode {
		case "", protocol.LLMModeHTTP:
			// 何もしない (既存の HTTP クライアントを維持)
		case protocol.LLMModeRemote:
			if server == nil {
				return nil, nil, fmt.Errorf("llm.mode=remote requires JSON-RPC server")
			}
			timeout := time.Duration(p.LLM.TimeoutSeconds) * time.Second
			completer = NewRemoteCompleter(server, timeout)
		default:
			return nil, nil, fmt.Errorf("llm.mode: unknown value %q (expected http|remote)", p.LLM.Mode)
		}
		applied = append(applied, "llm")
	}

	newEng, err := engine.New(completer, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("rebuild engine: %w", err)
	}

	for _, m := range prev.History() {
		newEng.AddMessage(m)
	}

	return newEng, applied, nil
}

// resolveInputGuard は名前から InputGuard を解決する。
// builtin → remote の順に試行し、どちらでも見つからなければ error を返す。
func resolveInputGuard(name string, remote *RemoteRegistry) (engine.InputGuard, error) {
	if g, err := builtin.InputGuard(name); err == nil {
		return g, nil
	}
	if remote != nil {
		if g, ok := remote.LookupInputGuard(name); ok {
			return g, nil
		}
	}
	return nil, fmt.Errorf("unknown input guard: %q (not registered as builtin or remote)", name)
}

// resolveToolCallGuard は名前から ToolCallGuard を解決する。
func resolveToolCallGuard(name string, remote *RemoteRegistry) (engine.ToolCallGuard, error) {
	if g, err := builtin.ToolCallGuard(name); err == nil {
		return g, nil
	}
	if remote != nil {
		if g, ok := remote.LookupToolCallGuard(name); ok {
			return g, nil
		}
	}
	return nil, fmt.Errorf("unknown tool_call guard: %q (not registered as builtin or remote)", name)
}

// resolveOutputGuard は名前から OutputGuard を解決する。
func resolveOutputGuard(name string, remote *RemoteRegistry) (engine.OutputGuard, error) {
	if g, err := builtin.OutputGuard(name); err == nil {
		return g, nil
	}
	if remote != nil {
		if g, ok := remote.LookupOutputGuard(name); ok {
			return g, nil
		}
	}
	return nil, fmt.Errorf("unknown output guard: %q (not registered as builtin or remote)", name)
}

// resolveVerifier は名前から Verifier を解決する。
func resolveVerifier(name string, remote *RemoteRegistry) (engine.Verifier, error) {
	if v, err := builtin.Verifier(name); err == nil {
		return v, nil
	}
	if remote != nil {
		if v, ok := remote.LookupVerifier(name); ok {
			return v, nil
		}
	}
	return nil, fmt.Errorf("unknown verifier: %q (not registered as builtin or remote)", name)
}
