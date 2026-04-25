package engine

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"time"

	"ai-agent/internal/llm"
)

// ErrorClass はエラーの4分類を表す。
// LangGraph のエラー分類に基づく。
type ErrorClass int

const (
	// ErrClassTransient は一時的エラー。指数バックオフでリトライする。
	// 例: ルーターパースエラー、空レスポンス。
	ErrClassTransient ErrorClass = iota
	// ErrClassLLMRecoverable はLLM回復可能エラー。
	// エラーをツール結果として履歴に追加し、モデルが自己修正する。
	// toolStep() 内で既に処理されるため、Run() レベルには通常到達しない。
	ErrClassLLMRecoverable
	// ErrClassUserFixable はユーザー修正可能エラー。人間の介入を要求する。
	// 例: 認証エラー（401/403）。
	ErrClassUserFixable
	// ErrClassFatal は予期しないエラー。安全停止する。
	// 例: context キャンセル、リトライ上限到達後のAPIエラー。
	ErrClassFatal
)

// RouterParseError はルーターのJSON出力パースに失敗した場合のエラー。
// SLLMは不安定なJSONを返すことがあり、これは一時的エラーとして分類される。
type RouterParseError struct {
	Cause error
}

func (e *RouterParseError) Error() string {
	return "router parse: " + e.Cause.Error()
}

func (e *RouterParseError) Unwrap() error {
	return e.Cause
}

// classifyError はエラーを4分類に分類する。
func classifyError(err error) ErrorClass {
	if err == nil {
		return ErrClassFatal
	}

	// context キャンセル → Fatal（リトライ無意味）
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrClassFatal
	}

	// トリップワイヤ → Fatal（即時停止）
	var tw *TripwireError
	if errors.As(err, &tw) {
		return ErrClassFatal
	}

	// ルーターパースエラー → Transient（SLLMのJSON出力は不安定）
	var rpe *RouterParseError
	if errors.As(err, &rpe) {
		return ErrClassTransient
	}

	// 空レスポンス → Transient（SLLMは時々空を返す）
	if errors.Is(err, llm.ErrEmptyResponse) {
		return ErrClassTransient
	}

	// APIエラー → ステータスコードで分類
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 401, 403:
			return ErrClassUserFixable
		case 400, 404:
			return ErrClassUserFixable
		default:
			// 429/5xx は LLM クライアント層で既にリトライ済み。
			// ここに到達した場合はリトライ上限を超えた状態なので Fatal。
			return ErrClassFatal
		}
	}

	return ErrClassFatal
}

// calcStepBackoff はstepリトライのバックオフ時間を計算する。
// 基本: min(1s * 2^attempt, 8s) に 25% のジッタを加える。
// HTTP層のバックオフ（最大32s）より短めに設定。
func calcStepBackoff(attempt int) time.Duration {
	base := math.Min(float64(time.Second)*math.Pow(2, float64(attempt)), float64(8*time.Second))
	jitter := base * 0.25 * (rand.Float64()*2 - 1)
	return time.Duration(base + jitter)
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
