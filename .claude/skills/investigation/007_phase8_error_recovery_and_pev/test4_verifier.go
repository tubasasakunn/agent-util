// test4_verifier.go
// Verifier が検証失敗メッセージを履歴に追加し、LLMが修正を試みることを検証する。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"ai-agent/internal/engine"
	"ai-agent/internal/llm"
	"ai-agent/pkg/tool"
)

// echoTool は入力をそのまま返すツール。
type echoTool struct{}

func (t *echoTool) Name() string             { return "echo" }
func (t *echoTool) Description() string       { return "Echoes the input text back" }
func (t *echoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"Text to echo"}},"required":["text"]}`)
}
func (t *echoTool) IsReadOnly() bool          { return true }
func (t *echoTool) IsConcurrencySafe() bool   { return true }
func (t *echoTool) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var params struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tool.Result{}, fmt.Errorf("parse args: %w", err)
	}
	return tool.Result{Content: params.Text}, nil
}

// uppercaseVerifier は出力が全て大文字であることを検証するVerifier。
// 1回目は失敗（小文字を含む場合）、LLMに大文字に修正するよう促す。
type uppercaseVerifier struct {
	calls int
}

func (v *uppercaseVerifier) Name() string { return "uppercase_check" }

func (v *uppercaseVerifier) Verify(_ context.Context, _ string, _ []byte, result string) (*engine.VerifyResult, error) {
	v.calls++
	if strings.ToUpper(result) != result {
		return &engine.VerifyResult{
			Passed:  false,
			Summary: fmt.Sprintf("Output must be ALL UPPERCASE. Got: %q", result),
			Details: []string{"lowercase characters detected"},
		}, nil
	}
	return &engine.VerifyResult{
		Passed:  true,
		Summary: "output is all uppercase",
	}, nil
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

	verifier := &uppercaseVerifier{}

	eng := engine.New(client,
		engine.WithTools(&echoTool{}),
		engine.WithMaxTurns(10),
		engine.WithMaxConsecutiveFailures(5),
		engine.WithVerifiers(verifier),
		engine.WithLogWriter(os.Stderr),
	)

	fmt.Fprintln(os.Stderr, "=== Test 4: Verifier Integration ===")
	fmt.Fprintln(os.Stderr, "uppercase_check verifier will reject lowercase output.")
	fmt.Fprintln(os.Stderr, "LLM should self-correct by using UPPERCASE text.")
	fmt.Fprintln(os.Stderr, "")

	result, err := eng.Run(context.Background(), "Use the echo tool to say 'HELLO WORLD'. The text must be ALL UPPERCASE.")
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[ERROR] %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\n[RESULT] reason=%s, turns=%d, verifier_calls=%d\n", result.Reason, result.Turns, verifier.calls)
	fmt.Println(result.Response)
}
