package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// StreamEvent はストリーミングレスポンスの1イベント。
// Delta はトークン差分テキスト、FinishReason はストリーム終了時のみ非空、
// Err は致命的なエラー時に設定される（チャネルはその直後にクローズされる）。
type StreamEvent struct {
	Delta        string
	FinishReason string
	Err          error
}

// StreamingCompleter はストリーミング対応の Completer。
// 既存の Completer インターフェースを壊さないため別インターフェースとして定義する。
type StreamingCompleter interface {
	Completer
	ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error)
}

// streamChunk は OpenAI 互換 SSE ストリーミング応答の1チャンク。
type streamChunk struct {
	Choices []streamChoice `json:"choices"`
}

type streamChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
	Index        int         `json:"index"`
}

type streamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// ChatCompletionStream は SSE ベースのストリーミング補完を実行する。
// 戻り値のチャネルは送信側（このメソッド）がクローズする。
// ctx キャンセル時はチャネルをクローズして goroutine を終了させる。
func (c *Client) ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	if req.Model == "" {
		req.Model = c.model
	}
	streamReq := *req
	streamReq.Stream = true

	c.logRequest(&streamReq)
	start := time.Now()

	bodyBytes, err := json.Marshal(&streamReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := c.buildHTTPRequest(ctx, bodyBytes)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.doWithRetry(ctx, bodyBytes, httpReq)
	if err != nil {
		return nil, fmt.Errorf("chat completion stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	out := make(chan StreamEvent, 16)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		c.parseSSEStream(ctx, resp.Body, out, start)
	}()
	return out, nil
}

// parseSSEStream は SSE 形式のレスポンスを読み取って StreamEvent をチャネルに送る。
func (c *Client) parseSSEStream(ctx context.Context, body io.Reader, out chan<- StreamEvent, start time.Time) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var totalDelta int
	var lastFinish string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			sendEvent(ctx, out, StreamEvent{Err: ctx.Err()})
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if len(payload) == 0 {
			continue
		}
		if bytes.Equal(payload, []byte("[DONE]")) {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal(payload, &chunk); err != nil {
			sendEvent(ctx, out, StreamEvent{Err: fmt.Errorf("parse stream chunk: %w", err)})
			return
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			totalDelta += len(choice.Delta.Content)
			if !sendEvent(ctx, out, StreamEvent{Delta: choice.Delta.Content}) {
				return
			}
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			lastFinish = *choice.FinishReason
		}
	}
	if err := scanner.Err(); err != nil {
		if !errors.Is(err, io.EOF) {
			sendEvent(ctx, out, StreamEvent{Err: fmt.Errorf("read stream: %w", err)})
			return
		}
	}
	sendEvent(ctx, out, StreamEvent{FinishReason: lastFinish})
	c.logStream(totalDelta, lastFinish, time.Since(start))
}

// sendEvent はチャネルへの送信時にctxキャンセルを尊重する。
func sendEvent(ctx context.Context, out chan<- StreamEvent, evt StreamEvent) bool {
	select {
	case out <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}

// logStream はストリーム完了の概要をログ出力する。
func (c *Client) logStream(totalChars int, finish string, elapsed time.Duration) {
	if c.logw == nil {
		return
	}
	c.logf("[llm] ← stream %d chars finish=%s %.1fs", totalChars, finish, elapsed.Seconds())
}
