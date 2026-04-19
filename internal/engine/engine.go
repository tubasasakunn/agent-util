package engine

import (
	"context"
	"fmt"

	"ai-agent/internal/llm"
)

// Engine はエージェントループを管理する。
type Engine struct {
	completer    llm.Completer
	messages     []llm.Message
	maxTurns     int
	systemPrompt string
}

// New は Engine を生成する。
func New(completer llm.Completer, opts ...Option) *Engine {
	cfg := defaultEngineConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Engine{
		completer:    completer,
		maxTurns:     cfg.maxTurns,
		systemPrompt: cfg.systemPrompt,
	}
}

// Run はユーザー入力を受け取り、エージェントループを実行して結果を返す。
// メッセージ履歴は Engine に蓄積され、複数回の Run() でマルチターン対話を実現する。
func (e *Engine) Run(ctx context.Context, input string) (*Result, error) {
	e.messages = append(e.messages, UserMessage(input))

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
func (e *Engine) step(ctx context.Context) (*LoopResult, error) {
	msgs := e.buildMessages()

	resp, err := e.completer.ChatCompletion(ctx, &llm.ChatRequest{
		Messages: msgs,
	})
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("step: %w", llm.ErrEmptyResponse)
	}

	assistantMsg := resp.Choices[0].Message
	e.messages = append(e.messages, assistantMsg)

	// Phase 3: ここにツール呼び出し検出を追加する
	// if len(assistantMsg.ToolCalls) > 0 {
	//     return &LoopResult{Kind: Continue, Reason: "tool_use", ...}, nil
	// }

	return &LoopResult{
		Kind:    Terminal,
		Reason:  "completed",
		Message: assistantMsg,
		Usage:   resp.Usage,
	}, nil
}

// buildMessages はシステムプロンプトと会話履歴を結合してリクエスト用メッセージを構築する。
func (e *Engine) buildMessages() []llm.Message {
	msgs := make([]llm.Message, 0, len(e.messages)+1)
	if e.systemPrompt != "" {
		msgs = append(msgs, SystemMessage(e.systemPrompt))
	}
	msgs = append(msgs, e.messages...)
	return msgs
}
