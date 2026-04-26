package engine

import (
	"io"

	agentctx "ai-agent/internal/context"
	"ai-agent/pkg/tool"
)

// Option は Engine の設定を変更する関数。
type Option func(*engineConfig)

type engineConfig struct {
	maxTurns               int
	systemPrompt           string
	tools                  []tool.Tool
	logWriter              io.Writer
	tokenLimit             int
	compaction             *agentctx.CompactionConfig
	delegateEnabled        bool
	delegateMaxChars       int
	workDir                string
	coordinatorEnabled     bool
	coordinateMaxChars     int
	dynamicSections        []Section
	reminderThreshold      int
	memoryEntries          []MemoryEntry
	toolScope              *ToolScope
	maxStepRetries         int
	maxConsecutiveFailures int
	verifiers              []Verifier
	permissionPolicy       *PermissionPolicy
	userApprover           UserApprover
	auditWriter            io.Writer
	inputGuards            []InputGuard
	toolCallGuards         []ToolCallGuard
	outputGuards           []OutputGuard
	stepCallback           StepCallback
	streamingEnabled       bool
	streamCallback         StreamCallback
	contextStatusCallback  ContextStatusCallback
}

// StepEvent はエージェントループの各ステップ完了時に発火するイベント。
type StepEvent struct {
	Turn       int     // 現在のターン番号（1始まり）
	Reason     string  // ステップの結果理由
	Response   string  // Terminal 時のアシスタント応答（Continue 時は空）
	UsageRatio float64 // コンテキスト使用率
	TokenCount int     // 現在のトークン数
	TokenLimit int     // トークン上限
}

// StepCallback はステップ完了時のコールバック関数型。
type StepCallback func(StepEvent)

// StreamCallback はトークン差分を受け取るコールバック関数型。
// turn は1始まりのターン番号、delta はモデルが新たに生成したテキスト断片。
type StreamCallback func(delta string, turn int)

// ContextStatusCallback はコンテキスト使用率の変化を受け取るコールバック関数型。
type ContextStatusCallback func(ratio float64, count, limit int)

const defaultSystemPrompt = "You are a helpful assistant."

const defaultReminderThreshold = 8

func defaultEngineConfig() engineConfig {
	return engineConfig{
		maxTurns:               10,
		systemPrompt:           defaultSystemPrompt,
		tokenLimit:             8192,
		delegateEnabled:        true,
		delegateMaxChars:       1500,
		coordinatorEnabled:     true,
		coordinateMaxChars:     3000,
		reminderThreshold:      defaultReminderThreshold,
		maxStepRetries:         2,
		maxConsecutiveFailures: 3,
	}
}

// WithMaxTurns は1回の Run() で許可する最大ターン数を設定する。
func WithMaxTurns(n int) Option {
	return func(c *engineConfig) { c.maxTurns = n }
}

// WithSystemPrompt はシステムプロンプトを設定する。
// 空文字を指定するとシステムプロンプトを省略する。
func WithSystemPrompt(prompt string) Option {
	return func(c *engineConfig) { c.systemPrompt = prompt }
}

// WithTools は利用可能なツールを設定する。
// ツールが登録されるとルーターステップが有効になる。
func WithTools(tools ...tool.Tool) Option {
	return func(c *engineConfig) {
		c.tools = append(c.tools, tools...)
	}
}

// WithLogWriter はログ出力先を設定する。
// 設定するとルーター選択、ツール実行、応答生成の過程がログ出力される。
// 通常は os.Stderr を渡す。nil の場合はログを出力しない。
func WithLogWriter(w io.Writer) Option {
	return func(c *engineConfig) { c.logWriter = w }
}

// WithTokenLimit はコンテキストのトークン上限を設定する。
// デフォルトは 8192。
func WithTokenLimit(n int) Option {
	return func(c *engineConfig) { c.tokenLimit = n }
}

// WithCompaction は縮約カスケードの設定を有効にする。
// この設定がない場合、閾値超過時もログ出力のみで縮約は実行しない。
func WithCompaction(cfg agentctx.CompactionConfig) Option {
	return func(c *engineConfig) {
		c.compaction = &cfg
	}
}

// WithDelegateEnabled は delegate_task（サブエージェント委譲）の有効/無効を設定する。
// デフォルトは true。Fork() で生成した子Engine では false に設定してネスト再帰を防止する。
func WithDelegateEnabled(enabled bool) Option {
	return func(c *engineConfig) { c.delegateEnabled = enabled }
}

// WithDelegateMaxChars はサブエージェント結果の凝縮時の最大文字数を設定する。
// デフォルトは 1500。
func WithDelegateMaxChars(n int) Option {
	return func(c *engineConfig) { c.delegateMaxChars = n }
}

// WithCoordinatorEnabled は coordinate_tasks（並列サブエージェント）の有効/無効を設定する。
// デフォルトは true。Fork() で生成した子Engine では false に設定してネスト再帰を防止する。
func WithCoordinatorEnabled(enabled bool) Option {
	return func(c *engineConfig) { c.coordinatorEnabled = enabled }
}

// WithCoordinateMaxChars は並列タスク結果集約時の最大合計文字数を設定する。
// デフォルトは 3000。各タスクの結果は coordinateMaxChars / タスク数 に制限される。
func WithCoordinateMaxChars(n int) Option {
	return func(c *engineConfig) { c.coordinateMaxChars = n }
}

// WithWorkDir はツール実行時のワーキングディレクトリを設定する。
// 設定すると tool.ContextWithWorkDir 経由でツール実行コンテキストに注入される。
// Worktree実行モデルで使用する。
func WithWorkDir(dir string) Option {
	return func(c *engineConfig) { c.workDir = dir }
}

// WithDynamicSection はカスタムのプロンプトセクションを追加する。
// 複数回呼び出すと追加される。Key が重複する場合は後勝ち。
func WithDynamicSection(s Section) Option {
	return func(c *engineConfig) {
		c.dynamicSections = append(c.dynamicSections, s)
	}
}

// WithReminderThreshold はリマインダー挿入の閾値（メッセージ数）を設定する。
// 会話履歴がこの数以上の場合に末尾リマインダーが挿入される。
// デフォルトは 8。0 に設定するとリマインダーを無効化する。
func WithReminderThreshold(n int) Option {
	return func(c *engineConfig) { c.reminderThreshold = n }
}

// WithMemoryEntries は MEMORY インデックスのエントリを設定する。
// エントリはプロンプトに軽量ポインタとして常時載り、実体は read_file で読み込む。
func WithMemoryEntries(entries ...MemoryEntry) Option {
	return func(c *engineConfig) {
		c.memoryEntries = append(c.memoryEntries, entries...)
	}
}

// WithToolScope はツールスコーピングを設定する。
// MaxTools でルーターに提示するツール数を制限し、IncludeAlways で必須ツールを保証する。
// nil（デフォルト）の場合は全ツールを提示する。
func WithToolScope(scope ToolScope) Option {
	return func(c *engineConfig) { c.toolScope = &scope }
}

// WithMaxStepRetries は step() の一時的エラーのリトライ上限を設定する。
// デフォルトは 2。LLMクライアント層のリトライとは別レイヤー。
func WithMaxStepRetries(n int) Option {
	return func(c *engineConfig) { c.maxStepRetries = n }
}

// WithMaxConsecutiveFailures は連続失敗の上限を設定する。
// ツール実行エラー・検証失敗が連続した場合に安全停止する。
// デフォルトは 3。
func WithMaxConsecutiveFailures(n int) Option {
	return func(c *engineConfig) { c.maxConsecutiveFailures = n }
}

// WithVerifiers は検証器を設定する。
// ツール実行後に全検証器が順に実行され、失敗時は自動修正ループに入る。
func WithVerifiers(vs ...Verifier) Option {
	return func(c *engineConfig) {
		c.verifiers = append(c.verifiers, vs...)
	}
}

// WithPermissionPolicy はパーミッションポリシーを設定する。
// deny→allow→readOnly→ask→fail-closed のパイプラインでツール実行を制御する。
// 未設定（nil）の場合は全ツールを許可する（後方互換）。
func WithPermissionPolicy(policy PermissionPolicy) Option {
	return func(c *engineConfig) {
		c.permissionPolicy = &policy
	}
}

// WithUserApprover はユーザー承認インターフェースを設定する。
// パーミッションパイプラインでask判定時にユーザーに確認する。
// nil の場合はask→deny（fail-closed）。
func WithUserApprover(approver UserApprover) Option {
	return func(c *engineConfig) { c.userApprover = approver }
}

// WithAuditWriter は監査ログの出力先を設定する。
// 未設定の場合は logWriter と同じ出力先を使用する。
func WithAuditWriter(w io.Writer) Option {
	return func(c *engineConfig) { c.auditWriter = w }
}

// WithInputGuards は入力ガードレールを設定する。
// ユーザー入力の検証（インジェクション検知等）に使用する。
func WithInputGuards(guards ...InputGuard) Option {
	return func(c *engineConfig) {
		c.inputGuards = append(c.inputGuards, guards...)
	}
}

// WithToolCallGuards はツール呼び出しガードレールを設定する。
// ツール呼び出しの引数の安全性検証に使用する。
func WithToolCallGuards(guards ...ToolCallGuard) Option {
	return func(c *engineConfig) {
		c.toolCallGuards = append(c.toolCallGuards, guards...)
	}
}

// WithOutputGuards は出力ガードレールを設定する。
// 最終出力の安全性検証（機密情報の漏洩検知等）に使用する。
func WithOutputGuards(guards ...OutputGuard) Option {
	return func(c *engineConfig) {
		c.outputGuards = append(c.outputGuards, guards...)
	}
}

// WithStepCallback はステップ完了時のコールバックを設定する。
// JSON-RPC サーバーモードでストリーミング通知に使用する。
func WithStepCallback(cb StepCallback) Option {
	return func(c *engineConfig) { c.stepCallback = cb }
}

// WithStreaming はトークンストリーミングを有効化する。
// 有効時は chatStep / routerStep が ChatCompletionStream を使用する。
// completer が StreamingCompleter を実装していない場合はフォールバックして通常の補完を使う。
func WithStreaming(enabled bool) Option {
	return func(c *engineConfig) { c.streamingEnabled = enabled }
}

// WithStreamCallback はトークン差分受信時のコールバックを設定する。
// JSON-RPC サーバーモードで stream.delta 通知の送信に使用する。
func WithStreamCallback(cb StreamCallback) Option {
	return func(c *engineConfig) { c.streamCallback = cb }
}

// WithContextStatusCallback はコンテキスト使用率変化時のコールバックを設定する。
// 各ターン開始時と縮約完了時に発火する。
func WithContextStatusCallback(cb ContextStatusCallback) Option {
	return func(c *engineConfig) { c.contextStatusCallback = cb }
}
