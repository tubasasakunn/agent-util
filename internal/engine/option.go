package engine

import (
	"io"

	agentctx "ai-agent/internal/context"
	"ai-agent/pkg/tool"
)

// Option は Engine の設定を変更する関数。
type Option func(*engineConfig)

type engineConfig struct {
	maxTurns           int
	systemPrompt       string
	tools              []tool.Tool
	logWriter          io.Writer
	tokenLimit         int
	compaction         *agentctx.CompactionConfig
	delegateEnabled    bool
	delegateMaxChars   int
	workDir            string
	coordinatorEnabled bool
	coordinateMaxChars int
	dynamicSections    []Section
	reminderThreshold  int
	memoryEntries      []MemoryEntry
	toolScope          *ToolScope
}

const defaultSystemPrompt = "You are a helpful assistant."

const defaultReminderThreshold = 8

func defaultEngineConfig() engineConfig {
	return engineConfig{
		maxTurns:           10,
		systemPrompt:       defaultSystemPrompt,
		tokenLimit:         8192,
		delegateEnabled:    true,
		delegateMaxChars:   1500,
		coordinatorEnabled: true,
		coordinateMaxChars: 3000,
		reminderThreshold:  defaultReminderThreshold,
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
