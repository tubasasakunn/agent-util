package rpc

import (
	"fmt"

	agentctx "ai-agent/internal/context"
	"ai-agent/internal/engine"
	"ai-agent/internal/engine/builtin"
	"ai-agent/pkg/protocol"
)

// rebuildEngine は現在の Engine を agent.configure の差分パラメータで再構築する。
// 既存の tools / 履歴 / Completer / LogWriter は引き継がれる。
// 設定済みフィールドは applied に追加される。
func rebuildEngine(prev *engine.Engine, p *protocol.AgentConfigureParams) (*engine.Engine, []string, error) {
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
			g, err := builtin.InputGuard(name)
			if err != nil {
				return nil, nil, fmt.Errorf("guards.input: %w", err)
			}
			opts = append(opts, engine.WithInputGuards(g))
		}
		for _, name := range p.Guards.ToolCall {
			g, err := builtin.ToolCallGuard(name)
			if err != nil {
				return nil, nil, fmt.Errorf("guards.tool_call: %w", err)
			}
			opts = append(opts, engine.WithToolCallGuards(g))
		}
		for _, name := range p.Guards.Output {
			g, err := builtin.OutputGuard(name)
			if err != nil {
				return nil, nil, fmt.Errorf("guards.output: %w", err)
			}
			opts = append(opts, engine.WithOutputGuards(g))
		}
		applied = append(applied, "guards")
	}

	if p.Verify != nil {
		for _, name := range p.Verify.Verifiers {
			v, err := builtin.Verifier(name)
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

	tools := prev.Tools()
	if len(tools) > 0 {
		opts = append(opts, engine.WithTools(tools...))
	}

	newEng := engine.New(prev.Completer(), opts...)

	for _, m := range prev.History() {
		newEng.AddMessage(m)
	}

	return newEng, applied, nil
}
