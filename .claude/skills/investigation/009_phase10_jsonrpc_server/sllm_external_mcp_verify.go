// Phase 10 実機検証: 外部 MCP サーバー（@modelcontextprotocol/server-filesystem）を使用
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

func intPtr(n int) *int                 { return &n }
func mustMarshal(v any) json.RawMessage { data, _ := json.Marshal(v); return data }

func sendReq(w interface{ Write([]byte) (int, error) }, req Request) {
	data, _ := json.Marshal(req)
	w.Write(append(data, '\n'))
}

func main() {
	projectDir, _ := filepath.Abs(".")
	log := func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }

	log("=== 外部 MCP サーバー検証: @modelcontextprotocol/server-filesystem ===")
	log("プロジェクト: %s", projectDir)

	// エージェントビルド
	log("\n--- エージェントビルド ---")
	if err := exec.Command("go", "build", "-o", "/tmp/agent_ext_mcp", "./cmd/agent/").Run(); err != nil {
		log("ビルド失敗: %v", err)
		os.Exit(1)
	}
	log("OK")

	// エージェント起動
	log("\n--- エージェント起動 ---")
	cmd := exec.Command("/tmp/agent_ext_mcp", "--rpc")
	cmd.Env = append(os.Environ(),
		"SLLM_ENDPOINT=http://localhost:8080/v1/chat/completions",
		"SLLM_API_KEY=sk-gemma4",
	)
	stdinPipe, _ := cmd.StdinPipe()
	stdoutPipe, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log("起動失敗: %v", err)
		os.Exit(1)
	}
	defer func() { stdinPipe.Close(); cmd.Process.Kill(); cmd.Wait() }()

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	log("OK")

	// mcp.register: 外部 MCP サーバー（npx @modelcontextprotocol/server-filesystem）
	log("\n--- mcp.register (npx @modelcontextprotocol/server-filesystem) ---")
	sendReq(stdinPipe, Request{
		JSONRPC: "2.0",
		Method:  "mcp.register",
		Params: mustMarshal(map[string]any{
			"command": "npx",
			"args":    []string{"-y", "@modelcontextprotocol/server-filesystem", projectDir},
		}),
		ID: intPtr(1),
	})

	if !scanner.Scan() {
		log("レスポンスなし")
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
	log("登録ツール: %v", regResult.Tools)

	// agent.run
	prompt := fmt.Sprintf("Use the tools to read the file at %s/CLAUDE.md and tell me the project name and how many ADRs are listed. Be concise.", projectDir)
	log("\n--- agent.run ---")
	log("プロンプト: %s", prompt)

	sendReq(stdinPipe, Request{
		JSONRPC: "2.0",
		Method:  "agent.run",
		Params:  mustMarshal(map[string]string{"prompt": prompt}),
		ID:      intPtr(2),
	})

	// メッセージループ
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
				goto end
			}
		case <-timeout:
			log("タイムアウト")
			goto end
		}

		var probe struct {
			Method *string `json:"method"`
			ID     *int    `json:"id"`
		}
		json.Unmarshal([]byte(line), &probe)

		if probe.Method != nil {
			log("[通知] %s", *probe.Method)
		} else {
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
		log("理由: %s, ターン: %d", result.Reason, result.Turns)
		log("\n--- SLLMの応答 ---")
		log("%s", result.Response)
		log("\n--- 判定 ---")
		if result.Reason == "completed" {
			log("PASS: 外部MCP (server-filesystem via npx) でSLLMがツールを使い応答生成")
		} else {
			log("FAIL: reason=%s", result.Reason)
		}
	} else {
		log("FAIL: 応答なし")
	}
}
