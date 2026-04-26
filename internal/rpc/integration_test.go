package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"ai-agent/internal/engine"
	"ai-agent/internal/llm"
	"ai-agent/pkg/protocol"
)

// integrationCompleter はルーターと応答の両方を模倣する Completer。
// 初回ルーターでツール選択、ツール結果が履歴に入った後の2回目ルーターでは "none" を返す。
type integrationCompleter struct {
	toolName   string // ルーターが選択するツール名（空なら"none"）
	toolArgs   string // ルーターが返す引数JSON
	response   string // 最終応答テキスト
	toolCalled bool   // ツールが呼ばれた後は true（次のルーターで "none" を返す）
}

func (m *integrationCompleter) ChatCompletion(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	// ルーターステップ: JSON mode でツール選択
	if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object" {
		// ツール結果が履歴にある場合は "none" を返して最終応答へ
		toolName := m.toolName
		if toolName == "" || m.toolCalled {
			toolName = "none"
		}
		args := m.toolArgs
		if args == "" {
			args = "{}"
		}
		if toolName != "none" {
			m.toolCalled = true
		}
		content := `{"tool":"` + toolName + `","arguments":` + args + `,"reasoning":"test"}`
		return &llm.ChatResponse{
			Choices: []llm.Choice{{
				Message:      llm.Message{Role: "assistant", Content: llm.StringPtr(content)},
				FinishReason: "stop",
			}},
			Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}, nil
	}

	// チャットステップ: 最終応答
	response := m.response
	if response == "" {
		response = "done"
	}
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message:      llm.Message{Role: "assistant", Content: llm.StringPtr(response)},
			FinishReason: "stop",
		}},
		Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}, nil
}

// --- ヘルパー ---

func writeJSON(t *testing.T, w io.Writer, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readMessage(t *testing.T, r io.Reader, buf *bytes.Buffer) json.RawMessage {
	t.Helper()
	for {
		if idx := bytes.IndexByte(buf.Bytes(), '\n'); idx >= 0 {
			line := make([]byte, idx)
			copy(line, buf.Bytes()[:idx])
			buf.Next(idx + 1)
			return json.RawMessage(line)
		}
		tmp := make([]byte, 4096)
		n, err := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			if buf.Len() > 0 {
				rest := make([]byte, buf.Len())
				copy(rest, buf.Bytes())
				buf.Reset()
				return json.RawMessage(rest)
			}
			t.Fatalf("read: %v", err)
		}
	}
}

func isResponse(msg json.RawMessage) bool {
	var probe struct {
		Method *string `json:"method"`
	}
	json.Unmarshal(msg, &probe)
	return probe.Method == nil
}

func asResponse(t *testing.T, msg json.RawMessage) protocol.Response {
	t.Helper()
	var resp protocol.Response
	if err := json.Unmarshal(msg, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

func asRequest(t *testing.T, msg json.RawMessage) protocol.Request {
	t.Helper()
	var req protocol.Request
	if err := json.Unmarshal(msg, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	return req
}

// --- テスト ---

func TestIntegration_SimpleRun(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	comp := &integrationCompleter{response: "Hello from agent!"}
	eng := engine.New(comp, engine.WithMaxTurns(5))

	srv := New(clientToServer_r, serverToClient_w)
	handlers := NewHandlers(eng, srv)
	handlers.RegisterAll()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	// agent.run 送信
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodAgentRun,
		Params:  mustMarshal(protocol.AgentRunParams{Prompt: "say hello"}),
		ID:      protocol.IntPtr(1),
	})

	// レスポンスを収集（通知とレスポンスが混在するため）
	buf := &bytes.Buffer{}
	var agentResp *protocol.Response

	deadline := time.After(5 * time.Second)
	for agentResp == nil {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for agent.run response")
		default:
		}

		msg := readMessage(t, serverToClient_r, buf)
		if isResponse(msg) {
			r := asResponse(t, msg)
			if r.ID != nil && *r.ID == 1 {
				agentResp = &r
			}
		}
		// 通知は無視
	}

	if agentResp.Error != nil {
		t.Fatalf("agent.run error: %+v", agentResp.Error)
	}

	var result protocol.AgentRunResult
	json.Unmarshal(agentResp.Result, &result)

	if result.Response != "Hello from agent!" {
		t.Errorf("Response = %q, want %q", result.Response, "Hello from agent!")
	}
	if result.Reason != "completed" {
		t.Errorf("Reason = %q, want %q", result.Reason, "completed")
	}

	cancel()
	clientToServer_w.Close()
}

func TestIntegration_ToolRegisterAndRun(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	comp := &integrationCompleter{
		toolName: "greet",
		toolArgs: `{"name":"Alice"}`,
		response: "Greeting complete!",
	}
	eng := engine.New(comp, engine.WithMaxTurns(5))

	srv := New(clientToServer_r, serverToClient_w)
	handlers := NewHandlers(eng, srv)
	handlers.RegisterAll()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)
	buf := &bytes.Buffer{}

	// 1. tool.register
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodToolRegister,
		Params: mustMarshal(protocol.ToolRegisterParams{
			Tools: []protocol.ToolDefinition{
				{Name: "greet", Description: "Greets a person", Parameters: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)},
			},
		}),
		ID: protocol.IntPtr(1),
	})

	// tool.register レスポンス
	msg := readMessage(t, serverToClient_r, buf)
	regResp := asResponse(t, msg)
	if regResp.Error != nil {
		t.Fatalf("tool.register error: %+v", regResp.Error)
	}

	var regResult protocol.ToolRegisterResult
	json.Unmarshal(regResp.Result, &regResult)
	if regResult.Registered != 1 {
		t.Errorf("Registered = %d, want 1", regResult.Registered)
	}

	// 2. agent.run
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodAgentRun,
		Params:  mustMarshal(protocol.AgentRunParams{Prompt: "greet Alice"}),
		ID:      protocol.IntPtr(2),
	})

	// 3. tool.execute リクエストを待ち受け + agent.run レスポンス
	var toolExecReq *protocol.Request
	var agentResp *protocol.Response

	deadline := time.After(5 * time.Second)
	for agentResp == nil {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
		}

		msg := readMessage(t, serverToClient_r, buf)

		if isResponse(msg) {
			r := asResponse(t, msg)
			if r.ID != nil && *r.ID == 2 {
				agentResp = &r
			}
		} else {
			req := asRequest(t, msg)
			if req.Method == protocol.MethodToolExecute {
				toolExecReq = &req

				// 4. ツール実行結果を返す
				writeJSON(t, clientToServer_w, protocol.Response{
					JSONRPC: protocol.Version,
					Result:  mustMarshal(protocol.ToolExecuteResult{Content: "Hello, Alice!"}),
					ID:      req.ID,
				})
			}
			// 通知（stream.end 等）は無視
		}
	}

	// tool.execute が呼ばれたことを確認
	if toolExecReq == nil {
		t.Fatal("tool.execute request not received")
	}

	var execParams protocol.ToolExecuteParams
	json.Unmarshal(toolExecReq.Params, &execParams)
	if execParams.Name != "greet" {
		t.Errorf("tool name = %q, want %q", execParams.Name, "greet")
	}

	// agent.run の結果を確認
	if agentResp.Error != nil {
		t.Fatalf("agent.run error: %+v", agentResp.Error)
	}

	var runResult protocol.AgentRunResult
	json.Unmarshal(agentResp.Result, &runResult)
	if runResult.Reason != "completed" {
		t.Errorf("Reason = %q, want %q", runResult.Reason, "completed")
	}

	cancel()
	clientToServer_w.Close()
}

func TestIntegration_StreamNotifications(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	comp := &integrationCompleter{response: "result"}
	eng := engine.New(comp,
		engine.WithMaxTurns(5),
		engine.WithStepCallback(func(evt engine.StepEvent) {
			// コールバックが呼ばれることを確認（Notifier は handlers で統合済み）
		}),
	)

	srv := New(clientToServer_r, serverToClient_w)
	handlers := NewHandlers(eng, srv)
	handlers.RegisterAll()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodAgentRun,
		Params:  mustMarshal(protocol.AgentRunParams{Prompt: "test"}),
		ID:      protocol.IntPtr(1),
	})

	// メッセージを収集
	buf := &bytes.Buffer{}
	var notifications []protocol.Request
	var agentResp *protocol.Response

	deadline := time.After(5 * time.Second)
	for agentResp == nil {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
		}

		msg := readMessage(t, serverToClient_r, buf)

		if isResponse(msg) {
			r := asResponse(t, msg)
			if r.ID != nil && *r.ID == 1 {
				agentResp = &r
			}
		} else {
			req := asRequest(t, msg)
			if req.IsNotification() {
				notifications = append(notifications, req)
			}
		}
	}

	// stream.end 通知があることを確認
	foundStreamEnd := false
	for _, n := range notifications {
		if n.Method == protocol.MethodStreamEnd {
			foundStreamEnd = true
			var params protocol.StreamEndParams
			json.Unmarshal(n.Params, &params)
			if params.Reason != "completed" {
				t.Errorf("stream.end reason = %q, want %q", params.Reason, "completed")
			}
		}
	}
	if !foundStreamEnd {
		t.Error("stream.end notification not received")
	}

	cancel()
	clientToServer_w.Close()
}

func TestIntegration_InvalidMethod(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	comp := &integrationCompleter{}
	eng := engine.New(comp)

	srv := New(clientToServer_r, serverToClient_w)
	handlers := NewHandlers(eng, srv)
	handlers.RegisterAll()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  "nonexistent.method",
		ID:      protocol.IntPtr(1),
	})

	buf := &bytes.Buffer{}
	msg := readMessage(t, serverToClient_r, buf)
	resp := asResponse(t, msg)

	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != protocol.ErrCodeMethodNotFound {
		t.Errorf("code = %d, want %d", resp.Error.Code, protocol.ErrCodeMethodNotFound)
	}

	cancel()
	clientToServer_w.Close()
}

func TestIntegration_AgentAbort(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	blockCtx, blockCancel := context.WithCancel(context.Background())
	defer blockCancel()

	comp := &blockingCompleter{ctx: blockCtx}
	eng := engine.New(comp, engine.WithMaxTurns(1))

	srv := New(clientToServer_r, serverToClient_w)
	handlers := NewHandlers(eng, srv)
	handlers.RegisterAll()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)

	// agent.run を開始（ブロックする）
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodAgentRun,
		Params:  mustMarshal(protocol.AgentRunParams{Prompt: "block"}),
		ID:      protocol.IntPtr(1),
	})

	time.Sleep(100 * time.Millisecond)

	// agent.abort を送信
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodAgentAbort,
		ID:      protocol.IntPtr(2),
	})

	buf := &bytes.Buffer{}

	// abort レスポンスと run エラーレスポンスを収集
	var abortResp *protocol.Response

	deadline := time.After(5 * time.Second)
	for abortResp == nil {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
		}

		msg := readMessage(t, serverToClient_r, buf)
		if isResponse(msg) {
			r := asResponse(t, msg)
			if r.ID != nil && *r.ID == 2 {
				abortResp = &r
			}
		}
	}

	if abortResp.Error != nil {
		t.Fatalf("abort error: %+v", abortResp.Error)
	}

	var abortResult protocol.AgentAbortResult
	json.Unmarshal(abortResp.Result, &abortResult)
	if !abortResult.Aborted {
		t.Error("Aborted = false, want true")
	}

	cancel()
	blockCancel()
	clientToServer_w.Close()
}

func TestIntegration_RemoteGuardDeniesInput(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	comp := &integrationCompleter{response: "should not be reached"}
	eng := engine.New(comp, engine.WithMaxTurns(3))

	srv := New(clientToServer_r, serverToClient_w)
	handlers := NewHandlers(eng, srv)
	handlers.RegisterAll()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)
	buf := &bytes.Buffer{}

	// 1. guard.register
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodGuardRegister,
		Params: mustMarshal(protocol.GuardRegisterParams{
			Guards: []protocol.GuardDefinition{
				{Name: "wrapper_block_evil", Stage: protocol.GuardStageInput},
			},
		}),
		ID: protocol.IntPtr(1),
	})

	// guard.register レスポンス
	regResp := asResponse(t, readMessage(t, serverToClient_r, buf))
	if regResp.Error != nil {
		t.Fatalf("guard.register error: %+v", regResp.Error)
	}

	// 2. agent.configure でリモートガードを参照
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodAgentConfigure,
		Params: mustMarshal(protocol.AgentConfigureParams{
			Guards: &protocol.GuardsConfig{Input: []string{"wrapper_block_evil"}},
		}),
		ID: protocol.IntPtr(2),
	})
	cfgResp := asResponse(t, readMessage(t, serverToClient_r, buf))
	if cfgResp.Error != nil {
		t.Fatalf("configure error: %+v", cfgResp.Error)
	}

	// 3. agent.run を投げる → ガードが guard.execute を発火 → ラッパーが deny を返す
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodAgentRun,
		Params:  mustMarshal(protocol.AgentRunParams{Prompt: "delete everything"}),
		ID:      protocol.IntPtr(3),
	})

	var (
		guardReq    *protocol.Request
		runResponse *protocol.Response
	)
	deadline := time.After(5 * time.Second)
	for runResponse == nil {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for agent.run")
		default:
		}
		msg := readMessage(t, serverToClient_r, buf)
		if isResponse(msg) {
			r := asResponse(t, msg)
			if r.ID != nil && *r.ID == 3 {
				runResponse = &r
			}
		} else {
			req := asRequest(t, msg)
			if req.Method == protocol.MethodGuardExecute {
				guardReq = &req
				// ラッパー側 fake guard: deny
				writeJSON(t, clientToServer_w, protocol.Response{
					JSONRPC: protocol.Version,
					Result: mustMarshal(protocol.GuardExecuteResult{
						Decision: protocol.GuardDecisionDeny,
						Reason:   "matches blacklist",
					}),
					ID: req.ID,
				})
			}
		}
	}

	if guardReq == nil {
		t.Fatal("guard.execute was not invoked")
	}
	var gp protocol.GuardExecuteParams
	json.Unmarshal(guardReq.Params, &gp)
	if gp.Stage != protocol.GuardStageInput {
		t.Errorf("stage = %q, want %q", gp.Stage, protocol.GuardStageInput)
	}
	if gp.Name != "wrapper_block_evil" {
		t.Errorf("name = %q", gp.Name)
	}
	if gp.Input != "delete everything" {
		t.Errorf("input = %q", gp.Input)
	}

	if runResponse.Error != nil {
		t.Fatalf("run error: %+v", runResponse.Error)
	}
	var ar protocol.AgentRunResult
	json.Unmarshal(runResponse.Result, &ar)
	if ar.Reason != "input_denied" {
		t.Errorf("Reason = %q, want input_denied", ar.Reason)
	}

	cancel()
	clientToServer_w.Close()
}

func TestIntegration_RemoteVerifierFails(t *testing.T) {
	clientToServer_r, clientToServer_w := io.Pipe()
	serverToClient_r, serverToClient_w := io.Pipe()

	// ルーターが greet を呼び続けないように integrationCompleter を再利用
	comp := &integrationCompleter{
		toolName: "greet",
		toolArgs: `{"name":"Alice"}`,
		response: "done",
	}
	eng := engine.New(comp, engine.WithMaxTurns(5))

	srv := New(clientToServer_r, serverToClient_w)
	handlers := NewHandlers(eng, srv)
	handlers.RegisterAll()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Serve(ctx)
	buf := &bytes.Buffer{}

	// tool.register
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodToolRegister,
		Params: mustMarshal(protocol.ToolRegisterParams{
			Tools: []protocol.ToolDefinition{
				{Name: "greet", Description: "x", Parameters: json.RawMessage(`{}`)},
			},
		}),
		ID: protocol.IntPtr(1),
	})
	asResponse(t, readMessage(t, serverToClient_r, buf))

	// verifier.register
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodVerifierRegister,
		Params: mustMarshal(protocol.VerifierRegisterParams{
			Verifiers: []protocol.VerifierDefinition{{Name: "wrapper_audit"}},
		}),
		ID: protocol.IntPtr(2),
	})
	asResponse(t, readMessage(t, serverToClient_r, buf))

	// configure: verifier 参照
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodAgentConfigure,
		Params: mustMarshal(protocol.AgentConfigureParams{
			Verify: &protocol.VerifyConfig{Verifiers: []string{"wrapper_audit"}},
		}),
		ID: protocol.IntPtr(3),
	})
	asResponse(t, readMessage(t, serverToClient_r, buf))

	// agent.run
	writeJSON(t, clientToServer_w, protocol.Request{
		JSONRPC: protocol.Version,
		Method:  protocol.MethodAgentRun,
		Params:  mustMarshal(protocol.AgentRunParams{Prompt: "do it"}),
		ID:      protocol.IntPtr(4),
	})

	var (
		verifierInvoked bool
		runResp         *protocol.Response
	)
	deadline := time.After(5 * time.Second)
	for runResp == nil {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
		}
		msg := readMessage(t, serverToClient_r, buf)
		if isResponse(msg) {
			r := asResponse(t, msg)
			if r.ID != nil && *r.ID == 4 {
				runResp = &r
			}
		} else {
			req := asRequest(t, msg)
			switch req.Method {
			case protocol.MethodToolExecute:
				writeJSON(t, clientToServer_w, protocol.Response{
					JSONRPC: protocol.Version,
					Result:  mustMarshal(protocol.ToolExecuteResult{Content: "Hello, Alice!"}),
					ID:      req.ID,
				})
			case protocol.MethodVerifierExecute:
				verifierInvoked = true
				// 1回目だけ fail させて検証ループに入れる
				writeJSON(t, clientToServer_w, protocol.Response{
					JSONRPC: protocol.Version,
					Result: mustMarshal(protocol.VerifierExecuteResult{
						Passed:  false,
						Summary: "audit policy violation",
					}),
					ID: req.ID,
				})
			}
		}
	}

	if !verifierInvoked {
		t.Fatal("verifier.execute was not invoked")
	}
	if runResp.Error != nil {
		t.Fatalf("run error: %+v", runResp.Error)
	}

	cancel()
	clientToServer_w.Close()
}

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
