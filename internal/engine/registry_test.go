package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRegistry_Register(t *testing.T) {
	reg := NewRegistry()
	t1 := newMockTool("echo", "Echo tool")
	t2 := newMockTool("read_file", "Read file tool")

	if err := reg.Register(t1); err != nil {
		t.Fatalf("Register echo: %v", err)
	}
	if err := reg.Register(t2); err != nil {
		t.Fatalf("Register read_file: %v", err)
	}
	if reg.Len() != 2 {
		t.Errorf("Len = %d, want 2", reg.Len())
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	reg := NewRegistry()
	t1 := newMockTool("echo", "Echo tool")

	if err := reg.Register(t1); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := reg.Register(t1); err == nil {
		t.Error("expected error on duplicate Register, got nil")
	}
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry()
	t1 := newMockTool("echo", "Echo tool")
	reg.Register(t1)

	got, ok := reg.Get("echo")
	if !ok {
		t.Fatal("Get echo: not found")
	}
	if got.Name() != "echo" {
		t.Errorf("Name = %q, want %q", got.Name(), "echo")
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("Get nonexistent: expected false, got true")
	}
}

func TestRegistry_Definitions_Order(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockTool("beta", "B"))
	reg.Register(newMockTool("alpha", "A"))
	reg.Register(newMockTool("gamma", "G"))

	defs := reg.Definitions()
	if len(defs) != 3 {
		t.Fatalf("len = %d, want 3", len(defs))
	}
	want := []string{"beta", "alpha", "gamma"}
	for i, d := range defs {
		if d.Name != want[i] {
			t.Errorf("defs[%d].Name = %q, want %q", i, d.Name, want[i])
		}
	}
}

func TestRegistry_FormatForPrompt(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{
		name:        "echo",
		description: "Echoes a message",
		parameters:  json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}}}`),
	})

	got := reg.FormatForPrompt()
	if !strings.Contains(got, "### echo") {
		t.Error("missing tool name header")
	}
	if !strings.Contains(got, "Echoes a message") {
		t.Error("missing description")
	}
	if !strings.Contains(got, `"message"`) {
		t.Error("missing parameter definition")
	}
}

func TestRegistry_FormatForPrompt_Empty(t *testing.T) {
	reg := NewRegistry()
	if got := reg.FormatForPrompt(); got != "" {
		t.Errorf("FormatForPrompt on empty registry = %q, want empty", got)
	}
}

func TestScopedFormatForPrompt_MaxTools(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockTool("alpha", "A tool"))
	reg.Register(newMockTool("beta", "B tool"))
	reg.Register(newMockTool("gamma", "G tool"))

	got := reg.ScopedFormatForPrompt(ToolScope{MaxTools: 2})

	if !strings.Contains(got, "### alpha") {
		t.Error("should contain alpha (first registered)")
	}
	if !strings.Contains(got, "### beta") {
		t.Error("should contain beta (second registered)")
	}
	if strings.Contains(got, "### gamma") {
		t.Error("should NOT contain gamma (exceeds MaxTools)")
	}
}

func TestScopedFormatForPrompt_IncludeAlways(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockTool("alpha", "A tool"))
	reg.Register(newMockTool("beta", "B tool"))
	reg.Register(newMockTool("gamma", "G tool"))

	got := reg.ScopedFormatForPrompt(ToolScope{
		MaxTools:      2,
		IncludeAlways: map[string]bool{"gamma": true},
	})

	if !strings.Contains(got, "### gamma") {
		t.Error("IncludeAlways tool should always be present")
	}
	// gamma + 残り枠1 = alpha
	if !strings.Contains(got, "### alpha") {
		t.Error("should fill remaining slots with first registered")
	}
	if strings.Contains(got, "### beta") {
		t.Error("should NOT contain beta (bumped by IncludeAlways)")
	}
}

func TestScopedFormatForPrompt_ZeroMaxTools(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockTool("alpha", "A tool"))
	reg.Register(newMockTool("beta", "B tool"))

	// MaxTools=0 は無制限
	got := reg.ScopedFormatForPrompt(ToolScope{MaxTools: 0})

	if !strings.Contains(got, "### alpha") {
		t.Error("should contain alpha")
	}
	if !strings.Contains(got, "### beta") {
		t.Error("should contain beta")
	}
}

func TestScopedFormatForPrompt_MaxToolsExceedsTotal(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMockTool("alpha", "A tool"))

	got := reg.ScopedFormatForPrompt(ToolScope{MaxTools: 10})

	if !strings.Contains(got, "### alpha") {
		t.Error("should contain all tools when MaxTools exceeds total")
	}
}

func TestScopedFormatForPrompt_Empty(t *testing.T) {
	reg := NewRegistry()
	got := reg.ScopedFormatForPrompt(ToolScope{MaxTools: 3})
	if got != "" {
		t.Errorf("empty registry scoped = %q, want empty", got)
	}
}
