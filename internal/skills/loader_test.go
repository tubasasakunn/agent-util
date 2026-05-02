package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantName    string
		wantDesc    string
		wantBodyHas string
		wantErr     bool
	}{
		{
			name: "minimal valid",
			input: `---
name: pdf-processing
description: Extract PDF text. Use when handling PDFs.
---

## Instructions

Do the thing.`,
			wantName:    "pdf-processing",
			wantDesc:    "Extract PDF text. Use when handling PDFs.",
			wantBodyHas: "## Instructions",
		},
		{
			name: "quoted values",
			input: `---
name: "my-skill"
description: "A skill with quoted description."
---
body`,
			wantName: "my-skill",
			wantDesc: "A skill with quoted description.",
		},
		{
			name: "with metadata sub-fields",
			input: `---
name: data-analysis
description: Analyze datasets.
metadata:
  author: org
  version: "1.0"
---
content`,
			wantName: "data-analysis",
			wantDesc: "Analyze datasets.",
		},
		{
			name:    "no frontmatter",
			input:   "# Just a markdown file\nNo frontmatter here.",
			wantErr: true,
		},
		{
			name: "unclosed frontmatter",
			input: `---
name: broken
description: Missing closing.
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, desc, body, err := parseFrontmatter(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if desc != tt.wantDesc {
				t.Errorf("desc = %q, want %q", desc, tt.wantDesc)
			}
			if tt.wantBodyHas != "" && !strings.Contains(body, tt.wantBodyHas) {
				t.Errorf("body = %q, want to contain %q", body, tt.wantBodyHas)
			}
		})
	}
}

func TestNewInline(t *testing.T) {
	sk := NewInline("test-skill", "A test skill.", "## Instructions\nDo something.")
	if sk.Name != "test-skill" {
		t.Errorf("Name = %q", sk.Name)
	}
	content, err := sk.Activate()
	if err != nil {
		t.Fatalf("Activate() error: %v", err)
	}
	if !strings.Contains(content, "## Instructions") {
		t.Errorf("content = %q", content)
	}
}

func TestNewFileSkill(t *testing.T) {
	dir := t.TempDir()
	location := filepath.Join(dir, "SKILL.md")
	content := `---
name: file-skill
description: A file-based skill.
---
## File Instructions
Content here.`
	if err := os.WriteFile(location, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sk := NewFileSkill("file-skill", "A file-based skill.", location, dir)
	body, err := sk.Activate()
	if err != nil {
		t.Fatalf("Activate() error: %v", err)
	}
	if !strings.Contains(body, "## File Instructions") {
		t.Errorf("body = %q", body)
	}
	if !strings.Contains(body, dir) {
		t.Errorf("body should contain skill dir, got: %q", body)
	}
}

func TestLoader_Load(t *testing.T) {
	dir := t.TempDir()
	mkSkill(t, dir, "my-skill", `---
name: my-skill
description: A test skill. Use for testing.
---
## Instructions
Test instructions here.`)

	if err := os.MkdirAll(filepath.Join(dir, "not-a-skill"), 0755); err != nil {
		t.Fatal(err)
	}

	catalog, err := NewLoader(dir).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if catalog.Len() != 1 {
		t.Errorf("Len() = %d, want 1", catalog.Len())
	}
	sk, ok := catalog.Get("my-skill")
	if !ok {
		t.Fatal("skill 'my-skill' not found")
	}
	if sk.Name != "my-skill" {
		t.Errorf("Name = %q, want 'my-skill'", sk.Name)
	}
	if sk.Activate == nil {
		t.Error("Activate should not be nil")
	}
	body, err := sk.Activate()
	if err != nil {
		t.Fatalf("Activate() error: %v", err)
	}
	if !strings.Contains(body, "Test instructions") {
		t.Errorf("body = %q", body)
	}
}

func TestLoader_NameCollision(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	mkSkill(t, dir1, "shared", `---
name: shared
description: From dir1 (high priority).
---
body1`)
	mkSkill(t, dir2, "shared", `---
name: shared
description: From dir2 (low priority).
---
body2`)

	catalog, err := NewLoader(dir1, dir2).Load()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Len() != 1 {
		t.Errorf("Len() = %d, want 1", catalog.Len())
	}
	sk, _ := catalog.Get("shared")
	if sk.Description != "From dir1 (high priority)." {
		t.Errorf("got lower-priority skill: %q", sk.Description)
	}
}

func TestLoader_NonExistentDir(t *testing.T) {
	catalog, err := NewLoader("/nonexistent/path/skills").Load()
	if err != nil {
		t.Fatalf("Load() should not error on missing dir, got: %v", err)
	}
	if catalog.Len() != 0 {
		t.Errorf("expected empty catalog, got %d", catalog.Len())
	}
}

func TestCatalog_InlineMix(t *testing.T) {
	// ファイルとインラインを混在させてカタログ生成できることを確認
	sk1 := NewInline("inline-skill", "An inline skill.", "inline content")
	sk2 := NewInline("another-skill", "Another skill.", "another content")
	catalog := NewCatalog([]Skill{sk1, sk2})

	if catalog.Len() != 2 {
		t.Errorf("Len() = %d, want 2", catalog.Len())
	}
	s, ok := catalog.Get("inline-skill")
	if !ok {
		t.Fatal("inline-skill not found")
	}
	body, err := s.Activate()
	if err != nil || body != "inline content" {
		t.Errorf("Activate() = %q, %v", body, err)
	}
}

func mkSkill(t *testing.T, base, name, content string) {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
