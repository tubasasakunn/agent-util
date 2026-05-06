package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"ai-agent/pkg/tool"
)

func TestAsTool_Interface(t *testing.T) {
	sk := NewInline("my-skill", "Does something useful.", "## Instructions\nDo it.")
	tl := AsTool(sk)

	if tl.Name() != "my-skill" {
		t.Errorf("Name() = %q, want 'my-skill'", tl.Name())
	}
	if tl.Description() != "Does something useful." {
		t.Errorf("Description() = %q", tl.Description())
	}
	if !tl.IsReadOnly() {
		t.Error("IsReadOnly() should be true")
	}

	// Parameters は空オブジェクト
	var params map[string]any
	if err := json.Unmarshal(tl.Parameters(), &params); err != nil {
		t.Fatalf("Parameters() invalid JSON: %v", err)
	}
}

func TestAsTool_Execute(t *testing.T) {
	sk := NewInline("my-skill", "A skill.", "## Instructions\nRespond with hello.")
	tl := AsTool(sk)

	result, err := tl.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() IsError=true: %s", result.Content)
	}
	if !strings.Contains(result.Content, "my-skill") {
		t.Errorf("content should contain skill name, got: %q", result.Content)
	}
	if !strings.Contains(result.Content, "## Instructions") {
		t.Errorf("content should contain skill body, got: %q", result.Content)
	}
	if !strings.Contains(result.Content, `tool="none"`) {
		t.Errorf("content should contain none hint, got: %q", result.Content)
	}
}

func TestAsTool_ActivateError(t *testing.T) {
	sk := New("broken-skill", "A broken skill.", func() (string, error) {
		return "", fmt.Errorf("activation failed")
	})
	tl := AsTool(sk)

	result, err := tl.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() should not return error, got: %v", err)
	}
	if !result.IsError {
		t.Error("Execute() IsError should be true on activation failure")
	}
}

func TestCatalogAsTools(t *testing.T) {
	catalog := NewCatalog([]Skill{
		NewInline("skill-a", "Skill A.", "content A"),
		NewInline("skill-b", "Skill B.", "content B"),
	})

	tools := CatalogAsTools(catalog)
	if len(tools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(tools))
	}

	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name()] = true
		// tool.Tool インターフェースを実装していることを確認
		var _ tool.Tool = tl
	}
	if !names["skill-a"] || !names["skill-b"] {
		t.Errorf("missing expected tools, got: %v", names)
	}
}
