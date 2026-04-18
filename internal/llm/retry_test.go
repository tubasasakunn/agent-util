package llm

import (
	"testing"
	"time"
)

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{200, false},
		{201, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{529, true},
	}
	for _, tt := range tests {
		got := isRetryable(tt.code)
		if got != tt.want {
			t.Errorf("isRetryable(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestCalcBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		minWait time.Duration
		maxWait time.Duration
	}{
		{0, 750 * time.Millisecond, 1250 * time.Millisecond},   // 1s ± 25%
		{1, 1500 * time.Millisecond, 2500 * time.Millisecond},  // 2s ± 25%
		{2, 3 * time.Second, 5 * time.Second},                  // 4s ± 25%
		{3, 6 * time.Second, 10 * time.Second},                 // 8s ± 25%
		{5, 24 * time.Second, 40 * time.Second},                // 32s ± 25% (cap)
		{10, 24 * time.Second, 40 * time.Second},               // 32s cap
	}
	for _, tt := range tests {
		// ジッタがあるため100回実行して範囲内か確認
		for range 100 {
			got := calcBackoff(tt.attempt)
			if got < tt.minWait || got > tt.maxWait {
				t.Errorf("calcBackoff(%d) = %v, want between %v and %v", tt.attempt, got, tt.minWait, tt.maxWait)
				break
			}
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		value string
		want  time.Duration
	}{
		{"5", 5 * time.Second},
		{"1", 1 * time.Second},
		{"60", 60 * time.Second},
		{"0", 0},
		{"-1", 0},
		{"invalid", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := parseRetryAfter(tt.value)
		if got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}
