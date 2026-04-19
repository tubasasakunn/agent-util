package engine

import (
	"errors"

	"ai-agent/internal/llm"
)

// ErrMaxTurnsReached はターン数上限に達した場合のエラー。
var ErrMaxTurnsReached = errors.New("max turns reached")

// ResultKind はエージェントループの継続・終了を表す判別子。
type ResultKind int

const (
	// Continue はループを継続する。
	Continue ResultKind = iota
	// Terminal はループを終了する。
	Terminal
)

// LoopResult は各ステップの結果。Kind でループの継続・終了を判別する。
type LoopResult struct {
	Kind    ResultKind
	Reason  string      // "completed", "tool_use", "max_turns", etc.
	Message llm.Message // このステップのアシスタント応答
	Usage   llm.Usage   // このステップのトークン使用量
}

// Result は Engine.Run() の戻り値。呼び出し元に最終結果を返す。
type Result struct {
	Response string    // アシスタントのテキスト応答
	Reason   string    // 終了理由
	Usage    llm.Usage // 累計トークン使用量
	Turns    int       // 消費ターン数
}
