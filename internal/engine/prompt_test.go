package engine

import (
	"strings"
	"testing"
)

func TestPromptBuilder_SectionOrdering(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{Key: "instructions", Priority: 80, Scope: ScopeRouter, Content: "## Instructions\nSelect a tool."})
	pb.Add(Section{Key: "system", Priority: PrioritySystem, Scope: ScopeAll, Content: "You are a helpful assistant."})
	pb.Add(Section{Key: "tools", Priority: PriorityTools, Scope: ScopeRouter, Content: "## Available Tools\n### echo"})

	prompt := pb.BuildRouterSystemPrompt()

	sysIdx := strings.Index(prompt, "You are a helpful assistant.")
	toolsIdx := strings.Index(prompt, "## Available Tools")
	instrIdx := strings.Index(prompt, "## Instructions")

	if sysIdx < 0 || toolsIdx < 0 || instrIdx < 0 {
		t.Fatalf("missing sections in prompt:\n%s", prompt)
	}
	if sysIdx >= toolsIdx {
		t.Errorf("system (%d) should come before tools (%d)", sysIdx, toolsIdx)
	}
	if toolsIdx >= instrIdx {
		t.Errorf("tools (%d) should come before instructions (%d)", toolsIdx, instrIdx)
	}
}

func TestPromptBuilder_ScopeFiltering(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{Key: "system", Priority: PrioritySystem, Scope: ScopeAll, Content: "base prompt"})
	pb.Add(Section{Key: "tools", Priority: PriorityTools, Scope: ScopeRouter, Content: "tool list"})

	chat := pb.BuildSystemPrompt()
	router := pb.BuildRouterSystemPrompt()

	if !strings.Contains(chat, "base prompt") {
		t.Error("chat prompt should contain base prompt")
	}
	if strings.Contains(chat, "tool list") {
		t.Error("chat prompt should NOT contain router-only sections")
	}

	if !strings.Contains(router, "base prompt") {
		t.Error("router prompt should contain base prompt")
	}
	if !strings.Contains(router, "tool list") {
		t.Error("router prompt should contain tool list")
	}
}

func TestPromptBuilder_DynamicSection(t *testing.T) {
	callCount := 0
	pb := NewPromptBuilder()
	pb.Add(Section{
		Key:      "dynamic",
		Priority: PriorityDeveloper,
		Scope:    ScopeAll,
		Dynamic: func() string {
			callCount++
			return "dynamic content"
		},
	})

	prompt := pb.BuildSystemPrompt()
	if !strings.Contains(prompt, "dynamic content") {
		t.Error("prompt should contain dynamic content")
	}
	if callCount != 1 {
		t.Errorf("dynamic func called %d times, want 1", callCount)
	}
}

func TestPromptBuilder_DynamicOverridesContent(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{
		Key:      "test",
		Priority: PrioritySystem,
		Scope:    ScopeAll,
		Content:  "static",
		Dynamic:  func() string { return "dynamic" },
	})

	prompt := pb.BuildSystemPrompt()
	if strings.Contains(prompt, "static") {
		t.Error("should use Dynamic over Content")
	}
	if !strings.Contains(prompt, "dynamic") {
		t.Error("should contain dynamic content")
	}
}

func TestPromptBuilder_AddOverwrite(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{Key: "system", Priority: PrioritySystem, Scope: ScopeAll, Content: "v1"})
	pb.Add(Section{Key: "system", Priority: PrioritySystem, Scope: ScopeAll, Content: "v2"})

	prompt := pb.BuildSystemPrompt()
	if strings.Contains(prompt, "v1") {
		t.Error("should have overwritten v1 with v2")
	}
	if !strings.Contains(prompt, "v2") {
		t.Error("should contain v2")
	}
}

func TestPromptBuilder_Remove(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{Key: "a", Priority: 0, Scope: ScopeAll, Content: "content-a"})
	pb.Add(Section{Key: "b", Priority: 10, Scope: ScopeAll, Content: "content-b"})
	pb.Remove("a")

	prompt := pb.BuildSystemPrompt()
	if strings.Contains(prompt, "content-a") {
		t.Error("removed section should not appear")
	}
	if !strings.Contains(prompt, "content-b") {
		t.Error("remaining section should appear")
	}
}

func TestPromptBuilder_EmptySections(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{Key: "empty", Priority: 0, Scope: ScopeAll, Content: ""})
	pb.Add(Section{Key: "present", Priority: 10, Scope: ScopeAll, Content: "hello"})

	prompt := pb.BuildSystemPrompt()
	if prompt != "hello" {
		t.Errorf("prompt = %q, want %q (empty sections should be skipped)", prompt, "hello")
	}
}

func TestPromptBuilder_EstimateReservedTokens(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{Key: "system", Priority: PrioritySystem, Scope: ScopeAll, Content: "You are a helpful assistant."})
	pb.Add(Section{Key: "tools", Priority: PriorityTools, Scope: ScopeRouter, Content: "## Available Tools\n### echo\nEchoes a message."})

	tokens := pb.EstimateReservedTokens()
	if tokens <= 0 {
		t.Errorf("tokens = %d, should be positive", tokens)
	}

	// キャッシュの検証: 2回目は dirty=false で同じ値を返す
	if pb.IsDirty() {
		t.Error("should not be dirty after EstimateReservedTokens")
	}
	tokens2 := pb.EstimateReservedTokens()
	if tokens2 != tokens {
		t.Errorf("cached tokens = %d, want %d", tokens2, tokens)
	}
}

func TestPromptBuilder_DirtyFlag(t *testing.T) {
	pb := NewPromptBuilder()
	if !pb.IsDirty() {
		t.Error("new builder should be dirty")
	}

	pb.EstimateReservedTokens()
	if pb.IsDirty() {
		t.Error("should not be dirty after estimation")
	}

	pb.Add(Section{Key: "new", Priority: 0, Scope: ScopeAll, Content: "new content"})
	if !pb.IsDirty() {
		t.Error("should be dirty after Add")
	}

	pb.EstimateReservedTokens()
	pb.Remove("new")
	if !pb.IsDirty() {
		t.Error("should be dirty after Remove")
	}
}

func TestPromptBuilder_Has(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{Key: "a", Priority: 0, Scope: ScopeAll, Content: "x"})

	if !pb.Has("a") {
		t.Error("should have key 'a'")
	}
	if pb.Has("b") {
		t.Error("should not have key 'b'")
	}
}

func TestPromptBuilder_Resolve(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{Key: "reminder", Priority: PriorityReminder, Scope: ScopeManual, Content: "Remember the rules."})

	content, ok := pb.Resolve("reminder")
	if !ok {
		t.Error("should find reminder section")
	}
	if content != "Remember the rules." {
		t.Errorf("content = %q, want %q", content, "Remember the rules.")
	}

	_, ok = pb.Resolve("missing")
	if ok {
		t.Error("should not find missing section")
	}
}

func TestPromptBuilder_ScopeManual_NotInBuild(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{Key: "system", Priority: PrioritySystem, Scope: ScopeAll, Content: "base"})
	pb.Add(Section{Key: "reminder", Priority: PriorityReminder, Scope: ScopeManual, Content: "reminder text"})

	chat := pb.BuildSystemPrompt()
	router := pb.BuildRouterSystemPrompt()

	if strings.Contains(chat, "reminder text") {
		t.Error("ScopeManual section should NOT appear in chat prompt")
	}
	if strings.Contains(router, "reminder text") {
		t.Error("ScopeManual section should NOT appear in router prompt")
	}
}

func TestPromptBuilder_ScopeManual_IncludedInReservedTokens(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{Key: "system", Priority: PrioritySystem, Scope: ScopeAll, Content: "base"})
	tokensWithout := pb.EstimateReservedTokens()

	pb.Add(Section{Key: "reminder", Priority: PriorityReminder, Scope: ScopeManual, Content: "Remember these important rules for your response."})
	tokensWith := pb.EstimateReservedTokens()

	if tokensWith <= tokensWithout {
		t.Errorf("ScopeManual should add to reserved tokens: %d <= %d", tokensWith, tokensWithout)
	}
}

func TestPromptBuilder_SamePriorityStableOrder(t *testing.T) {
	pb := NewPromptBuilder()
	pb.Add(Section{Key: "b", Priority: 10, Scope: ScopeAll, Content: "BBB"})
	pb.Add(Section{Key: "a", Priority: 10, Scope: ScopeAll, Content: "AAA"})

	prompt := pb.BuildSystemPrompt()
	aIdx := strings.Index(prompt, "AAA")
	bIdx := strings.Index(prompt, "BBB")
	// 同じ優先度の場合はキー名のアルファベット順（安定ソート）
	if aIdx > bIdx {
		t.Errorf("same priority should sort by key: a (%d) before b (%d)", aIdx, bIdx)
	}
}
