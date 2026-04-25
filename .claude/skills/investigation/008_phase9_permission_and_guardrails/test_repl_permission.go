// +build ignore

// Phase 9 実機検証: REPLモードでの非ReadOnlyツールのパーミッション確認フロー
//
// 実行方法:
//   SLLM_ENDPOINT="http://localhost:8080/v1/chat/completions" SLLM_API_KEY="sk-gemma4" \
//     go run .claude/skills/investigation/008_phase9_permission_and_guardrails/test_repl_permission.go
//
// echoツールをIsReadOnly=falseで登録し、SLLMが選択した場合にユーザー確認プロンプトが表示されることを検証する。

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"ai-agent/internal/engine"
	"ai-agent/internal/llm"
	"ai-agent/internal/tools/readfile"
	"ai-agent/pkg/tool"
)

// writeEcho は IsReadOnly=false の echoツール（テスト用）。
type writeEcho struct{}

func (t *writeEcho) Name() string            { return "write_echo" }
func (t *writeEcho) Description() string      { return "Echoes a message (simulates a write operation for testing permissions)" }
func (t *writeEcho) IsReadOnly() bool         { return false }
func (t *writeEcho) IsConcurrencySafe() bool  { return true }
func (t *writeEcho) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"message": {"type": "string", "description": "The message to echo"}
		},
		"required": ["message"]
	}`)
}
func (t *writeEcho) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var a struct{ Message string `json:"message"` }
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Result{Content: "invalid arguments", IsError: true}, nil
	}
	return tool.Result{Content: "ECHOED: " + a.Message}, nil
}

// stdinApprover はstdinでユーザー確認を行う。
// REPLと同じ *bufio.Reader を共有して使用する。
type stdinApprover struct {
	reader *bufio.Reader
}

func (a *stdinApprover) Approve(_ context.Context, toolName string, args json.RawMessage) (bool, error) {
	argsStr := string(args)
	if len(argsStr) > 200 {
		argsStr = argsStr[:200] + "..."
	}
	fmt.Fprintf(os.Stderr, "\n[permission] Tool %q を実行しますか？\n  引数: %s\n  [y/N]: ", toolName, argsStr)
	line, err := a.reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes", nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

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
		llm.WithModel("gemma-4-E2B-it-Q4_K_M"),
		llm.WithAPIKey(apiKey),
		llm.WithLogWriter(os.Stderr),
	)

	// stdinの *bufio.Reader をREPLとApproverで共有
	stdinReader := bufio.NewReader(os.Stdin)

	eng := engine.New(client,
		engine.WithMaxTurns(10),
		engine.WithTokenLimit(8192),
		engine.WithTools(
			readfile.New(),
			&writeEcho{},
		),
		engine.WithLogWriter(os.Stderr),
		engine.WithPermissionPolicy(engine.PermissionPolicy{}),
		engine.WithUserApprover(&stdinApprover{reader: stdinReader}),
	)

	fmt.Fprintln(os.Stderr, "=== Phase 9 Permission Test (REPL) ===")
	fmt.Fprintln(os.Stderr, "Tools: read_file (ReadOnly), write_echo (non-ReadOnly)")
	fmt.Fprintln(os.Stderr, "Expected: read_file → auto-approve, write_echo → user confirmation [y/N]")
	fmt.Fprintln(os.Stderr, "Type 'exit' to quit.")
	fmt.Fprintln(os.Stderr, "")

	for {
		fmt.Fprint(os.Stderr, "> ")
		rawLine, err := stdinReader.ReadString('\n')
		if err != nil {
			break
		}
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			break
		}

		reqCtx, reqCancel := context.WithCancel(ctx)
		result, err := eng.Run(reqCtx, line)
		reqCancel()

		if err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "\n(interrupted)")
				return
			}
			var tw *engine.TripwireError
			if errors.As(err, &tw) {
				fmt.Fprintf(os.Stderr, "\n[TRIPWIRE] %s: %s\n", tw.Source, tw.Reason)
				return
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		fmt.Println(result.Response)
		fmt.Println()
	}
}
