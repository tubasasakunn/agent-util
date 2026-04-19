package engine

// Option は Engine の設定を変更する関数。
type Option func(*engineConfig)

type engineConfig struct {
	maxTurns     int
	systemPrompt string
}

const defaultSystemPrompt = "You are a helpful assistant."

func defaultEngineConfig() engineConfig {
	return engineConfig{
		maxTurns:     10,
		systemPrompt: defaultSystemPrompt,
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
