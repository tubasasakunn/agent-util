package tool

import (
	"context"
	"encoding/json"
	"testing"
)

// stubTool はテスト用のTool実装。
type stubTool struct {
	name            string
	description     string
	parameters      json.RawMessage
	readOnly        bool
	concurrencySafe bool
}

func (s *stubTool) Name() string                { return s.name }
func (s *stubTool) Description() string          { return s.description }
func (s *stubTool) Parameters() json.RawMessage  { return s.parameters }
func (s *stubTool) IsReadOnly() bool             { return s.readOnly }
func (s *stubTool) IsConcurrencySafe() bool      { return s.concurrencySafe }
func (s *stubTool) Execute(_ context.Context, _ json.RawMessage) (Result, error) {
	return Result{Content: "stub"}, nil
}

func TestDefinitionOf(t *testing.T) {
	params := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)

	tests := []struct {
		name string
		tool Tool
		want Definition
	}{
		{
			name: "read-only tool",
			tool: &stubTool{
				name:        "read_file",
				description: "Reads a file",
				parameters:  params,
				readOnly:    true,
			},
			want: Definition{
				Name:        "read_file",
				Description: "Reads a file",
				Parameters:  params,
				ReadOnly:    true,
			},
		},
		{
			name: "writable tool",
			tool: &stubTool{
				name:        "write_file",
				description: "Writes a file",
				parameters:  params,
				readOnly:    false,
			},
			want: Definition{
				Name:        "write_file",
				Description: "Writes a file",
				Parameters:  params,
				ReadOnly:    false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefinitionOf(tt.tool)
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.Description != tt.want.Description {
				t.Errorf("Description = %q, want %q", got.Description, tt.want.Description)
			}
			if string(got.Parameters) != string(tt.want.Parameters) {
				t.Errorf("Parameters = %s, want %s", got.Parameters, tt.want.Parameters)
			}
			if got.ReadOnly != tt.want.ReadOnly {
				t.Errorf("ReadOnly = %v, want %v", got.ReadOnly, tt.want.ReadOnly)
			}
		})
	}
}

func TestWorkDirContext_RoundTrip(t *testing.T) {
	ctx := context.Background()

	// 未設定時は空文字
	if got := WorkDirFromContext(ctx); got != "" {
		t.Errorf("WorkDirFromContext(empty) = %q, want empty", got)
	}

	// 設定 → 取得
	ctx = ContextWithWorkDir(ctx, "/tmp/worktree-1")
	if got := WorkDirFromContext(ctx); got != "/tmp/worktree-1" {
		t.Errorf("WorkDirFromContext = %q, want %q", got, "/tmp/worktree-1")
	}

	// 上書き
	ctx = ContextWithWorkDir(ctx, "/tmp/worktree-2")
	if got := WorkDirFromContext(ctx); got != "/tmp/worktree-2" {
		t.Errorf("WorkDirFromContext(overwrite) = %q, want %q", got, "/tmp/worktree-2")
	}
}

func TestResult_JSON(t *testing.T) {
	tests := []struct {
		name string
		r    Result
		want string
	}{
		{
			name: "success result",
			r:    Result{Content: "hello"},
			want: `{"content":"hello"}`,
		},
		{
			name: "error result",
			r:    Result{Content: "not found", IsError: true},
			want: `{"content":"not found","is_error":true}`,
		},
		{
			name: "result with metadata",
			r: Result{
				Content:  "data",
				Metadata: map[string]any{"bytes": 42},
			},
			want: `{"content":"data","metadata":{"bytes":42}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.r)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			if string(b) != tt.want {
				t.Errorf("JSON = %s, want %s", b, tt.want)
			}
		})
	}
}
