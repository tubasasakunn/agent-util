package rpc

import (
	"testing"

	"ai-agent/internal/engine"
	"ai-agent/internal/llm"
)

// mustEngineNew は engine.New のテスト用ラッパー。エラー時は t.Fatal する。
func mustEngineNew(t *testing.T, completer llm.Completer, opts ...engine.Option) *engine.Engine {
	t.Helper()
	eng, err := engine.New(completer, opts...)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	return eng
}
