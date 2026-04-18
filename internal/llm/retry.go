package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// doWithRetry はHTTPリクエストをリトライ付きで実行する。
// bodyBytes はリトライごとにリクエストボディを再構築するために保持する。
func (c *Client) doWithRetry(ctx context.Context, bodyBytes []byte, req *http.Request) (*http.Response, error) {
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("request canceled: %w", ctx.Err())
		default:
		}

		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt == c.maxRetries {
				return nil, fmt.Errorf("http request (attempt %d/%d): %w", attempt+1, c.maxRetries+1, err)
			}
			if err := sleepWithContext(ctx, calcBackoff(attempt)); err != nil {
				return nil, fmt.Errorf("request canceled: %w", err)
			}
			continue
		}

		if !isRetryable(resp.StatusCode) {
			return resp, nil
		}

		resp.Body.Close()

		if attempt == c.maxRetries {
			return nil, fmt.Errorf("max retries exceeded: last status %d", resp.StatusCode)
		}

		wait := calcBackoff(attempt)
		if ra := parseRetryAfter(resp.Header.Get("Retry-After")); ra > 0 {
			wait = ra
		}
		if err := sleepWithContext(ctx, wait); err != nil {
			return nil, fmt.Errorf("request canceled: %w", err)
		}
	}
	return nil, fmt.Errorf("unexpected: retry loop exhausted")
}

// isRetryable はステータスコードがリトライ対象か判定する。
func isRetryable(statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 529:
		return true
	default:
		return false
	}
}

// calcBackoff はattempt回目のバックオフ時間を計算する。
// 基本: min(1s * 2^attempt, 32s) に 25% のジッタを加える。
func calcBackoff(attempt int) time.Duration {
	base := math.Min(float64(time.Second)*math.Pow(2, float64(attempt)), float64(32*time.Second))
	jitter := base * 0.25 * (rand.Float64()*2 - 1) // -25% 〜 +25%
	return time.Duration(base + jitter)
}

// parseRetryAfter は Retry-After ヘッダの値（秒数）をパースする。
// パース失敗時は 0 を返す。
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	secs, err := strconv.Atoi(value)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// sleepWithContext はコンテキストのキャンセルを考慮してスリープする。
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
