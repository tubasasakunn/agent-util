package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ai-agent/internal/llm"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{
			name: "nil error",
			err:  nil,
			want: ErrClassFatal,
		},
		{
			name: "router parse error",
			err:  &RouterParseError{Cause: errors.New("invalid json")},
			want: ErrClassTransient,
		},
		{
			name: "wrapped router parse error",
			err:  fmt.Errorf("tool step: %w", &RouterParseError{Cause: errors.New("invalid json")}),
			want: ErrClassTransient,
		},
		{
			name: "empty response",
			err:  llm.ErrEmptyResponse,
			want: ErrClassTransient,
		},
		{
			name: "wrapped empty response",
			err:  fmt.Errorf("chat step: %w", llm.ErrEmptyResponse),
			want: ErrClassTransient,
		},
		{
			name: "api error 401",
			err:  &llm.APIError{StatusCode: 401, Body: "unauthorized"},
			want: ErrClassUserFixable,
		},
		{
			name: "api error 403",
			err:  &llm.APIError{StatusCode: 403, Body: "forbidden"},
			want: ErrClassUserFixable,
		},
		{
			name: "api error 400",
			err:  &llm.APIError{StatusCode: 400, Body: "bad request"},
			want: ErrClassUserFixable,
		},
		{
			name: "api error 404",
			err:  &llm.APIError{StatusCode: 404, Body: "not found"},
			want: ErrClassUserFixable,
		},
		{
			name: "wrapped api error 401",
			err:  fmt.Errorf("chat completion: %w", &llm.APIError{StatusCode: 401, Body: "unauthorized"}),
			want: ErrClassUserFixable,
		},
		{
			name: "api error 500 (after client retries)",
			err:  &llm.APIError{StatusCode: 500, Body: "internal server error"},
			want: ErrClassFatal,
		},
		{
			name: "api error 429 (after client retries)",
			err:  &llm.APIError{StatusCode: 429, Body: "rate limited"},
			want: ErrClassFatal,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			want: ErrClassFatal,
		},
		{
			name: "wrapped context canceled",
			err:  fmt.Errorf("request: %w", context.Canceled),
			want: ErrClassFatal,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: ErrClassFatal,
		},
		{
			name: "tripwire error",
			err:  &TripwireError{Source: "input", Reason: "injection detected"},
			want: ErrClassFatal,
		},
		{
			name: "wrapped tripwire error",
			err:  fmt.Errorf("guard: %w", &TripwireError{Source: "tool_call", Reason: "exfiltration"}),
			want: ErrClassFatal,
		},
		{
			name: "unknown error",
			err:  errors.New("something unexpected"),
			want: ErrClassFatal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyError(tt.err)
			if got != tt.want {
				t.Errorf("classifyError() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRouterParseError_Unwrap(t *testing.T) {
	cause := errors.New("invalid json")
	rpe := &RouterParseError{Cause: cause}

	if !errors.Is(rpe, cause) {
		t.Error("RouterParseError should unwrap to its cause")
	}

	wrapped := fmt.Errorf("step: %w", rpe)
	var target *RouterParseError
	if !errors.As(wrapped, &target) {
		t.Error("wrapped RouterParseError should be extractable with errors.As")
	}
}

func TestTripwireError(t *testing.T) {
	tw := &TripwireError{Source: "input", Reason: "injection detected"}

	// Error() メッセージの確認
	want := "tripwire [input]: injection detected"
	if tw.Error() != want {
		t.Errorf("Error() = %q, want %q", tw.Error(), want)
	}

	// errors.As で抽出可能か
	wrapped := fmt.Errorf("run: %w", tw)
	var target *TripwireError
	if !errors.As(wrapped, &target) {
		t.Error("wrapped TripwireError should be extractable with errors.As")
	}
	if target.Source != "input" {
		t.Errorf("Source = %q, want %q", target.Source, "input")
	}
}

func TestCalcStepBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		minD    time.Duration
		maxD    time.Duration
	}{
		{attempt: 0, minD: 750 * time.Millisecond, maxD: 1250 * time.Millisecond},
		{attempt: 1, minD: 1500 * time.Millisecond, maxD: 2500 * time.Millisecond},
		{attempt: 2, minD: 3 * time.Second, maxD: 5 * time.Second},
		{attempt: 3, minD: 6 * time.Second, maxD: 10 * time.Second}, // capped at 8s base
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			for i := 0; i < 100; i++ {
				d := calcStepBackoff(tt.attempt)
				if d < tt.minD || d > tt.maxD {
					t.Errorf("calcStepBackoff(%d) = %v, want [%v, %v]", tt.attempt, d, tt.minD, tt.maxD)
				}
			}
		})
	}
}
