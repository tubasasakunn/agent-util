package llm

import "context"

// CompleterFunc は関数を Completer インターフェースに適合させるアダプター。
// 既存の Completer 実装なしに、関数リテラルで LLM 呼び出しを定義できる。
type CompleterFunc func(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

// ChatCompletion は f を呼び出す。
func (f CompleterFunc) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	return f(ctx, req)
}
