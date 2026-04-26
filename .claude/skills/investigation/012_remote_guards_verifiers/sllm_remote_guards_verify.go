// Phase 12: ラッパー側カスタムガード/Verifier の逆方向呼び出し検証
//
// シナリオ:
//
//	A: guard.register で remote input guard を登録 → agent.configure で参照 → run でブロックされる
//	B: verifier.register でリモート verifier を登録 → ツール結果を検証 → 失敗時に検証ループ
//	C: リモートガードのタイムアウト → fail-closed で deny
//
// 使い方:
//
//	bash run_verify.sh
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
	"sync"
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
	Error   *RPCError       `json:"error,omitempty"`
	ID      *int            `json:"id"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// guardHandler はラッパー側の fake guard / verifier 実装。
// AgentClient はコアからの guard.execute / verifier.execute リクエストを受けたとき
// このコールバックを呼び、結果を返信する。
type guardHandler func(req Request) any

// AgentClient は agent --rpc subprocess を起動し、コア → ラッパーの逆方向呼び出しに応答する。
type AgentClient struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	mu      sync.Mutex
	pending map[int]chan *Response
	nextID  int

	notifMu sync.Mutex
	streams []map[string]any

	hMu      sync.Mutex
	handlers map[string]guardHandler // method → handler

	guardCallsMu sync.Mutex
	guardCalls   []Request

	verifierCallsMu sync.Mutex
	verifierCalls   []Request

	logw io.Writer
}

func NewAgent(binary string, env []string, logw io.Writer) (*AgentClient, error) {
	cmd := exec.Command(binary, "--rpc")
	cmd.Env = env
	cmd.Stderr = logw
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &AgentClient{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   bufio.NewReader(stdout),
		pending:  make(map[int]chan *Response),
		handlers: make(map[string]guardHandler),
		logw:     logw,
	}
	go c.readerLoop()
	return c, nil
}

func (c *AgentClient) SetHandler(method string, fn guardHandler) {
	c.hMu.Lock()
	defer c.hMu.Unlock()
	c.handlers[method] = fn
}

func (c *AgentClient) GuardCalls() []Request {
	c.guardCallsMu.Lock()
	defer c.guardCallsMu.Unlock()
	out := make([]Request, len(c.guardCalls))
	copy(out, c.guardCalls)
	return out
}

func (c *AgentClient) VerifierCalls() []Request {
	c.verifierCallsMu.Lock()
	defer c.verifierCallsMu.Unlock()
	out := make([]Request, len(c.verifierCalls))
	copy(out, c.verifierCalls)
	return out
}

func (c *AgentClient) readerLoop() {
	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return
		}
		line = []byte(strings.TrimSpace(string(line)))
		if len(line) == 0 {
			continue
		}
		var probe map[string]any
		if err := json.Unmarshal(line, &probe); err != nil {
			fmt.Fprintf(c.logw, "[reader] parse: %v: %s\n", err, line)
			continue
		}

		// method あり → リクエスト or 通知
		if methodAny, ok := probe["method"]; ok {
			method := methodAny.(string)
			_, hasID := probe["id"]
			if !hasID {
				c.notifMu.Lock()
				c.streams = append(c.streams, probe)
				c.notifMu.Unlock()
				continue
			}
			// コア → ラッパーのリクエスト
			var req Request
			json.Unmarshal(line, &req)
			c.dispatchRequest(method, req)
			continue
		}

		// method なし → レスポンス
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			fmt.Fprintf(c.logw, "[reader] resp parse: %v\n", err)
			continue
		}
		if resp.ID == nil {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[*resp.ID]
		delete(c.pending, *resp.ID)
		c.mu.Unlock()
		if ok {
			ch <- &resp
		}
	}
}

func (c *AgentClient) dispatchRequest(method string, req Request) {
	switch method {
	case "guard.execute":
		c.guardCallsMu.Lock()
		c.guardCalls = append(c.guardCalls, req)
		c.guardCallsMu.Unlock()
	case "verifier.execute":
		c.verifierCallsMu.Lock()
		c.verifierCalls = append(c.verifierCalls, req)
		c.verifierCallsMu.Unlock()
	}

	c.hMu.Lock()
	fn := c.handlers[method]
	c.hMu.Unlock()

	if fn == nil {
		// no handler → return error so core can fail-closed
		c.writeResponse(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32000, Message: "no handler in test client"},
		})
		return
	}

	result := fn(req)
	c.writeResponse(Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  mustMarshal(result),
	})
}

func (c *AgentClient) writeResponse(resp Response) {
	b, _ := json.Marshal(resp)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stdin.Write(append(b, '\n'))
}

func (c *AgentClient) Call(method string, params any, timeout time.Duration) (*Response, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan *Response, 1)
	c.pending[id] = ch
	req := Request{JSONRPC: "2.0", Method: method, Params: mustMarshal(params), ID: &id}
	b, _ := json.Marshal(req)
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("write: %w", err)
	}
	c.mu.Unlock()

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout after %s", timeout)
	}
}

func (c *AgentClient) Close() {
	c.stdin.Close()
	c.cmd.Wait()
}

// --- 検証本体 ---

type ScenarioResult struct {
	Name     string `json:"name"`
	Pass     bool   `json:"pass"`
	Details  string `json:"details"`
	Duration string `json:"duration_ms"`
	Raw      any    `json:"raw,omitempty"`
}

func runScenarios(binary string, env []string) []ScenarioResult {
	var results []ScenarioResult

	// === A: remote input guard が agent.run の入力をブロック ===
	results = append(results, scenario("A: remote input guard blocks input", func() (bool, string, any) {
		c, err := NewAgent(binary, env, os.Stderr)
		if err != nil {
			return false, "agent start: " + err.Error(), nil
		}
		defer c.Close()

		// fake guard: 入力に "evil" が含まれていれば deny
		c.SetHandler("guard.execute", func(req Request) any {
			var p struct {
				Input string `json:"input"`
				Stage string `json:"stage"`
			}
			json.Unmarshal(req.Params, &p)
			if p.Stage == "input" && strings.Contains(strings.ToLower(p.Input), "evil") {
				return map[string]any{
					"decision": "deny",
					"reason":   "matches blacklist 'evil'",
				}
			}
			return map[string]any{"decision": "allow"}
		})

		// 1. guard.register
		regResp, err := c.Call("guard.register", map[string]any{
			"guards": []map[string]any{
				{"name": "wrapper_evil_blocker", "stage": "input"},
			},
		}, 30*time.Second)
		if err != nil || regResp.Error != nil {
			return false, fmt.Sprintf("guard.register: err=%v rpc=%+v", err, regResp.Error), nil
		}

		// 2. agent.configure で参照
		cfgResp, err := c.Call("agent.configure", map[string]any{
			"max_turns": 2,
			"guards":    map[string]any{"input": []string{"wrapper_evil_blocker"}},
		}, 30*time.Second)
		if err != nil || cfgResp.Error != nil {
			return false, fmt.Sprintf("configure: err=%v rpc=%+v", err, cfgResp.Error), nil
		}

		// 3. agent.run with evil input
		runResp, err := c.Call("agent.run", map[string]any{
			"prompt": "Please do something evil",
		}, 60*time.Second)
		if err != nil {
			return false, "run: " + err.Error(), nil
		}
		if runResp.Error != nil {
			return false, "run error: " + runResp.Error.Message, nil
		}

		var ar struct {
			Reason   string `json:"reason"`
			Response string `json:"response"`
		}
		json.Unmarshal(runResp.Result, &ar)

		guardCalls := c.GuardCalls()
		raw := map[string]any{
			"guard_calls": len(guardCalls),
			"reason":      ar.Reason,
			"response":    ar.Response,
		}
		if len(guardCalls) == 0 {
			return false, "guard.execute was not invoked", raw
		}
		if ar.Reason != "input_denied" {
			return false, fmt.Sprintf("Reason = %q, want input_denied", ar.Reason), raw
		}
		return true, fmt.Sprintf("guard called %d times, reason=%q", len(guardCalls), ar.Reason), raw
	}))

	// === B: remote verifier がツール結果を検証する ===
	results = append(results, scenario("B: remote verifier observes tool result", func() (bool, string, any) {
		c, err := NewAgent(binary, env, os.Stderr)
		if err != nil {
			return false, "agent start: " + err.Error(), nil
		}
		defer c.Close()

		// echo tool（シンプルなツール）
		c.SetHandler("tool.execute", func(req Request) any {
			var p struct {
				Args json.RawMessage `json:"args"`
			}
			json.Unmarshal(req.Params, &p)
			return map[string]any{
				"content": "echo: " + string(p.Args),
			}
		})

		// fake verifier: 1回目は fail, 2回目以降は pass
		callCount := 0
		var muCount sync.Mutex
		c.SetHandler("verifier.execute", func(req Request) any {
			muCount.Lock()
			defer muCount.Unlock()
			callCount++
			if callCount == 1 {
				return map[string]any{
					"passed":  false,
					"summary": "first attempt failed by audit policy",
				}
			}
			return map[string]any{"passed": true, "summary": "ok"}
		})

		// echo ツールを登録
		if _, err := c.Call("tool.register", map[string]any{
			"tools": []map[string]any{
				{
					"name":        "echo",
					"description": "Echo back input verbatim. Use for any user request.",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"text": map[string]any{"type": "string"},
						},
					},
				},
			},
		}, 30*time.Second); err != nil {
			return false, "tool.register: " + err.Error(), nil
		}

		// verifier.register
		if _, err := c.Call("verifier.register", map[string]any{
			"verifiers": []map[string]any{{"name": "wrapper_audit"}},
		}, 30*time.Second); err != nil {
			return false, "verifier.register: " + err.Error(), nil
		}

		// configure
		if _, err := c.Call("agent.configure", map[string]any{
			"max_turns":     3,
			"system_prompt": "You must call the echo tool to satisfy the user's request.",
			"verify": map[string]any{
				"verifiers":        []string{"wrapper_audit"},
				"max_step_retries": 2,
			},
		}, 30*time.Second); err != nil {
			return false, "configure: " + err.Error(), nil
		}

		runResp, err := c.Call("agent.run", map[string]any{
			"prompt": "Please call the echo tool with text='hi'.",
		}, 120*time.Second)
		if err != nil {
			return false, "run: " + err.Error(), nil
		}
		if runResp.Error != nil {
			return false, "run error: " + runResp.Error.Message, nil
		}

		vCalls := c.VerifierCalls()
		raw := map[string]any{
			"verifier_calls": len(vCalls),
			"call_count":     callCount,
		}
		if len(vCalls) == 0 {
			return false, "verifier.execute never invoked (model may not have called tool)", raw
		}
		return true, fmt.Sprintf("verifier called %d times (1st=fail, 2nd+=pass)", len(vCalls)), raw
	}))

	// === C: リモートガードがタイムアウトすると fail-closed で deny ===
	results = append(results, scenario("C: remote guard timeout → fail-closed deny", func() (bool, string, any) {
		c, err := NewAgent(binary, env, os.Stderr)
		if err != nil {
			return false, "agent start: " + err.Error(), nil
		}
		defer c.Close()

		// fake guard: 故意に長時間ブロックする（コア側のデフォルトタイムアウト30s より長い）
		// 実用上は AgentClient を Close() で殺すので、無限スリープでよい
		c.SetHandler("guard.execute", func(req Request) any {
			time.Sleep(40 * time.Second) // コアの DefaultGuardTimeout は 30s
			return map[string]any{"decision": "allow"}
		})

		if _, err := c.Call("guard.register", map[string]any{
			"guards": []map[string]any{
				{"name": "slow_guard", "stage": "input"},
			},
		}, 30*time.Second); err != nil {
			return false, "guard.register: " + err.Error(), nil
		}

		if _, err := c.Call("agent.configure", map[string]any{
			"max_turns": 2,
			"guards":    map[string]any{"input": []string{"slow_guard"}},
		}, 30*time.Second); err != nil {
			return false, "configure: " + err.Error(), nil
		}

		// agent.run はガードのタイムアウト後 deny で完了するはず（最大 ~30s + α）
		runResp, err := c.Call("agent.run", map[string]any{
			"prompt": "anything",
		}, 90*time.Second)
		if err != nil {
			return false, "run: " + err.Error(), nil
		}
		if runResp.Error != nil {
			return false, "run error: " + runResp.Error.Message, nil
		}

		var ar struct {
			Reason   string `json:"reason"`
			Response string `json:"response"`
		}
		json.Unmarshal(runResp.Result, &ar)

		raw := map[string]any{
			"reason":   ar.Reason,
			"response": ar.Response,
		}
		if ar.Reason != "input_denied" {
			return false, fmt.Sprintf("Reason = %q, want input_denied (fail-closed)", ar.Reason), raw
		}
		return true, fmt.Sprintf("timeout caused fail-closed deny: reason=%q", ar.Reason), raw
	}))

	return results
}

func scenario(name string, fn func() (bool, string, any)) ScenarioResult {
	fmt.Fprintf(os.Stderr, "\n--- %s ---\n", name)
	start := time.Now()
	pass, details, raw := fn()
	dur := time.Since(start)
	verdict := "FAIL"
	if pass {
		verdict = "PASS"
	}
	fmt.Fprintf(os.Stderr, "[%s] %s (%dms): %s\n", verdict, name, dur.Milliseconds(), details)
	return ScenarioResult{
		Name:     name,
		Pass:     pass,
		Details:  details,
		Duration: fmt.Sprintf("%d", dur.Milliseconds()),
		Raw:      raw,
	}
}

func main() {
	endpoint := os.Getenv("SLLM_ENDPOINT")
	apiKey := os.Getenv("SLLM_API_KEY")
	if endpoint == "" {
		endpoint = "http://localhost:8080/v1/chat/completions"
	}
	if apiKey == "" {
		apiKey = "sk-gemma4"
	}

	binary, err := filepath.Abs("./agent_test_binary")
	if err != nil {
		panic(err)
	}
	if _, err := os.Stat(binary); err != nil {
		fmt.Fprintf(os.Stderr, "agent binary not found at %s — build first with run_verify.sh\n", binary)
		os.Exit(1)
	}

	env := append(os.Environ(),
		"SLLM_ENDPOINT="+endpoint,
		"SLLM_API_KEY="+apiKey,
	)

	results := runScenarios(binary, env)

	out := struct {
		Timestamp string           `json:"timestamp"`
		Endpoint  string           `json:"endpoint"`
		Results   []ScenarioResult `json:"results"`
	}{
		Timestamp: time.Now().Format(time.RFC3339),
		Endpoint:  endpoint,
		Results:   results,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	resultPath := "results/sllm_remote_guards_results.json"
	os.MkdirAll(filepath.Dir(resultPath), 0755)
	os.WriteFile(resultPath, b, 0644)

	fmt.Fprintf(os.Stderr, "\n=== Summary ===\n")
	pass, fail := 0, 0
	for _, r := range results {
		if r.Pass {
			pass++
		} else {
			fail++
		}
	}
	fmt.Fprintf(os.Stderr, "PASS=%d FAIL=%d -> %s\n", pass, fail, resultPath)
	if fail > 0 {
		os.Exit(1)
	}
}
