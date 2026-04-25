// test4b_verifier_fail.go
// Verifier が検証失敗し、LLMが修正を試みるフローを検証する。
// echo ツールで小文字テキストを出力させ、uppercase_check が失敗するケースを作る。
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

type echoTool struct{}

func (t *echoTool) Name() string             { return "echo" }
func (t *echoTool) Description() string       { return "Echoes the input text back exactly as provided" }
func (t *echoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"Text to echo back"}},"required":["text"]}`)
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

type uppercaseVerifier struct {
	calls int
}

func (v *uppercaseVerifier) Name() string { return "uppercase_check" }

func (v *uppercaseVerifier) Verify(_ context.Context, _ string, _ []byte, result string) (*engine.VerifyResult, error) {
	v.calls++
	if strings.ToUpper(result) != result {
		return &engine.VerifyResult{
			Passed:  false,
			Summary: fmt.Sprintf("Output must be ALL UPPERCASE. Got: %q. Use uppercase text with the echo tool.", result),
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

	fmt.Fprintln(os.Stderr, "=== Test 4b: Verifier Fail → Self-Correct ===")
	fmt.Fprintln(os.Stderr, "Asking to echo lowercase text. Verifier should fail, LLM should retry with uppercase.")
	fmt.Fprintln(os.Stderr, "")

	// 小文字テキストを echo させる → Verifier 失敗 → LLM が大文字に修正して再試行を期待
	result, err := eng.Run(context.Background(),
		"Use the echo tool to say 'hello world'. Important: output must pass the uppercase verification check.")
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[ERROR] %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\n[RESULT] reason=%s, turns=%d, verifier_calls=%d\n", result.Reason, result.Turns, verifier.calls)
	fmt.Println(result.Response)
}
