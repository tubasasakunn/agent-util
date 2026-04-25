// test3_consecutive_failures.go
// 常に失敗するツールのみを登録し、連続失敗キャップが機能するかを検証する。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"ai-agent/internal/engine"
	"ai-agent/internal/llm"
	"ai-agent/pkg/tool"
)

// brokenTool は常にエラーを返すツール。
type brokenTool struct{}

func (t *brokenTool) Name() string             { return "broken_tool" }
func (t *brokenTool) Description() string       { return "A tool that always fails (for testing)" }
func (t *brokenTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`)
}
func (t *brokenTool) IsReadOnly() bool          { return true }
func (t *brokenTool) IsConcurrencySafe() bool   { return false }
func (t *brokenTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Result{}, errors.New("this tool always fails")
}

func main() {
	endpoint := os.Getenv("SLLM_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8080/v1/chat/completions"
	}
	apiKey := os.Getenv("SLLM_API_KEY")
	if apiKey == "" {
		apiKey = "sk-gemma4"
	}

	client := llm.NewClient(
		llm.WithEndpoint(endpoint),
		llm.WithAPIKey(apiKey),
		llm.WithLogWriter(os.Stderr),
	)

	eng := engine.New(client,
		engine.WithTools(&brokenTool{}),
		engine.WithMaxTurns(10),
		engine.WithMaxConsecutiveFailures(3),
		engine.WithLogWriter(os.Stderr),
	)

	fmt.Fprintln(os.Stderr, "=== Test 3: Consecutive Failure Cap ===")
	fmt.Fprintln(os.Stderr, "Only broken_tool is available. Expecting safe stop after 3 consecutive failures.")
	fmt.Fprintln(os.Stderr, "")

	result, err := eng.Run(context.Background(), "Use the broken_tool with input 'test'. Keep trying.")
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[ERROR] %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\n[RESULT] reason=%s, turns=%d\n", result.Reason, result.Turns)
	fmt.Println(result.Response)

	if result.Reason == "max_consecutive_failures" {
		fmt.Fprintln(os.Stderr, "\n[PASS] Consecutive failure cap triggered correctly.")
	} else {
		fmt.Fprintln(os.Stderr, "\n[UNEXPECTED] Expected max_consecutive_failures, got: "+result.Reason)
	}
}
