package llm

import "context"

// llmSessionKey は ChatCompletion 経路で SessionID を伝達する型キー (E2)。
//
// engine が agent.run 開始時にユニークな ID を生成し、ctx に載せて
// completer.ChatCompletion を呼ぶと、RemoteCompleter がこれを取り出して
// llm.execute の SessionID に詰めて送る。ラッパー側 (SDK) はこれをキーに
// Anthropic prompt caching / ollama context などの再利用を判定できる。
type llmSessionKey struct{}

// WithSessionID は ctx に SessionID をセットする。空文字列なら ctx をそのまま返す。
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, llmSessionKey{}, sessionID)
}

// SessionIDFromContext は ctx から SessionID を取り出す。未設定なら空文字。
func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(llmSessionKey{}).(string); ok {
		return v
	}
	return ""
}
