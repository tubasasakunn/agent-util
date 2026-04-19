package engine

import (
	"context"
	"fmt"
	"io"
	"strings"

	agentctx "ai-agent/internal/context"
	"ai-agent/internal/llm"
)

// Engine はエージェントループを管理する。
type Engine struct {
	completer    llm.Completer
	ctxManager   *agentctx.Manager
	maxTurns     int
	systemPrompt string
	registry     *Registry
	logw         io.Writer
	compaction   *agentctx.CompactionConfig
}

// New は Engine を生成する。
func New(completer llm.Completer, opts ...Option) *Engine {
	cfg := defaultEngineConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	reg := NewRegistry()
	for _, t := range cfg.tools {
		if err := reg.Register(t); err != nil {
			panic(fmt.Sprintf("engine: %v", err))
		}
	}

	ctxMgr := agentctx.NewManager(cfg.tokenLimit)

	eng := &Engine{
		completer:    completer,
		ctxManager:   ctxMgr,
		maxTurns:     cfg.maxTurns,
		systemPrompt: cfg.systemPrompt,
		registry:     reg,
		logw:         cfg.logWriter,
		compaction:   cfg.compaction,
	}

	// 閾値超過時のログ出力を登録
	ctxMgr.OnThreshold(func(evt agentctx.Event) {
		switch evt.Kind {
		case agentctx.ThresholdExceeded:
			eng.logf("[context] threshold exceeded: %.0f%% (%d/%d tokens)",
				evt.UsageRatio*100, evt.TokenCount, evt.TokenLimit)
		case agentctx.ThresholdRecovered:
			eng.logf("[context] threshold recovered: %.0f%% (%d/%d tokens)",
				evt.UsageRatio*100, evt.TokenCount, evt.TokenLimit)
		}
	})

	// システムプロンプトとツール定義のトークン数を予約
	eng.updateReservedTokens()

	return eng
}

// logf はログメッセージを出力する。logw が nil の場合は何もしない。
func (e *Engine) logf(format string, args ...any) {
	if e.logw != nil {
		fmt.Fprintf(e.logw, format+"\n", args...)
	}
}

// Run はユーザー入力を受け取り、エージェントループを実行して結果を返す。
// メッセージ履歴は Engine に蓄積され、複数回の Run() でマルチターン対話を実現する。
func (e *Engine) Run(ctx context.Context, input string) (*Result, error) {
	e.ctxManager.Add(UserMessage(input))
	e.logf("[context] %d/%d tokens (%.0f%%)",
		e.ctxManager.TokenCount(), e.ctxManager.TokenLimit(), e.ctxManager.UsageRatio()*100)

	var totalUsage llm.Usage
	for turn := 0; turn < e.maxTurns; turn++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		lr, err := e.step(ctx)
		if err != nil {
			return nil, fmt.Errorf("turn %d: %w", turn, err)
		}

		totalUsage.PromptTokens += lr.Usage.PromptTokens
		totalUsage.CompletionTokens += lr.Usage.CompletionTokens
		totalUsage.TotalTokens += lr.Usage.TotalTokens

		switch lr.Kind {
		case Terminal:
			e.logf("[done] %d turns, %d tokens", turn+1, totalUsage.TotalTokens)
			return &Result{
				Response: lr.Message.ContentString(),
				Reason:   lr.Reason,
				Usage:    totalUsage,
				Turns:    turn + 1,
			}, nil
		case Continue:
			continue
		}
	}
	return nil, ErrMaxTurnsReached
}

// step は1ターンのモデル呼び出しを実行し、LoopResult を返す。
// ツールが登録されている場合はルーターステップを経由する。
func (e *Engine) step(ctx context.Context) (*LoopResult, error) {
	if err := e.maybeCompact(ctx); err != nil {
		return nil, fmt.Errorf("compaction: %w", err)
	}

	if e.registry.Len() == 0 {
		return e.chatStep(ctx)
	}
	return e.toolStep(ctx)
}

// maybeCompact は閾値超過時に縮約カスケードを実行する。
func (e *Engine) maybeCompact(ctx context.Context) error {
	if e.compaction == nil {
		return nil
	}
	if e.ctxManager.UsageRatio() < e.ctxManager.Threshold() {
		return nil
	}

	e.logf("[context] compaction triggered at %.0f%%", e.ctxManager.UsageRatio()*100)
	before := e.ctxManager.TokenCount()
	if err := e.ctxManager.Compact(ctx, *e.compaction); err != nil {
		return err
	}
	after := e.ctxManager.TokenCount()
	e.logf("[context] compaction complete: %d → %d tokens (%.0f%%)",
		before, after, e.ctxManager.UsageRatio()*100)
	return nil
}

// chatStep は通常のチャット補完（Phase 2互換）。
func (e *Engine) chatStep(ctx context.Context) (*LoopResult, error) {
	e.logf("[chat] 応答を生成中...")
	msgs := e.buildMessages()

	resp, err := e.completer.ChatCompletion(ctx, &llm.ChatRequest{
		Messages: msgs,
	})
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("chat step: %w", llm.ErrEmptyResponse)
	}

	assistantMsg := resp.Choices[0].Message
	e.ctxManager.Add(assistantMsg)

	return &LoopResult{
		Kind:    Terminal,
		Reason:  "completed",
		Message: assistantMsg,
		Usage:   resp.Usage,
	}, nil
}

// toolStep はルーターステップ + ツール実行。
func (e *Engine) toolStep(ctx context.Context) (*LoopResult, error) {
	// 1. ルーターでツール選択
	e.logf("[router] ツールを選択中...")
	rr, usage, err := e.routerStep(ctx)
	if err != nil {
		return nil, fmt.Errorf("tool step: %w", err)
	}

	// 2. tool == "none" → 通常チャットで最終応答を生成
	if rr.Tool == "none" {
		e.logf("[router] ツール不要 → 直接応答 (%s)", rr.Reasoning)
		lr, err := e.chatStep(ctx)
		if err != nil {
			return nil, err
		}
		// chatStep の usage にルーターの usage を加算
		lr.Usage.PromptTokens += usage.PromptTokens
		lr.Usage.CompletionTokens += usage.CompletionTokens
		lr.Usage.TotalTokens += usage.TotalTokens
		return lr, nil
	}

	e.logf("[router] %s を選択 | 引数: %s", rr.Tool, string(rr.Arguments))
	if rr.Reasoning != "" {
		e.logf("[router] 理由: %s", rr.Reasoning)
	}

	// 3. ツールの取得
	t, ok := e.registry.Get(rr.Tool)
	if !ok {
		e.logf("[tool] %s が見つかりません", rr.Tool)
		callID := generateCallID()
		errContent := fmt.Sprintf("Error: tool %q not found. Available tools: %s",
			rr.Tool, e.availableToolNames())
		e.ctxManager.Add(ToolCallMessage(callID, rr.Tool, rr.Arguments))
		e.ctxManager.Add(ToolResultMessage(callID, errContent))
		return &LoopResult{
			Kind:   Continue,
			Reason: "tool_not_found",
			Usage:  *usage,
		}, nil
	}

	// 4. ツール実行
	e.logf("[tool] %s を実行中...", rr.Tool)
	result, execErr := t.Execute(ctx, rr.Arguments)

	// 5. 合成メッセージの構築と履歴への追加
	callID := generateCallID()
	e.ctxManager.Add(ToolCallMessage(callID, rr.Tool, rr.Arguments))

	var resultContent string
	switch {
	case execErr != nil:
		resultContent = fmt.Sprintf("Error executing tool %q: %s", rr.Tool, execErr.Error())
		e.logf("[tool] %s 実行エラー: %s", rr.Tool, execErr.Error())
	case result.IsError:
		resultContent = fmt.Sprintf("Error: %s", result.Content)
		e.logf("[tool] %s エラー: %s", rr.Tool, result.Content)
	default:
		resultContent = result.Content
		preview := resultContent
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		e.logf("[tool] %s 完了 (%d bytes): %s", rr.Tool, len(resultContent), preview)
	}
	e.ctxManager.Add(ToolResultMessage(callID, resultContent))

	return &LoopResult{
		Kind:   Continue,
		Reason: "tool_use",
		Usage:  *usage,
	}, nil
}

// availableToolNames はカンマ区切りのツール名リストを返す。
func (e *Engine) availableToolNames() string {
	defs := e.registry.Definitions()
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return strings.Join(names, ", ")
}

// buildMessages はシステムプロンプトと会話履歴を結合してリクエスト用メッセージを構築する。
func (e *Engine) buildMessages() []llm.Message {
	history := e.ctxManager.Messages()
	msgs := make([]llm.Message, 0, len(history)+1)
	if e.systemPrompt != "" {
		msgs = append(msgs, SystemMessage(e.systemPrompt))
	}
	msgs = append(msgs, history...)
	return msgs
}

// updateReservedTokens はシステムプロンプトとツール定義の推定トークン数を計算し、Manager に設定する。
func (e *Engine) updateReservedTokens() {
	var reserved int
	if e.systemPrompt != "" {
		reserved += agentctx.EstimateTextTokens(e.systemPrompt)
	}
	if e.registry.Len() > 0 {
		reserved += agentctx.EstimateTextTokens(e.registry.FormatForPrompt())
	}
	e.ctxManager.SetReserved(reserved)
}
