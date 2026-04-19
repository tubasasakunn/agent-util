package engine

import (
	"io"

	agentctx "ai-agent/internal/context"
	"ai-agent/pkg/tool"
)

// Option は Engine の設定を変更する関数。
type Option func(*engineConfig)

type engineConfig struct {
	maxTurns         int
	systemPrompt     string
	tools            []tool.Tool
	logWriter        io.Writer
	tokenLimit       int
	compaction       *agentctx.CompactionConfig
	delegateEnabled  bool
	delegateMaxChars int
}

const defaultSystemPrompt = "You are a helpful assistant."

func defaultEngineConfig() engineConfig {
	return engineConfig{
		maxTurns:         10,
		systemPrompt:     defaultSystemPrompt,
		tokenLimit:       8192,
		delegateEnabled:  true,
		delegateMaxChars: 1500,
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
