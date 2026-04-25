// Phase 10 実機SLLM検証: JSON-RPCサーバー経由でMCP的なツールを使えるかテスト
//
// 検証シナリオ:
// 1. JSON-RPCサーバーを起動
// 2. MCPの代表的なツール（read_file, list_directory）を tool.register で登録
// 3. agent.run でSLLMにタスクを依頼
// 4. SLLMがルーターでツールを選択 → tool.execute がラッパーに送信される
// 5. ラッパーが実際にファイルシステム操作を実行して結果を返す
// 6. SLLMが結果を活用して最終応答を生成
//
// 使い方:
//   go run ./...claude/skills/investigation/009_phase10_jsonrpc_server/sllm_rpc_test.go

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// JSON-RPC 2.0 型定義（軽量版）
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      *int            `json:"id,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      *int            `json:"id"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func intPtr(n int) *int { return &n }

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func sendRequest(w io.Writer, req Request) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

func sendResponse(w io.Writer, resp Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// handleToolExecute はラッパー側でMCPツールを実際に実行する
func handleToolExecute(params json.RawMessage) (string, bool) {
	var p struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	}
	json.Unmarshal(params, &p)

	switch p.Name {
	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		json.Unmarshal(p.Args, &args)
		fmt.Fprintf(os.Stderr, "  [MCP] read_file: %s\n", args.Path)

		data, err := os.ReadFile(args.Path)
		if err != nil {
			return fmt.Sprintf("Error: %s", err), true
		}
		// 長すぎる場合は切り詰め
		content := string(data)
		if len(content) > 2000 {
			content = content[:2000] + "\n... (truncated)"
		}
		return content, false

	case "list_directory":
		var args struct {
			Path string `json:"path"`
		}
		json.Unmarshal(p.Args, &args)
		fmt.Fprintf(os.Stderr, "  [MCP] list_directory: %s\n", args.Path)

		entries, err := os.ReadDir(args.Path)
		if err != nil {
			return fmt.Sprintf("Error: %s", err), true
		}
		var sb strings.Builder
		for _, e := range entries {
			if e.IsDir() {
				sb.WriteString("[DIR]  " + e.Name() + "\n")
			} else {
				sb.WriteString("[FILE] " + e.Name() + "\n")
			}
		}
		return sb.String(), false

	default:
		return fmt.Sprintf("Unknown tool: %s", p.Name), true
	}
}

func main() {
	projectDir, _ := filepath.Abs(".")
	fmt.Fprintf(os.Stderr, "=== Phase 10 実機SLLM検証: MCP ツール統合 ===\n")
	fmt.Fprintf(os.Stderr, "プロジェクトディレクトリ: %s\n\n", projectDir)

	// Goバイナリをビルド
	fmt.Fprintf(os.Stderr, "--- バイナリビルド ---\n")
	buildCmd := exec.Command("go", "build", "-o", "/tmp/agent_sllm_test", "./cmd/agent/")
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ビルド失敗: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "ビルド成功\n\n")

	// JSON-RPCサーバーを起動
	fmt.Fprintf(os.Stderr, "--- サーバー起動 ---\n")
	cmd := exec.Command("/tmp/agent_sllm_test", "--rpc")
	cmd.Env = append(os.Environ(),
		"SLLM_ENDPOINT=http://localhost:8080/v1/chat/completions",
		"SLLM_API_KEY=sk-gemma4",
		"SLLM_MODEL=gemma-4-E2B-it-Q4_K_M",
	)

	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "起動失敗: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	fmt.Fprintf(os.Stderr, "サーバー起動完了\n\n")

	// テスト1: MCPツールの登録
	fmt.Fprintf(os.Stderr, "--- テスト1: MCPツール登録 (read_file, list_directory) ---\n")
	sendRequest(stdin, Request{
		JSONRPC: "2.0",
		Method:  "tool.register",
		Params: mustMarshal(map[string]any{
			"tools": []map[string]any{
				{
					"name":        "read_file",
					"description": "Read the contents of a file at the given path. Returns the file content as text.",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"path": map[string]any{
								"type":        "string",
								"description": "Absolute path to the file to read",
							},
						},
						"required": []string{"path"},
					},
					"read_only": true,
				},
				{
					"name":        "list_directory",
					"description": "List files and directories in the given directory path. Shows [DIR] for directories and [FILE] for files.",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"path": map[string]any{
								"type":        "string",
								"description": "Absolute path to the directory to list",
							},
						},
						"required": []string{"path"},
					},
					"read_only": true,
				},
			},
		}),
		ID: intPtr(1),
	})

	if !scanner.Scan() {
		fmt.Fprintf(os.Stderr, "レスポンス読み取り失敗\n")
		os.Exit(1)
	}
	var regResp Response
	json.Unmarshal(scanner.Bytes(), &regResp)
	if regResp.Error != nil {
		fmt.Fprintf(os.Stderr, "登録エラー: %s\n", regResp.Error.Message)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "ツール登録成功: %s\n\n", string(regResp.Result))

	// テスト2: SLLMにタスクを依頼（MCPツールの使用を期待）
	prompt := fmt.Sprintf("List the files in the directory %s/cmd/agent/ and then read the file main.go in that directory. Tell me what the main function does in 2-3 sentences.", projectDir)
	fmt.Fprintf(os.Stderr, "--- テスト2: agent.run (SLLMにMCPツール使用を依頼) ---\n")
	fmt.Fprintf(os.Stderr, "プロンプト: %s\n\n", prompt)

	sendRequest(stdin, Request{
		JSONRPC: "2.0",
		Method:  "agent.run",
		Params:  mustMarshal(map[string]string{"prompt": prompt}),
		ID:      intPtr(2),
	})

	// メッセージループ: tool.execute リクエストに応答しつつ、agent.run の完了を待つ
	fmt.Fprintf(os.Stderr, "--- メッセージループ開始 ---\n")
	timeout := time.After(120 * time.Second)
	toolExecuteCount := 0
	var finalResult json.RawMessage

	for {
		done := make(chan bool, 1)
		var line string
		go func() {
			if scanner.Scan() {
				line = scanner.Text()
				done <- true
			} else {
				done <- false
			}
		}()

		select {
		case ok := <-done:
			if !ok {
				fmt.Fprintf(os.Stderr, "読み取り終了\n")
				goto end
			}
		case <-timeout:
			fmt.Fprintf(os.Stderr, "タイムアウト（120秒）\n")
			goto end
		}

		// メッセージの種類を判別
		var probe struct {
			Method *string         `json:"method"`
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *RPCError       `json:"error"`
		}
		json.Unmarshal([]byte(line), &probe)

		if probe.Method != nil {
			// リクエストまたは通知
			var req Request
			json.Unmarshal([]byte(line), &req)

			if req.Method == "tool.execute" {
				toolExecuteCount++
				fmt.Fprintf(os.Stderr, "\n[tool.execute #%d] 受信\n", toolExecuteCount)

				content, isErr := handleToolExecute(req.Params)
				preview := content
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				fmt.Fprintf(os.Stderr, "  結果 (isError=%v): %s\n", isErr, preview)

				// レスポンスを返す
				sendResponse(stdin, Response{
					JSONRPC: "2.0",
					Result: mustMarshal(map[string]any{
						"content":  content,
						"is_error": isErr,
					}),
					ID: req.ID,
				})
			} else {
				// 通知（stream.end 等）
				fmt.Fprintf(os.Stderr, "[通知] %s: %s\n", req.Method, string(req.Params))
			}
		} else {
			// レスポンス
			var resp Response
			json.Unmarshal([]byte(line), &resp)

			if resp.ID != nil && *resp.ID == 2 {
				// agent.run の最終レスポンス
				if resp.Error != nil {
					fmt.Fprintf(os.Stderr, "\n[agent.run] エラー: %s\n", resp.Error.Message)
				} else {
					finalResult = resp.Result
				}
				goto end
			}
		}
	}

end:
	fmt.Fprintf(os.Stderr, "\n--- 結果 ---\n")
	fmt.Fprintf(os.Stderr, "tool.execute 呼び出し回数: %d\n", toolExecuteCount)

	if finalResult != nil {
		var result struct {
			Response string `json:"response"`
			Reason   string `json:"reason"`
			Turns    int    `json:"turns"`
		}
		json.Unmarshal(finalResult, &result)
		fmt.Fprintf(os.Stderr, "終了理由: %s\n", result.Reason)
		fmt.Fprintf(os.Stderr, "ターン数: %d\n", result.Turns)
		fmt.Fprintf(os.Stderr, "\n--- SLLMの応答 ---\n")
		fmt.Fprintf(os.Stderr, "%s\n", result.Response)

		// 判定
		fmt.Fprintf(os.Stderr, "\n--- 判定 ---\n")
		if toolExecuteCount > 0 && result.Reason == "completed" {
			fmt.Fprintf(os.Stderr, "PASS: SLLMがMCPツールを選択・実行し、結果を活用して応答を生成した\n")
		} else if toolExecuteCount == 0 {
			fmt.Fprintf(os.Stderr, "PARTIAL: SLLMがツールを選択しなかった（ルーターの判断）\n")
		} else {
			fmt.Fprintf(os.Stderr, "FAIL: 予期しない結果 (reason=%s, tools=%d)\n", result.Reason, toolExecuteCount)
		}
	} else {
		fmt.Fprintf(os.Stderr, "FAIL: 最終応答を取得できなかった\n")
	}
}
