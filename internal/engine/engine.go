package engine

import (
	"context"
	"fmt"
	"io"
	"strings"

	agentctx "ai-agent/internal/context"
	"ai-agent/internal/llm"
	"ai-agent/internal/skills"
	"ai-agent/pkg/tool"
)

// defaultCoordinatorMaxParallelism は coordinate_tasks の並列実行上限のデフォルト値。
const defaultCoordinatorMaxParallelism = 10

// Engine はエージェントループを管理する。
type Engine struct {
	completer                    llm.Completer
	ctxManager                   *agentctx.Manager
	maxTurns                     int
	systemPrompt                 string
	registry                     *Registry
	logw                         io.Writer
	compaction                   *agentctx.CompactionConfig
	delegateEnabled              bool
	delegateMaxChars             int
	workDir                      string
	coordinatorEnabled           bool
	coordinateMaxChars           int
	coordinatorMaxParallelism    int
	promptBuilder                *PromptBuilder
	reminderThreshold            int
	toolScope                    *ToolScope
	maxStepRetries               int
	maxConsecutiveFailures       int
	verifiers                    *VerifierRegistry
	permChecker                  *PermissionChecker // nil なら全許可（後方互換）
	guards                       *GuardRegistry     // nil ならガードなし（後方互換）
	stepCallback                 StepCallback       // nil ならコールバックなし
	streamingEnabled             bool
	streamCallback               StreamCallback
	contextStatusCallback        ContextStatusCallback
	skillToolNames               map[string]struct{}
	// ループパターンと拡張コンポーネント
	loopType        LoopType
	routerCompleter llm.Completer // nil なら completer を使用
	goalJudge       GoalJudge    // nil ならヒューリスティック判定
}

// New は Engine を生成する。ツール名の重複など設定エラーは error で返す。
func New(completer llm.Completer, opts ...Option) (*Engine, error) {
	cfg := defaultEngineConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	reg := NewRegistry()
	for _, t := range cfg.tools {
		if err := reg.Register(t); err != nil {
			return nil, fmt.Errorf("engine: %w", err)
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
		completer:                 completer,
		ctxManager:                ctxMgr,
		maxTurns:                  cfg.maxTurns,
		systemPrompt:              cfg.systemPrompt,
		registry:                  reg,
		logw:                      cfg.logWriter,
		compaction:                cfg.compaction,
		delegateEnabled:           cfg.delegateEnabled,
		delegateMaxChars:          cfg.delegateMaxChars,
		workDir:                   cfg.workDir,
		coordinatorEnabled:        cfg.coordinatorEnabled,
		coordinateMaxChars:        cfg.coordinateMaxChars,
		coordinatorMaxParallelism: cfg.coordinatorMaxParallelism,
		reminderThreshold:         cfg.reminderThreshold,
		toolScope:                 cfg.toolScope,
		maxStepRetries:            cfg.maxStepRetries,
		maxConsecutiveFailures:    cfg.maxConsecutiveFailures,
		verifiers:                 NewVerifierRegistry(cfg.verifiers...),
		permChecker:               permChecker,
		guards:                    guards,
		stepCallback:              cfg.stepCallback,
		streamingEnabled:          cfg.streamingEnabled,
		streamCallback:            cfg.streamCallback,
		contextStatusCallback:     cfg.contextStatusCallback,
		loopType:                  cfg.loopType,
		routerCompleter:           cfg.routerCompleter,
		goalJudge:                 cfg.goalJudge,
	}

	// Skills サポートの初期化。
	// AI目線ではスキルも通常ツールも区別なし。各スキルを tool.Tool として直接登録する。
	if len(cfg.skillsDirs) > 0 {
		catalog, err := skills.NewLoader(cfg.skillsDirs...).Load()
		if err != nil {
			eng.logf("[skills] load error: %v", err)
		} else {
			registered := 0
			skillNames := make(map[string]struct{})
			for _, t := range skills.CatalogAsTools(catalog) {
				if err := reg.Register(t); err != nil {
					eng.logf("[skills] register %s error: %v", t.Name(), err)
				} else {
					skillNames[t.Name()] = struct{}{}
					registered++
				}
			}
			if registered > 0 {
				eng.skillToolNames = skillNames
				eng.logf("[skills] loaded %d skill(s)", registered)
			}
		}
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

	return eng, nil
}

// Fork は同じ Completer とツールセットを共有する子 Engine を生成する。
// コンテキストはクリーンな状態で開始する。
// opts で systemPrompt, maxTurns 等を上書き可能。
// delegate_task / coordinate_tasks は無効化されネスト再帰を防止する。
// PermissionPolicy / Guards / Verifiers は親から継承する。
func (e *Engine) Fork(opts ...Option) *Engine {
	// 親設定を再現し、ネスト再帰防止のため delegate/coordinator を無効化する
	forkOpts := []Option{
		WithMaxTurns(e.maxTurns),
		WithSystemPrompt(e.systemPrompt),
		WithTools(e.registry.Tools()...),
		WithLogWriter(e.logw),
		WithTokenLimit(e.ctxManager.TokenLimit()),
		withOptionalCompaction(e.compaction),
		WithDelegateEnabled(false), // ネスト再帰防止
		WithDelegateMaxChars(e.delegateMaxChars),
		WithWorkDir(e.workDir),
		WithCoordinatorEnabled(false), // ネスト再帰防止
		WithCoordinateMaxChars(e.coordinateMaxChars),
		WithCoordinatorMaxParallelism(e.coordinatorMaxParallelism),
		WithMaxStepRetries(e.maxStepRetries),
		WithMaxConsecutiveFailures(e.maxConsecutiveFailures),
	}

	// パーミッションポリシーを継承（userApprover は意図的に省略: 子はask→deny）
	if e.permChecker != nil {
		policy := e.permChecker.Policy()
		forkOpts = append(forkOpts, WithPermissionPolicy(policy))
	}

	// ガードレールを継承
	if e.guards != nil {
		if guards := e.guards.InputGuards(); len(guards) > 0 {
			forkOpts = append(forkOpts, WithInputGuards(guards...))
		}
		if guards := e.guards.ToolCallGuards(); len(guards) > 0 {
			forkOpts = append(forkOpts, WithToolCallGuards(guards...))
		}
		if guards := e.guards.OutputGuards(); len(guards) > 0 {
			forkOpts = append(forkOpts, WithOutputGuards(guards...))
		}
	}

	// Verifiers を継承
	if e.verifiers != nil {
		if vs := e.verifiers.All(); len(vs) > 0 {
			forkOpts = append(forkOpts, WithVerifiers(vs...))
		}
	}

	// ループパターンと拡張コンポーネントを継承
	forkOpts = append(forkOpts, WithLoopType(e.loopType))
	if e.routerCompleter != nil {
		forkOpts = append(forkOpts, WithRouterCompleter(e.routerCompleter))
	}
	if e.goalJudge != nil {
		forkOpts = append(forkOpts, WithGoalJudge(e.goalJudge))
	}

	// 外部 opts を末尾に追加してオーバーライドを適用
	child := mustNew(e.completer, append(forkOpts, opts...)...)

	// スキルツール名を継承（toolAlreadySucceeded が子でも正しく動作するため）
	if len(e.skillToolNames) > 0 {
		child.skillToolNames = make(map[string]struct{}, len(e.skillToolNames))
		for k := range e.skillToolNames {
			child.skillToolNames[k] = struct{}{}
		}
	}

	return child
}

// mustNew は Fork/SessionRunner 等の内部用途でのみ使用する New のラッパー。
// 親 Engine から継承したツールセットは重複しないことが保証されるため、
// エラーが発生した場合は内部的な論理エラーとして panic する。
func mustNew(completer llm.Completer, opts ...Option) *Engine {
	eng, err := New(completer, opts...)
	if err != nil {
		panic(fmt.Sprintf("engine: internal error in Fork/Session: %v", err))
	}
	return eng
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

// RegisterTool はツールを動的に登録する。
// JSON-RPC の tool.register で使用する。
func (e *Engine) RegisterTool(t tool.Tool) error {
	if err := e.registry.Register(t); err != nil {
		return err
	}
	e.updateReservedTokens()
	return nil
}

// Tools は登録済みの全ツールを登録順で返す。Engine の再構築で引き継ぐ用途。
func (e *Engine) Tools() []tool.Tool {
	return e.registry.Tools()
}

// History は会話履歴を返す。Engine の再構築で引き継ぐ用途。
func (e *Engine) History() []llm.Message {
	return e.ctxManager.Messages()
}

// Inject はメッセージを会話履歴の指定位置に挿入する。
// position は "prepend" / "append" / "replace"。
func (e *Engine) Inject(msgs []llm.Message, position string) {
	e.ctxManager.Inject(msgs, position)
}

// summarizePrompt は Summarize() で LLM に送る要約指示プロンプト。
const summarizePrompt = "上記の会話を2〜3文で簡潔に要約してください。重要なトピックと結果に焦点を当ててください。"

// Summarize は現在の会話履歴を LLM で要約して返す。
// 履歴が空の場合は空文字を返す。
func (e *Engine) Summarize(ctx context.Context) (string, error) {
	msgs := e.ctxManager.Messages()
	if len(msgs) == 0 {
		return "", nil
	}

	summaryReq := append(msgs, llm.Message{
		Role:    "user",
		Content: llm.StringPtr(summarizePrompt),
	})

	resp, err := e.completer.ChatCompletion(ctx, &llm.ChatRequest{
		Messages: summaryReq,
	})
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("summarize: %w", llm.ErrEmptyResponse)
	}
	return resp.Choices[0].Message.ContentString(), nil
}

// Completer は LLM クライアントを返す。Engine の再構築で共有する用途。
func (e *Engine) Completer() llm.Completer {
	return e.completer
}

// RouterCompleter はルーター専用 LLM クライアントを返す。未設定時は nil。
func (e *Engine) RouterCompleter() llm.Completer {
	return e.routerCompleter
}

// GoalJudge はゴール判定器を返す。未設定時は nil。
func (e *Engine) GoalJudge() GoalJudge {
	return e.goalJudge
}

// LoopType は現在のループパターンを返す。
func (e *Engine) LoopType() LoopType {
	return e.loopType
}

// LogWriter はログ出力先を返す。Engine の再構築で引き継ぐ用途。
func (e *Engine) LogWriter() io.Writer {
	return e.logw
}

// logf はログメッセージを出力する。logw が nil の場合は何もしない。
func (e *Engine) logf(format string, args ...any) {
	if e.logw != nil {
		fmt.Fprintf(e.logw, format+"\n", args...)
	}
}

// toolAlreadySucceeded はスキルツールが直近のユーザー入力以降すでに成功実行済みかを返す。
// スキルツール以外は対象外（read_file 等は同一ターン内で複数回呼んで良い）。
func (e *Engine) toolAlreadySucceeded(toolName string) bool {
	if _, ok := e.skillToolNames[toolName]; !ok {
		return false
	}
	msgs := e.ctxManager.Messages()

	// パス1: 末尾から走査し、最後のユーザーメッセージより後の成功ツール結果IDを収集する。
	successIDs := map[string]struct{}{}
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if msg.Role == "user" {
			break
		}
		if msg.Role == "tool" {
			content := ""
			if msg.Content != nil {
				content = *msg.Content
			}
			if !strings.HasPrefix(content, "Error:") {
				successIDs[msg.ToolCallID] = struct{}{}
			}
		}
	}

	// パス2: assistant メッセージで toolName を successIDs と照合する。
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if msg.Role == "user" {
			break
		}
		if msg.Role == "assistant" {
			for _, tc := range msg.ToolCalls {
				if tc.Function.Name == toolName {
					if _, ok := successIDs[tc.ID]; ok {
						return true
					}
				}
			}
		}
	}
	return false
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
	e.emitContextStatus()

	var totalUsage llm.Usage
	var stepRetries int
	var consecutiveFailures int

	for turn := 0; turn < e.maxTurns; turn++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		e.emitContextStatus()

		lr, err := e.stepWithTurn(ctx, turn+1)
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

		// ステップコールバック（JSON-RPCストリーミング通知用）
		if e.stepCallback != nil {
			evt := StepEvent{
				Turn:       turn + 1,
				Reason:     lr.Reason,
				UsageRatio: e.ctxManager.UsageRatio(),
				TokenCount: e.ctxManager.TokenCount(),
				TokenLimit: e.ctxManager.TokenLimit(),
			}
			if lr.Kind == Terminal {
				evt.Response = lr.Message.ContentString()
			}
			e.stepCallback(evt)
		}

		// GoalJudge: "completed" 応答後に外部判定器でゴール達成を確認する。
		// judge が false を返すとループを継続（fail-open: エラー時は継続）。
		if lr.Kind == Terminal && lr.Reason == "completed" && e.goalJudge != nil {
			terminate, judgeReason, judgeErr := e.goalJudge.ShouldTerminate(ctx, lr.Message.ContentString(), turn+1)
			if judgeErr != nil {
				e.logf("[judge] error: %v (treating as terminal)", judgeErr)
			} else if !terminate {
				e.logf("[judge] not done yet, continuing (turn %d)", turn+1)
				lr = &LoopResult{Kind: Continue, Reason: "judge_continue", Usage: lr.Usage}
			} else if judgeReason != "" {
				lr.Reason = judgeReason
			}
		}

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

// stepWithTurn は turn 番号を引き回して1ステップを実行する。
// turn は streaming コールバックに渡される（0 は未指定）。
func (e *Engine) stepWithTurn(ctx context.Context, turn int) (*LoopResult, error) {
	if err := e.maybeCompact(ctx); err != nil {
		return nil, fmt.Errorf("compaction: %w", err)
	}

	if e.registry.Len() == 0 {
		return e.chatStep(ctx, turn)
	}
	return e.toolStep(ctx, turn)
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
	e.emitContextStatus()
	return nil
}

// emitContextStatus は contextStatusCallback が設定されていれば現在のコンテキスト使用率を通知する。
func (e *Engine) emitContextStatus() {
	if e.contextStatusCallback == nil {
		return
	}
	e.contextStatusCallback(
		e.ctxManager.UsageRatio(),
		e.ctxManager.TokenCount(),
		e.ctxManager.TokenLimit(),
	)
}

// complete はストリーミング設定とコールバック有無に応じて補完を実行する。
// streaming 有効かつ completer が StreamingCompleter で streamCallback が設定されている場合のみ
// ストリーム経路を使う。それ以外は通常の ChatCompletion を呼ぶ。
// 戻り値は ChatResponse 形式に集約される（履歴追加・usage 集計の互換のため）。
func (e *Engine) complete(ctx context.Context, req *llm.ChatRequest, turn int) (*llm.ChatResponse, error) {
	if !e.streamingEnabled || e.streamCallback == nil {
		return e.completer.ChatCompletion(ctx, req)
	}
	streamer, ok := e.completer.(llm.StreamingCompleter)
	if !ok {
		return e.completer.ChatCompletion(ctx, req)
	}

	ch, err := streamer.ChatCompletionStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("stream start: %w", err)
	}

	var sb strings.Builder
	var finishReason string
	for evt := range ch {
		if evt.Err != nil {
			return nil, fmt.Errorf("stream event: %w", evt.Err)
		}
		if evt.Delta != "" {
			sb.WriteString(evt.Delta)
			e.streamCallback(evt.Delta, turn)
		}
		if evt.FinishReason != "" {
			finishReason = evt.FinishReason
		}
	}

	full := sb.String()
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{
				Index:        0,
				Message:      llm.Message{Role: "assistant", Content: llm.StringPtr(full)},
				FinishReason: finishReason,
			},
		},
	}, nil
}

// chatStep は通常のチャット補完（Phase 2互換）。
// turn は streaming コールバックに渡されるターン番号（0 は未指定）。
func (e *Engine) chatStep(ctx context.Context, turn int) (*LoopResult, error) {
	e.logf("[chat] 応答を生成中...")
	msgs := e.buildMessages()
	req := &llm.ChatRequest{Messages: msgs}

	resp, err := e.complete(ctx, req, turn)
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
func (e *Engine) toolStep(ctx context.Context, turn int) (*LoopResult, error) {
	e.logf("[router] ツールを選択中...")
	rr, usage, err := e.routerStep(ctx)
	if err != nil {
		return nil, fmt.Errorf("tool step: %w", err)
	}

	// tool == "none" → 通常チャットで最終応答を生成
	if rr.Tool == "none" {
		e.logf("[router] ツール不要 → 直接応答 (%s)", rr.Reasoning)
		return e.chatStepWithRouterUsage(ctx, turn, usage)
	}

	// バーチャルツールの検出（ADR-006）
	if rr.Tool == "delegate_task" && e.delegateEnabled {
		return e.delegateStep(ctx, rr, usage)
	}
	if rr.Tool == "coordinate_tasks" && e.coordinatorEnabled {
		return e.coordinateStep(ctx, rr, usage)
	}

	// 同一ターン内で同じスキルツールが成功済みなら直接応答へ切り替える。
	// 「呼ぶと指示が返る」型のツールは1回呼べば十分で、
	// 2回目はモデルが内容を理解せず無限ループする原因になる。
	if e.toolAlreadySucceeded(rr.Tool) {
		e.logf("[router] %s は既に実行済み → 直接応答に切り替え", rr.Tool)
		return e.chatStepWithRouterUsage(ctx, turn, usage)
	}

	e.logf("[router] %s を選択 | 引数: %s", rr.Tool, string(rr.Arguments))
	if rr.Reasoning != "" {
		e.logf("[router] 理由: %s", rr.Reasoning)
	}

	// ツールの取得
	t, ok := e.registry.Get(rr.Tool)
	if !ok {
		return e.recordToolNotFound(rr, usage), nil
	}

	// パーミッション + ガードチェック
	if lr, err := e.checkToolAccess(ctx, t, rr, usage); lr != nil || err != nil {
		return lr, err
	}

	// ツール実行と結果記録
	content, reason := e.executeAndRecord(ctx, t, rr)

	// Verify: 検証ループ（PEVサイクルの V）
	if reason == "tool_use" && e.verifiers.Len() > 0 {
		if lr := e.runVerification(ctx, rr, content, usage); lr != nil {
			return lr, nil
		}
	}

	return &LoopResult{Kind: Continue, Reason: reason, Usage: *usage}, nil
}

// chatStepWithRouterUsage はチャットステップを実行し、ルーターのusageを加算して返す。
func (e *Engine) chatStepWithRouterUsage(ctx context.Context, turn int, routerUsage *llm.Usage) (*LoopResult, error) {
	lr, err := e.chatStep(ctx, turn)
	if err != nil {
		return nil, err
	}
	lr.Usage.PromptTokens += routerUsage.PromptTokens
	lr.Usage.CompletionTokens += routerUsage.CompletionTokens
	lr.Usage.TotalTokens += routerUsage.TotalTokens
	return lr, nil
}

// recordToolNotFound はツール未発見時に履歴へエラーを記録し、LoopResultを返す。
func (e *Engine) recordToolNotFound(rr *routerResponse, usage *llm.Usage) *LoopResult {
	e.logf("[tool] %s が見つかりません", rr.Tool)
	callID := generateCallID()
	errContent := fmt.Sprintf("Error: tool %q not found. Available tools: %s",
		rr.Tool, e.availableToolNames())
	e.ctxManager.Add(ToolCallMessage(callID, rr.Tool, rr.Arguments))
	e.ctxManager.Add(ToolResultMessage(callID, errContent))
	return &LoopResult{Kind: Continue, Reason: "tool_not_found", Usage: *usage}
}

// checkToolAccess はパーミッション→ガードの順でアクセス制御を実行する。
// ブロック時は (*LoopResult, nil)、トリップワイヤ時は (nil, error)、通過時は (nil, nil) を返す。
func (e *Engine) checkToolAccess(ctx context.Context, t tool.Tool, rr *routerResponse, usage *llm.Usage) (*LoopResult, error) {
	if e.permChecker != nil {
		decision := e.permChecker.Check(ctx, t, rr.Arguments)
		if decision == PermDenied {
			e.logf("[permission] %s 拒否", rr.Tool)
			callID := generateCallID()
			e.ctxManager.Add(ToolCallMessage(callID, rr.Tool, rr.Arguments))
			e.ctxManager.Add(ToolResultMessage(callID,
				fmt.Sprintf("Permission denied: tool %q is not allowed by the current policy.", rr.Tool)))
			return &LoopResult{Kind: Continue, Reason: "permission_denied", Usage: *usage}, nil
		}
	}

	if e.guards != nil {
		gr := e.guards.RunToolCall(ctx, rr.Tool, rr.Arguments, e.logf)
		switch gr.Decision {
		case GuardTripwire:
			return nil, &TripwireError{Source: "tool_call", Reason: gr.Reason}
		case GuardDeny:
			e.logf("[guard] %s ブロック: %s", rr.Tool, gr.Reason)
			callID := generateCallID()
			e.ctxManager.Add(ToolCallMessage(callID, rr.Tool, rr.Arguments))
			e.ctxManager.Add(ToolResultMessage(callID, fmt.Sprintf("Blocked by guard: %s", gr.Reason)))
			return &LoopResult{Kind: Continue, Reason: "guard_blocked", Usage: *usage}, nil
		}
	}
	return nil, nil
}

// executeAndRecord はツールを実行し、呼び出し・結果メッセージを履歴に追加する。
// 戻り値は resultContent と reason。
func (e *Engine) executeAndRecord(ctx context.Context, t tool.Tool, rr *routerResponse) (string, string) {
	e.logf("[tool] %s を実行中...", rr.Tool)
	toolCtx := ctx
	if e.workDir != "" {
		toolCtx = tool.ContextWithWorkDir(ctx, e.workDir)
	}
	result, execErr := t.Execute(toolCtx, rr.Arguments)

	callID := generateCallID()
	e.ctxManager.Add(ToolCallMessage(callID, rr.Tool, rr.Arguments))

	var content, reason string
	switch {
	case execErr != nil:
		content = fmt.Sprintf("Error executing tool %q: %s", rr.Tool, execErr.Error())
		reason = "tool_error"
		e.logf("[tool] %s 実行エラー: %s", rr.Tool, execErr.Error())
	case result.IsError:
		content = fmt.Sprintf("Error: %s", result.Content)
		reason = "tool_error"
		e.logf("[tool] %s エラー: %s", rr.Tool, result.Content)
	default:
		content = result.Content
		reason = "tool_use"
		preview := content
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		e.logf("[tool] %s 完了 (%d bytes): %s", rr.Tool, len(content), preview)
	}
	e.ctxManager.Add(ToolResultMessage(callID, content))
	return content, reason
}

// runVerification は検証器を実行し、失敗時は検証エラーを履歴に追加して LoopResult を返す。
// 通過時は nil を返す。
func (e *Engine) runVerification(ctx context.Context, rr *routerResponse, content string, usage *llm.Usage) *LoopResult {
	vr := e.verifiers.RunAll(ctx, rr.Tool, rr.Arguments, content, e.logf)
	if !vr.Passed {
		e.logf("[verify] %s 検証失敗: %s", rr.Tool, vr.Summary)
		verifyMsg := fmt.Sprintf("[Verification Failed]\n%s\nPlease fix the issues and try again.", vr.Summary)
		e.ctxManager.Add(UserMessage(verifyMsg))
		return &LoopResult{Kind: Continue, Reason: "verify_failed", Usage: *usage}
	}
	e.logf("[verify] %s 検証パス", rr.Tool)
	return nil
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
