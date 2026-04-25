package engine

import (
	"strings"
	"testing"

	agentctx "ai-agent/internal/context"
)

func TestMemoryIndex_FormatForPrompt(t *testing.T) {
	mi := NewMemoryIndex([]MemoryEntry{
		{Key: "adr-001", Summary: "JSON-RPC over stdio", Path: "decisions/001-json-rpc.md"},
		{Key: "project", Summary: "プロジェクト構造", Path: "CLAUDE.md"},
	})

	text := mi.FormatForPrompt()

	if !strings.Contains(text, "## Knowledge Index") {
		t.Error("missing header")
	}
	if !strings.Contains(text, "[adr-001] JSON-RPC over stdio") {
		t.Error("missing adr-001 entry")
	}
	if !strings.Contains(text, "[project] プロジェクト構造") {
		t.Error("missing project entry")
	}
	if !strings.Contains(text, "decisions/001-json-rpc.md") {
		t.Error("missing adr-001 path")
	}
	if !strings.Contains(text, "read_file") {
		t.Error("missing read_file instruction")
	}
}

func TestMemoryIndex_Empty(t *testing.T) {
	mi := NewMemoryIndex(nil)
	if mi.FormatForPrompt() != "" {
		t.Error("empty index should return empty string")
	}
	if mi.Len() != 0 {
		t.Errorf("Len = %d, want 0", mi.Len())
	}
}

func TestMemoryIndex_TokenCost(t *testing.T) {
	entries := make([]MemoryEntry, 10)
	for i := range entries {
		entries[i] = MemoryEntry{
			Key:     "key-" + string(rune('a'+i)),
			Summary: "Short summary of entry",
			Path:    "docs/entry.md",
		}
	}
	mi := NewMemoryIndex(entries)

	text := mi.FormatForPrompt()
	tokens := agentctx.EstimateTextTokens(text)
	if tokens > 200 {
		t.Errorf("10 entries cost %d tokens, want <= 200", tokens)
	}
}

func TestEngine_WithMemoryEntries(t *testing.T) {
	entries := []MemoryEntry{
		{Key: "adr-001", Summary: "JSON-RPC over stdio", Path: "decisions/001.md"},
	}

	eng := New(&mockCompleter{},
		WithMemoryEntries(entries...),
	)

	// PromptBuilder にメモリインデックスが登録されている
	if !eng.promptBuilder.Has("memory_index") {
		t.Error("memory_index section not registered")
	}

	// router プロンプトに含まれる
	prompt := eng.promptBuilder.BuildRouterSystemPrompt()
	if !strings.Contains(prompt, "Knowledge Index") {
		t.Error("memory index not in router prompt")
	}
	if !strings.Contains(prompt, "adr-001") {
		t.Error("memory entry not in prompt")
	}
}

func TestEngine_WithMemoryEntries_ReservedTokens(t *testing.T) {
	engWithout := New(&mockCompleter{})
	tokensWithout := engWithout.promptBuilder.EstimateReservedTokens()

	entries := []MemoryEntry{
		{Key: "adr-001", Summary: "JSON-RPC over stdio", Path: "decisions/001.md"},
		{Key: "project", Summary: "Project structure", Path: "CLAUDE.md"},
	}
	engWith := New(&mockCompleter{},
		WithMemoryEntries(entries...),
	)
	tokensWith := engWith.promptBuilder.EstimateReservedTokens()

	if tokensWith <= tokensWithout {
		t.Errorf("memory entries should increase reserved tokens: %d <= %d", tokensWith, tokensWithout)
	}
}
