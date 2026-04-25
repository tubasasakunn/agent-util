package engine

import (
	"context"
	"fmt"
	"io"
	"strings"

	agentctx "ai-agent/internal/context"
	"ai-agent/internal/llm"
	"ai-agent/pkg/tool"
)

// Engine はエージェントループを管理する。
type Engine struct {
	completer              llm.Completer
	ctxManager             *agentctx.Manager
	maxTurns               int
	systemPrompt           string
	registry               *Registry
	logw                   io.Writer
	compaction             *agentctx.CompactionConfig
	delegateEnabled        bool
	delegateMaxChars       int
	workDir                string
	coordinatorEnabled     bool
	coordinateMaxChars     int
	promptBuilder          *PromptBuilder
	reminderThreshold      int
	toolScope              *ToolScope
	maxStepRetries         int
	maxConsecutiveFailures int
	verifiers              *VerifierRegistry
	permChecker            *PermissionChecker // nil なら全許可（後方互換）
	guards                 *GuardRegistry     // nil ならガードなし（後方互換）
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

	// パーミッションチェッカーの初期化（ポリシーが設定されている場合のみ）
	var permChecker *PermissionChecker
	if cfg.permissionPolicy != nil {
		auditW := cfg.auditWriter
		if auditW == nil {
			auditW = cfg.logWriter
		}
		permChecker = NewPermissionChecker(*cfg.permissionPolicy, cfg.userApprover, NewAuditLogger(auditW))
	}

	// ガードレジストリの初期化
	var guards *GuardRegistry
	if len(cfg.inputGuards) > 0 || len(cfg.toolCallGuards) > 0 || len(cfg.outputGuards) > 0 {
		guards = NewGuardRegistry()
		for _, g := range cfg.inputGuards {
			guards.AddInput(g)
		}
		for _, g := range cfg.toolCallGuards {
			guards.AddToolCall(g)
		}
		for _, g := range cfg.outputGuards {
			guards.AddOutput(g)
		}
	}

	eng := &Engine{
		completer:              completer,
		ctxManager:             ctxMgr,
		maxTurns:               cfg.maxTurns,
		systemPrompt:           cfg.systemPrompt,
		registry:               reg,
		logw:                   cfg.logWriter,
		compaction:             cfg.compaction,
		delegateEnabled:        cfg.delegateEnabled,
		delegateMaxChars:       cfg.delegateMaxChars,
		workDir:                cfg.workDir,
		coordinatorEnabled:     cfg.coordinatorEnabled,
		coordinateMaxChars:     cfg.coordinateMaxChars,
		reminderThreshold:      cfg.reminderThreshold,
		toolScope:              cfg.toolScope,
		maxStepRetries:         cfg.maxStepRetries,
		maxConsecutiveFailures: cfg.maxConsecutiveFailures,
		verifiers:              NewVerifierRegistry(cfg.verifiers...),
		permChecker:            permChecker,
		guards:                 guards,
	}

	// PromptBuilder を初期化
	eng.initPromptBuilder(cfg)

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

// Fork は同じ Completer とツールセットを共有する子 Engine を生成する。
// コンテキストはクリーンな状態で開始する。
// opts で systemPrompt, maxTurns 等を上書き可能。
// delegate_task は無効化されネスト再帰を防止する。
func (e *Engine) Fork(opts ...Option) *Engine {
	// 親の設定をベースに子Engineの設定を構築
	cfg := engineConfig{
		maxTurns:               e.maxTurns,
		systemPrompt:           e.systemPrompt,
		tools:                  e.registry.Tools(),
		logWriter:              e.logw,
		tokenLimit:             e.ctxManager.TokenLimit(),
		compaction:             e.compaction,
		delegateEnabled:        false, // ネスト再帰防止
		delegateMaxChars:       e.delegateMaxChars,
		workDir:                e.workDir,
		coordinatorEnabled:     false, // ネスト再帰防止
		coordinateMaxChars:     e.coordinateMaxChars,
		maxStepRetries:         e.maxStepRetries,
		maxConsecutiveFailures: e.maxConsecutiveFailures,
	}
	// パーミッションポリシーを継承（UserApproverはnil: 子はask→deny）
	if e.permChecker != nil {
		policy := e.permChecker.Policy()
		cfg.permissionPolicy = &policy
		// userApprover は意図的に nil（子Engineは対話的確認不可、fail-closed）
	}
	// ガードレールを継承
	if e.guards != nil {
		cfg.inputGuards = e.guards.InputGuards()
		cfg.toolCallGuards = e.guards.ToolCallGuards()
		cfg.outputGuards = e.guards.OutputGuards()
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return New(e.completer,
		WithMaxTurns(cfg.maxTurns),
		WithSystemPrompt(cfg.systemPrompt),
		WithTools(cfg.tools...),
		WithLogWriter(cfg.logWriter),
		WithTokenLimit(cfg.tokenLimit),
		WithDelegateEnabled(cfg.delegateEnabled),
		WithDelegateMaxChars(cfg.delegateMaxChars),
		WithWorkDir(cfg.workDir),
		WithCoordinatorEnabled(cfg.coordinatorEnabled),
		WithCoordinateMaxChars(cfg.coordinateMaxChars),
		WithMaxStepRetries(cfg.maxStepRetries),
		WithMaxConsecutiveFailures(cfg.maxConsecutiveFailures),
		withOptionalCompaction(cfg.compaction),
	)
}

// withOptionalCompaction は compaction が非nil の場合のみ WithCompaction を適用する。
func withOptionalCompaction(cfg *agentctx.CompactionConfig) Option {
	return func(c *engineConfig) {
		c.compaction = cfg
	}
}

// AddMessage はメッセージを会話履歴に追加する。外部テスト向けの公開メソッド。
func (e *Engine) AddMessage(msg llm.Message) {
	e.ctxManager.Add(msg)
}

// ReservedTokens はシステムプロンプト + ツール定義の予約トークン数を返す。
func (e *Engine) ReservedTokens() int {
	return e.promptBuilder.EstimateReservedTokens()
}

// UsageRatio は現在のコンテキスト使用率を返す。
func (e *Engine) UsageRatio() float64 {
	return e.ctxManager.UsageRatio()
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
	// 入力ガードレール（Phase 9）
	if e.guards != nil {
		gr := e.guards.RunInput(ctx, input, e.logf)
		switch gr.Decision {
		case GuardTripwire:
			return nil, &TripwireError{Source: "input", Reason: gr.Reason}
		case GuardDeny:
			return &Result{
				Response: "Input rejected: " + gr.Reason,
				Reason:   "input_denied",
			}, nil
		}
	}

	e.ctxManager.Add(UserMessage(input))
	e.logf("[context] %d/%d tokens (%.0f%%)",
		e.ctxManager.TokenCount(), e.ctxManager.TokenLimit(), e.ctxManager.UsageRatio()*100)

	var totalUsage llm.Usage
	var stepRetries int
	var consecutiveFailures int

	for turn := 0; turn < e.maxTurns; turn++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		lr, err := e.step(ctx)
		if err != nil {
			class := classifyError(err)
			switch class {
			case ErrClassTransient:
				if stepRetries >= e.maxStepRetries {
					return nil, fmt.Errorf("turn %d: %w: %w", turn, ErrMaxStepRetries, err)
				}
				stepRetries++
				e.logf("[retry] transient error (attempt %d/%d): %s",
					stepRetries, e.maxStepRetries, err)
				wait := calcStepBackoff(stepRetries - 1)
				if err := sleepWithContext(ctx, wait); err != nil {
					return nil, err
				}
				continue
			case ErrClassUserFixable:
				e.logf("[error] user fixable: %s", err)
				return &Result{
					Response: fmt.Sprintf("User intervention required: %s", err),
					Reason:   "user_fixable",
					Usage:    totalUsage,
					Turns:    turn + 1,
				}, nil
			default: // ErrClassFatal, ErrClassLLMRecoverable
				return nil, fmt.Errorf("turn %d: %w", turn, err)
			}
		}
		stepRetries = 0 // step 成功時にリセット

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
			if isFailureReason(lr.Reason) {
				consecutiveFailures++
				if consecutiveFailures >= e.maxConsecutiveFailures {
					e.logf("[safety] consecutive failures (%d) reached limit", consecutiveFailures)
					return &Result{
						Response: fmt.Sprintf("Stopped: %d consecutive failures", consecutiveFailures),
						Reason:   "max_consecutive_failures",
						Usage:    totalUsage,
						Turns:    turn + 1,
					}, nil
				}
			} else {
				consecutiveFailures = 0
			}
			continue
		}
	}
	return nil, ErrMaxTurnsReached
}

// isFailureReason は連続失敗としてカウントすべき Reason かを判定する。
func isFailureReason(reason string) bool {
	switch reason {
	case "tool_error", "tool_not_found", "verify_failed", "permission_denied", "guard_blocked":
		return true
	default:
		return false
	}
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

	// 出力ガードレール（Phase 9）
	if e.guards != nil {
		output := assistantMsg.ContentString()
		gr := e.guards.RunOutput(ctx, output, e.logf)
		switch gr.Decision {
		case GuardTripwire:
			return nil, &TripwireError{Source: "output", Reason: gr.Reason}
		case GuardDeny:
			e.logf("[guard] 出力ブロック: %s", gr.Reason)
			safeMsg := llm.Message{Role: "assistant", Content: llm.StringPtr("I cannot provide that response.")}
			e.ctxManager.Add(safeMsg)
			return &LoopResult{
				Kind:    Terminal,
				Reason:  "output_blocked",
				Message: safeMsg,
				Usage:   resp.Usage,
			}, nil
		}
	}

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

	// バーチャルツールの検出（ADR-006）
	if rr.Tool == "delegate_task" && e.delegateEnabled {
		return e.delegateStep(ctx, rr, usage)
	}
	if rr.Tool == "coordinate_tasks" && e.coordinatorEnabled {
		return e.coordinateStep(ctx, rr, usage)
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

	// 4. パーミッションチェック（Phase 9）
	if e.permChecker != nil {
		decision := e.permChecker.Check(ctx, t, rr.Arguments)
		if decision == PermDenied {
			e.logf("[permission] %s 拒否", rr.Tool)
			callID := generateCallID()
			e.ctxManager.Add(ToolCallMessage(callID, rr.Tool, rr.Arguments))
			e.ctxManager.Add(ToolResultMessage(callID,
				fmt.Sprintf("Permission denied: tool %q is not allowed by the current policy.", rr.Tool)))
			return &LoopResult{
				Kind:   Continue,
				Reason: "permission_denied",
				Usage:  *usage,
			}, nil
		}
	}

	// 5. ツール呼び出しガードレール（Phase 9）
	if e.guards != nil {
		gr := e.guards.RunToolCall(ctx, rr.Tool, rr.Arguments, e.logf)
		switch gr.Decision {
		case GuardTripwire:
			return nil, &TripwireError{Source: "tool_call", Reason: gr.Reason}
		case GuardDeny:
			e.logf("[guard] %s ブロック: %s", rr.Tool, gr.Reason)
			callID := generateCallID()
			e.ctxManager.Add(ToolCallMessage(callID, rr.Tool, rr.Arguments))
			e.ctxManager.Add(ToolResultMessage(callID,
				fmt.Sprintf("Blocked by guard: %s", gr.Reason)))
			return &LoopResult{
				Kind:   Continue,
				Reason: "guard_blocked",
				Usage:  *usage,
			}, nil
		}
	}

	// 6. ツール実行（workDir があればコンテキストに注入）
	e.logf("[tool] %s を実行中...", rr.Tool)
	toolCtx := ctx
	if e.workDir != "" {
		toolCtx = tool.ContextWithWorkDir(ctx, e.workDir)
	}
	result, execErr := t.Execute(toolCtx, rr.Arguments)

	// 7. 合成メッセージの構築と履歴への追加
	callID := generateCallID()
	e.ctxManager.Add(ToolCallMessage(callID, rr.Tool, rr.Arguments))

	var resultContent string
	var reason string
	switch {
	case execErr != nil:
		resultContent = fmt.Sprintf("Error executing tool %q: %s", rr.Tool, execErr.Error())
		reason = "tool_error"
		e.logf("[tool] %s 実行エラー: %s", rr.Tool, execErr.Error())
	case result.IsError:
		resultContent = fmt.Sprintf("Error: %s", result.Content)
		reason = "tool_error"
		e.logf("[tool] %s エラー: %s", rr.Tool, result.Content)
	default:
		resultContent = result.Content
		reason = "tool_use"
		preview := resultContent
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		e.logf("[tool] %s 完了 (%d bytes): %s", rr.Tool, len(resultContent), preview)
	}
	e.ctxManager.Add(ToolResultMessage(callID, resultContent))

	// 8. Verify: 検証ループ（PEVサイクルの V）
	// 検証スキップ条件: ツール実行エラー時 / 検証器未登録
	if reason == "tool_use" && e.verifiers.Len() > 0 {
		vr := e.verifiers.RunAll(ctx, rr.Tool, rr.Arguments, resultContent, e.logf)
		if !vr.Passed {
			e.logf("[verify] %s 検証失敗: %s", rr.Tool, vr.Summary)
			verifyMsg := fmt.Sprintf("[Verification Failed]\n%s\nPlease fix the issues and try again.", vr.Summary)
			e.ctxManager.Add(UserMessage(verifyMsg))
			return &LoopResult{
				Kind:   Continue,
				Reason: "verify_failed",
				Usage:  *usage,
			}, nil
		}
		e.logf("[verify] %s 検証パス", rr.Tool)
	}

	return &LoopResult{
		Kind:   Continue,
		Reason: reason,
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

// initPromptBuilder は PromptBuilder を初期化し、セクションを登録する。
func (e *Engine) initPromptBuilder(cfg engineConfig) {
	pb := NewPromptBuilder()

	// システムプロンプト（chat + router 共通）
	if cfg.systemPrompt != "" {
		pb.Add(Section{
			Key:      "system",
			Priority: PrioritySystem,
			Scope:    ScopeAll,
			Content:  cfg.systemPrompt,
			Required: true,
		})
	}

	// ツール定義（router のみ）
	reg := e.registry
	delegateEnabled := e.delegateEnabled
	coordinatorEnabled := e.coordinatorEnabled
	toolScope := e.toolScope
	pb.Add(Section{
		Key:      "tools",
		Priority: PriorityTools,
		Scope:    ScopeRouter,
		Dynamic: func() string {
			var sb strings.Builder
			if toolScope != nil {
				sb.WriteString(reg.ScopedFormatForPrompt(*toolScope))
			} else {
				sb.WriteString(reg.FormatForPrompt())
			}
			if delegateEnabled {
				sb.WriteString(delegateToolDef())
			}
			if coordinatorEnabled {
				sb.WriteString(coordinatorToolDef())
			}
			return sb.String()
		},
	})

	// ルーターの Instructions（router のみ、末尾近くに配置）
	// Lost in the Middle 対策: JSON応答指示はユーザー入力に近い位置に配置
	pb.Add(Section{
		Key:      "instructions",
		Priority: PriorityReminder - 1, // リマインダーの直前
		Scope:    ScopeRouter,
		Content:  routerInstructions(),
		Required: true,
	})

	// MEMORY インデックス（developer 優先度で router に含める）
	if len(cfg.memoryEntries) > 0 {
		mi := NewMemoryIndex(cfg.memoryEntries)
		pb.Add(Section{
			Key:      "memory_index",
			Priority: PriorityDeveloper,
			Scope:    ScopeRouter,
			Dynamic:  mi.FormatForPrompt,
		})
	}

	// ユーザー指定の動的セクション
	for _, ds := range cfg.dynamicSections {
		pb.Add(ds)
	}

	e.promptBuilder = pb
}

// buildMessages はシステムプロンプトと会話履歴を結合してリクエスト用メッセージを構築する。
// リマインダーが登録されており会話が十分長い場合、末尾近くにリマインダーを挿入する。
func (e *Engine) buildMessages() []llm.Message {
	history := e.ctxManager.Messages()
	sysPrompt := e.promptBuilder.BuildSystemPrompt()
	reminder := e.buildReminder(len(history))

	msgs := make([]llm.Message, 0, len(history)+2)
	if sysPrompt != "" {
		msgs = append(msgs, SystemMessage(sysPrompt))
	}

	if reminder == "" {
		msgs = append(msgs, history...)
		return msgs
	}

	// リマインダーを最後の user メッセージの直前に挿入する
	insertIdx := findLastUserIndex(history)
	for i, m := range history {
		if i == insertIdx {
			msgs = append(msgs, UserMessage("[System Reminder] "+reminder))
		}
		msgs = append(msgs, m)
	}
	// 最後の user メッセージがない場合は末尾に追加
	if insertIdx == len(history) {
		msgs = append(msgs, UserMessage("[System Reminder] "+reminder))
	}

	return msgs
}

// buildReminder はリマインダーテキストを返す。
// リマインダーセクションが未登録、閾値未到達、または内容が空の場合は空文字を返す。
func (e *Engine) buildReminder(historyLen int) string {
	if e.reminderThreshold <= 0 {
		return ""
	}
	if historyLen < e.reminderThreshold {
		return ""
	}
	content, ok := e.promptBuilder.Resolve("reminder")
	if !ok || content == "" {
		return ""
	}
	return content
}

// findLastUserIndex は history 内の最後の user メッセージのインデックスを返す。
// 見つからない場合は len(history) を返す。
func findLastUserIndex(history []llm.Message) int {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			return i
		}
	}
	return len(history)
}

// updateReservedTokens は PromptBuilder の推定トークン数を Manager に設定する。
func (e *Engine) updateReservedTokens() {
	reserved := e.promptBuilder.EstimateReservedTokens()
	e.ctxManager.SetReserved(reserved)
}
