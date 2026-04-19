package readfile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFile_Execute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello file"), 0644)

	tool := New()
	args, _ := json.Marshal(readFileArgs{Path: path})

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", result.Content)
	}
	if result.Content != "hello file" {
		t.Errorf("Content = %q, want %q", result.Content, "hello file")
	}
	if result.Metadata["path"] != path {
		t.Errorf("Metadata.path = %v, want %q", result.Metadata["path"], path)
	}
	if result.Metadata["bytes"] != 10 {
		t.Errorf("Metadata.bytes = %v, want 10", result.Metadata["bytes"])
	}
}

func TestReadFile_Execute_FileNotFound(t *testing.T) {
	tool := New()
	args, _ := json.Marshal(readFileArgs{Path: "/nonexistent/file.txt"})

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("IsError should be true for missing file")
	}
}

func TestReadFile_Execute_EmptyPath(t *testing.T) {
	tool := New()
	args := json.RawMessage(`{"path":""}`)

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("IsError should be true for empty path")
	}
}

func TestReadFile_Execute_InvalidArgs(t *testing.T) {
	tool := New()
	args := json.RawMessage(`{bad json}`)

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("IsError should be true for invalid args")
	}
}

func TestReadFile_Metadata(t *testing.T) {
	tool := New()

	if tool.Name() != "read_file" {
		t.Errorf("Name = %q", tool.Name())
	}
	if !tool.IsReadOnly() {
		t.Error("should be read-only")
	}
}
