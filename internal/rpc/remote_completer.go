package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ai-agent/internal/llm"
	"ai-agent/pkg/protocol"
)

// DefaultLLMTimeout は llm.execute のデフォルトタイムアウト。
// ラッパー側の LLM 呼び出しは外部 API を叩く可能性があるためツールよりやや長め。
const DefaultLLMTimeout = 120 * time.Second

// SessionID の context.Context 伝達は internal/llm/session.go に統合した (E2)。

// RemoteCompleter はラッパー側に LLM 呼び出しを委譲する Completer。
// llm.Completer インターフェースを実装する。
//
// agent.configure で llm.mode="remote" を指定したときに使われる。
// すべての ChatCompletion 呼び出しが llm.execute (コア → ラッパー) として送られ、
// ラッパー側で任意の API 形式 (Anthropic / Bedrock / ollama / mock 等) に変換できる。
//
// callIndex はインスタンス単位の通し番号で、ラッパーが KV cache や Anthropic の
// prompt caching を維持するためのヒントとして llm.execute に載せて送る (E2)。
// 同一 RemoteCompleter を共有するすべての呼び出しでカウントされるため、
// 厳密な「単一 agent.run 内」と完全一致はしないが、ラッパー側で session_id と
// 組み合わせれば実用上問題ない。
type RemoteCompleter struct {
	server    *Server
	timeout   time.Duration
	callIndex int64 // atomic でなくとも sequence 性は不要 (cache hint なので)
}

// NewRemoteCompleter は RemoteCompleter を生成する。
// timeout が 0 以下のときは DefaultLLMTimeout を使う。
func NewRemoteCompleter(server *Server, timeout time.Duration) *RemoteCompleter {
	if timeout <= 0 {
		timeout = DefaultLLMTimeout
	}
	return &RemoteCompleter{server: server, timeout: timeout}
}

// ChatCompletion は llm.execute をラッパーへ送信し、結果を ChatResponse にデコードする。
func (c *RemoteCompleter) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	execCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// ChatRequest をそのまま透過させる (将来フィールドが増えてもプロトコル変更不要)
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("remote llm: marshal request: %w", err)
	}

	// E2: KV cache 用ヒントを ChatRequest と一緒に送る
	idx := c.callIndex
	c.callIndex++
	params := protocol.LLMExecuteParams{
		Request:   reqJSON,
		SessionID: llm.SessionIDFromContext(ctx),
		CallIndex: int(idx),
	}

	resp, err := c.server.SendRequest(execCtx, protocol.MethodLLMExecute, params)
	if err != nil {
		return nil, fmt.Errorf("remote llm: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("remote llm: wrapper returned error: %s", resp.Error.Message)
	}

	var result protocol.LLMExecuteResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("remote llm: unmarshal result: %w", err)
	}
	if len(result.Response) == 0 {
		return nil, fmt.Errorf("remote llm: empty response")
	}

	var chatResp llm.ChatResponse
	if err := json.Unmarshal(result.Response, &chatResp); err != nil {
		return nil, fmt.Errorf("remote llm: unmarshal chat response: %w", err)
	}
	return &chatResp, nil
}
