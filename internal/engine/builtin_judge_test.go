package engine

import (
	"context"
	"strings"
	"testing"
)

func TestBuildBuiltinGoalJudge_MinLength(t *testing.T) {
	j, err := BuildBuiltinGoalJudge("min_length:10")
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	ctx := context.Background()

	// 短すぎる → 継続
	terminate, _, err := j.ShouldTerminate(ctx, "短い", 1)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if terminate {
		t.Error("3 chars should NOT terminate min_length:10")
	}

	// 十分長い → 終了
	terminate, reason, err := j.ShouldTerminate(ctx, "これは十分に長い応答です", 1)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !terminate {
		t.Error("long string should terminate min_length:10")
	}
	if !strings.Contains(reason, "min_length") {
		t.Errorf("reason = %q, want to contain min_length", reason)
	}
}

func TestBuildBuiltinGoalJudge_Contains(t *testing.T) {
	j, err := BuildBuiltinGoalJudge("contains:FINAL")
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	ctx := context.Background()

	terminate, _, _ := j.ShouldTerminate(ctx, "hello world", 1)
	if terminate {
		t.Error("no keyword → should NOT terminate")
	}

	terminate, reason, _ := j.ShouldTerminate(ctx, "FINAL ANSWER: 42", 1)
	if !terminate {
		t.Error("keyword present → should terminate")
	}
	if !strings.Contains(reason, "contains") {
		t.Errorf("reason = %q, want to contain 'contains'", reason)
	}
}

func TestBuildBuiltinGoalJudge_InvalidSpec(t *testing.T) {
	tests := []string{
		"",
		"min_length",
		"min_length:abc",
		"min_length:-5",
		"contains:",
		"unknown:value",
	}
	for _, spec := range tests {
		t.Run(spec, func(t *testing.T) {
			_, err := BuildBuiltinGoalJudge(spec)
			if err == nil {
				t.Errorf("spec %q should fail", spec)
			}
		})
	}
}

func TestRegistry_ToolBudgetExcludesExceeded(t *testing.T) {
	reg := NewRegistry()
	for _, name := range []string{"a", "b", "c"} {
		if err := reg.Register(newMockTool(name, "test tool "+name)); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}

	scope := ToolScope{
		ToolBudget: map[string]int{"a": 1, "b": 2},
	}
	calls := map[string]int{
		"a": 1, // 上限到達 → 除外される
		"b": 1, // まだ予算内
	}
	out := reg.ScopedFormatForPromptWithCalls(scope, calls)
	if strings.Contains(out, "### a\n") {
		t.Error("tool 'a' should be excluded (budget reached)")
	}
	if !strings.Contains(out, "### b\n") {
		t.Error("tool 'b' should still be present (1/2)")
	}
	if !strings.Contains(out, "### c\n") {
		t.Error("tool 'c' should be present (no budget)")
	}
}
