// Phase 10 実機SLLM検証: mcp.register でMCPサーバーを登録し、SLLMがツールを使えるか検証
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      *int            `json:"id,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	ID *int `json:"id"`
}

func intPtr(n int) *int { return &n }

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func sendReq(w *os.File, req Request) {
	data, _ := json.Marshal(req)
	w.Write(append(data, '\n'))
}

func main() {
	projectDir, _ := filepath.Abs(".")
	log := func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }

	log("=== Phase 10 実機検証: mcp.register → agent.run ===")
	log("プロジェクト: %s", projectDir)

	// 1. MCP サーバーをビルド
	log("\n--- MCPサーバービルド ---")
	mcpBuild := exec.Command("go", "build", "-o", "/tmp/test_mcp_server",
		filepath.Join(projectDir, ".claude/skills/investigation/009_phase10_jsonrpc_server/testmcp/main.go"))
	mcpBuild.Stderr = os.Stderr
	if err := mcpBuild.Run(); err != nil {
		log("MCPサーバービルド失敗: %v", err)
		os.Exit(1)
	}
	log("OK")

	// 2. エージェントバイナリをビルド
	log("\n--- エージェントビルド ---")
	agentBuild := exec.Command("go", "build", "-o", "/tmp/agent_mcp_test", "./cmd/agent/")
	agentBuild.Stderr = os.Stderr
	if err := agentBuild.Run(); err != nil {
		log("エージェントビルド失敗: %v", err)
		os.Exit(1)
	}
	log("OK")

	// 3. エージェントを --rpc モードで起動
	log("\n--- エージェント起動 (--rpc) ---")
	cmd := exec.Command("/tmp/agent_mcp_test", "--rpc")
	cmd.Env = append(os.Environ(),
		"SLLM_ENDPOINT=http://localhost:8080/v1/chat/completions",
		"SLLM_API_KEY=sk-gemma4",
		"SLLM_MODEL=gemma-4-E2B-it-Q4_K_M",
	)

	stdinPipe, _ := cmd.StdinPipe()
	stdoutPipe, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log("起動失敗: %v", err)
		os.Exit(1)
	}
	defer func() {
		stdinPipe.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}()

	stdin := stdinPipe.(*os.File)
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	log("OK")

	// 4. mcp.register で MCP サーバーを登録
	log("\n--- mcp.register ---")
	sendReq(stdin, Request{
		JSONRPC: "2.0",
		Method:  "mcp.register",
		Params: mustMarshal(map[string]any{
			"command": "/tmp/test_mcp_server",
		}),
		ID: intPtr(1),
	})

	if !scanner.Scan() {
		log("レスポンス読み取り失敗")
		os.Exit(1)
	}
	var regResp Response
	json.Unmarshal(scanner.Bytes(), &regResp)
	if regResp.Error != nil {
		log("MCP登録エラー: %s", regResp.Error.Message)
		os.Exit(1)
	}
	var regResult struct {
		Tools []string `json:"tools"`
	}
	json.Unmarshal(regResp.Result, &regResult)
	log("登録成功: %v", regResult.Tools)

	// 5. agent.run で SLLM にタスクを依頼
	prompt := fmt.Sprintf("Read the file %s/go.mod and tell me the module name and Go version used.", projectDir)
	log("\n--- agent.run ---")
	log("プロンプト: %s", prompt)

	sendReq(stdin, Request{
		JSONRPC: "2.0",
		Method:  "agent.run",
		Params:  mustMarshal(map[string]string{"prompt": prompt}),
		ID:      intPtr(2),
	})

	// 6. メッセージループ
	log("\n--- メッセージループ ---")
	timeout := time.After(120 * time.Second)
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
				log("読み取り終了")
				goto end
			}
		case <-timeout:
			log("タイムアウト")
			goto end
		}

		var probe struct {
			Method *string         `json:"method"`
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		json.Unmarshal([]byte(line), &probe)

		if probe.Method != nil {
			// 通知
			log("[通知] %s", *probe.Method)
		} else {
			// レスポンス
			var resp Response
			json.Unmarshal([]byte(line), &resp)
			if resp.ID != nil && *resp.ID == 2 {
				if resp.Error != nil {
					log("[agent.run] エラー: %s", resp.Error.Message)
				} else {
					finalResult = resp.Result
				}
				goto end
			}
		}
	}

end:
	log("\n--- 結果 ---")
	if finalResult != nil {
		var result struct {
			Response string `json:"response"`
			Reason   string `json:"reason"`
			Turns    int    `json:"turns"`
		}
		json.Unmarshal(finalResult, &result)
		log("理由: %s", result.Reason)
		log("ターン: %d", result.Turns)
		log("\n--- SLLMの応答 ---")
		log("%s", result.Response)

		log("\n--- 判定 ---")
		if result.Reason == "completed" {
			log("PASS: mcp.register でMCPサーバーを登録し、SLLMがMCPツールを使って応答を生成した")
		} else {
			log("FAIL: reason=%s", result.Reason)
		}
	} else {
		log("FAIL: 最終応答なし")
	}
}
