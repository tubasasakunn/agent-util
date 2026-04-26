package builtin

import (
	"context"
	"testing"
)

func TestNonEmptyVerifier(t *testing.T) {
	v := &NonEmptyVerifier{}
	ctx := context.Background()

	r, _ := v.Verify(ctx, "any", nil, "hello")
	if !r.Passed {
		t.Errorf("non-empty result should pass: %s", r.Summary)
	}

	r, _ = v.Verify(ctx, "any", nil, "   \n\t ")
	if r.Passed {
		t.Errorf("whitespace-only should fail")
	}

	r, _ = v.Verify(ctx, "any", nil, "")
	if r.Passed {
		t.Errorf("empty should fail")
	}
}

func TestJSONValidVerifier(t *testing.T) {
	v := &JSONValidVerifier{}
	ctx := context.Background()

	tests := []struct {
		name   string
		result string
		want   bool
	}{
		{"plain_text_skipped", "hello world", true},
		{"empty_skipped", "", true},
		{"valid_object", `{"k":"v","n":1}`, true},
		{"valid_array", `[1,2,3]`, true},
		{"broken_object", `{"k":}`, false},
		{"broken_array", `[1,2,`, false},
		{"with_whitespace", "\n  {\"a\":1}\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := v.Verify(ctx, "any", nil, tt.result)
			if r.Passed != tt.want {
				t.Errorf("passed = %v, want %v (summary=%s)", r.Passed, tt.want, r.Summary)
			}
		})
	}
}
