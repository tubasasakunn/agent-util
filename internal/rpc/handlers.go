package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"ai-agent/internal/engine"
	"ai-agent/internal/llm"
	"ai-agent/internal/mcp"
	"ai-agent/pkg/protocol"
	"ai-agent/pkg/tool"
)

// Handlers は全メソッドハンドラをサーバーに登録する。
type Handlers struct {
	server   *Server
	notifier *Notifier

	engMu sync.Mutex
	eng   *engine.Engine // agent.configure で差し替えられるため Mutex で保護

	runMu      sync.Mutex
	runCancel  context.CancelFunc
	mcpClients []*mcp.Client // 登録された MCP サーバーのクライアント

	remote *RemoteRegistry // ラッパーから登録された名前付きガード/Verifier
}

// NewHandlers は Handlers を生成する。
func NewHandlers(eng *engine.Engine, server *Server) *Handlers {
	return &Handlers{
		eng:      eng,
		server:   server,
		notifier: NewNotifier(server),
		remote:   NewRemoteRegistry(),
	}
}

// RegisterAll は全メソッドハンドラをサーバーに登録する。
func (h *Handlers) RegisterAll() {
	h.server.Handle(protocol.MethodAgentRun, h.handleAgentRun)
	h.server.Handle(protocol.MethodAgentAbort, h.handleAgentAbort)
	h.server.Handle(protocol.MethodAgentConfigure, h.handleAgentConfigure)
	h.server.Handle(protocol.MethodToolRegister, h.handleToolRegister)
	h.server.Handle(protocol.MethodMCPRegister, h.handleMCPRegister)
	h.server.Handle(protocol.MethodGuardRegister, h.handleGuardRegister)
	h.server.Handle(protocol.MethodVerifierRegister, h.handleVerifierRegister)
	// セッション管理・コンテキスト操作
	h.server.Handle(protocol.MethodSessionHistory, h.handleSessionHistory)
	h.server.Handle(protocol.MethodSessionInject, h.handleSessionInject)
	h.server.Handle(protocol.MethodContextSummarize, h.handleContextSummarize)
}

// RemoteRegistry は登録済みのリモートガード/Verifier を返す（テスト/動的差し替え確認用）。
func (h *Handlers) RemoteRegistry() *RemoteRegistry { return h.remote }

// Engine は現在保持している Engine を返す。テストや動的差し替えの確認用。
func (h *Handlers) Engine() *engine.Engine {
	h.engMu.Lock()
	defer h.engMu.Unlock()
	return h.eng
}

// CloseAll は登録された MCP サーバーを全て終了する。
func (h *Handlers) CloseAll() {
	for _, c := range h.mcpClients {
		c.Close()
	}
}

// unmarshalParams は JSON-RPC params を型 T にデコードする共通ヘルパー。
func unmarshalParams[T any](params json.RawMessage) (T, *protocol.RPCError) {
	var p T
	if err := json.Unmarshal(params, &p); err != nil {
		return p, &protocol.RPCError{
			Code:    protocol.ErrCodeInvalidParams,
			Message: "invalid params: " + err.Error(),
		}
	}
	return p, nil
}

func (h *Handlers) handleAgentRun(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	h.runMu.Lock()
	if h.runCancel != nil {
		h.runMu.Unlock()
		return nil, &protocol.RPCError{
			Code:    protocol.ErrCodeAgentBusy,
			Message: "agent.run is already in progress",
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	h.runCancel = cancel
	h.runMu.Unlock()

	defer func() {
		cancel()
		h.runMu.Lock()
		h.runCancel = nil
		h.runMu.Unlock()
	}()

	p, rpcErr := unmarshalParams[protocol.AgentRunParams](params)
	if rpcErr != nil {
		return nil, rpcErr
	}

	result, err := h.Engine().Run(runCtx, p.Prompt)
	if err != nil {
		h.notifier.StreamEnd("error", 0)
		return nil, &protocol.RPCError{
			Code:    protocol.ErrCodeInternal,
			Message: err.Error(),
		}
	}

	h.notifier.StreamEnd(result.Reason, result.Turns)

	return protocol.AgentRunResult{
		Response: result.Response,
		Reason:   result.Reason,
		Turns:    result.Turns,
		Usage: protocol.UsageInfo{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
		},
	}, nil
}

func (h *Handlers) handleAgentConfigure(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	h.runMu.Lock()
	busy := h.runCancel != nil
	h.runMu.Unlock()
	if busy {
		return nil, &protocol.RPCError{
			Code:    protocol.ErrCodeAgentBusy,
			Message: "agent.configure is not allowed while agent.run is in progress",
		}
	}

	p, rpcErr := unmarshalParams[protocol.AgentConfigureParams](params)
	if rpcErr != nil {
		return nil, rpcErr
	}

	h.engMu.Lock()
	defer h.engMu.Unlock()
	newEng, applied, err := rebuildEngine(h.eng, &p, h.notifier, h.remote)
	if err != nil {
		return nil, &protocol.RPCError{
			Code:    protocol.ErrCodeInvalidParams,
			Message: err.Error(),
		}
	}
	h.eng = newEng

	return protocol.AgentConfigureResult{Applied: applied}, nil
}

func (h *Handlers) handleAgentAbort(_ context.Context, _ json.RawMessage) (any, *protocol.RPCError) {
	h.runMu.Lock()
	cancel := h.runCancel
	h.runMu.Unlock()

	if cancel == nil {
		return protocol.AgentAbortResult{Aborted: false}, nil
	}
	cancel()
	return protocol.AgentAbortResult{Aborted: true}, nil
}

func (h *Handlers) handleToolRegister(_ context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	p, rpcErr := unmarshalParams[protocol.ToolRegisterParams](params)
	if rpcErr != nil {
		return nil, rpcErr
	}

	registered := 0
	for _, def := range p.Tools {
		td := tool.Definition{
			Name:        def.Name,
			Description: def.Description,
			Parameters:  def.Parameters,
			ReadOnly:    def.ReadOnly,
		}
		rt := NewRemoteTool(td, h.server)
		if err := h.Engine().RegisterTool(rt); err != nil {
			return nil, &protocol.RPCError{
				Code:    protocol.ErrCodeInvalidParams,
				Message: fmt.Sprintf("register tool %q: %s", def.Name, err.Error()),
			}
		}
		registered++
	}

	return protocol.ToolRegisterResult{Registered: registered}, nil
}

func (h *Handlers) handleMCPRegister(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	p, rpcErr := unmarshalParams[protocol.MCPRegisterParams](params)
	if rpcErr != nil {
		return nil, rpcErr
	}

	cfg := mcp.ServerConfig{
		Transport: p.Transport,
		Command:   p.Command,
		Args:      p.Args,
		Env:       p.Env,
		URL:       p.URL,
	}

	client, tools, err := mcp.RegisterMCPServer(ctx, cfg)
	if err != nil {
		return nil, &protocol.RPCError{
			Code:    protocol.ErrCodeInternal,
			Message: fmt.Sprintf("mcp register: %s", err),
		}
	}

	h.mcpClients = append(h.mcpClients, client)

	names := make([]string, 0, len(tools))
	for _, t := range tools {
		if err := h.Engine().RegisterTool(t); err != nil {
			client.Close()
			return nil, &protocol.RPCError{
				Code:    protocol.ErrCodeInvalidParams,
				Message: fmt.Sprintf("register mcp tool %q: %s", t.Name(), err),
			}
		}
		names = append(names, t.Name())
	}

	return protocol.MCPRegisterResult{Tools: names}, nil
}

func (h *Handlers) handleGuardRegister(_ context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	p, rpcErr := unmarshalParams[protocol.GuardRegisterParams](params)
	if rpcErr != nil {
		return nil, rpcErr
	}

	registered := 0
	for _, def := range p.Guards {
		if def.Name == "" {
			return nil, &protocol.RPCError{
				Code:    protocol.ErrCodeInvalidParams,
				Message: "guard name must not be empty",
			}
		}
		switch def.Stage {
		case protocol.GuardStageInput:
			h.remote.AddInputGuard(NewRemoteInputGuard(def.Name, h.server))
		case protocol.GuardStageToolCall:
			h.remote.AddToolCallGuard(NewRemoteToolCallGuard(def.Name, h.server))
		case protocol.GuardStageOutput:
			h.remote.AddOutputGuard(NewRemoteOutputGuard(def.Name, h.server))
		default:
			return nil, &protocol.RPCError{
				Code: protocol.ErrCodeInvalidParams,
				Message: fmt.Sprintf("guard %q: unknown stage %q (expected input|tool_call|output)",
					def.Name, def.Stage),
			}
		}
		registered++
	}

	return protocol.GuardRegisterResult{Registered: registered}, nil
}

func (h *Handlers) handleVerifierRegister(_ context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	p, rpcErr := unmarshalParams[protocol.VerifierRegisterParams](params)
	if rpcErr != nil {
		return nil, rpcErr
	}

	registered := 0
	for _, def := range p.Verifiers {
		if def.Name == "" {
			return nil, &protocol.RPCError{
				Code:    protocol.ErrCodeInvalidParams,
				Message: "verifier name must not be empty",
			}
		}
		h.remote.AddVerifier(NewRemoteVerifier(def.Name, h.server))
		registered++
	}

	return protocol.VerifierRegisterResult{Registered: registered}, nil
}

// handleSessionHistory は現在の会話履歴を返す。
func (h *Handlers) handleSessionHistory(_ context.Context, _ json.RawMessage) (any, *protocol.RPCError) {
	msgs := h.Engine().History()

	sessionMsgs := make([]protocol.SessionMessage, 0, len(msgs))
	for _, msg := range msgs {
		sm := protocol.SessionMessage{
			Role:       msg.Role,
			Content:    msg.ContentString(),
			ToolCallID: msg.ToolCallID,
		}
		for _, tc := range msg.ToolCalls {
			sm.ToolCalls = append(sm.ToolCalls, protocol.SessionToolCall{
				ID: tc.ID,
				Function: protocol.SessionToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: string(tc.Function.Arguments),
				},
			})
		}
		sessionMsgs = append(sessionMsgs, sm)
	}
	return protocol.SessionHistoryResult{Messages: sessionMsgs, Count: len(sessionMsgs)}, nil
}

// handleSessionInject は指定位置にメッセージを注入する。
func (h *Handlers) handleSessionInject(_ context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	p, rpcErr := unmarshalParams[protocol.SessionInjectParams](params)
	if rpcErr != nil {
		return nil, rpcErr
	}

	msgs := make([]llm.Message, 0, len(p.Messages))
	for _, sm := range p.Messages {
		content := sm.Content
		msg := llm.Message{
			Role:       sm.Role,
			Content:    &content,
			ToolCallID: sm.ToolCallID,
		}
		for _, tc := range sm.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: json.RawMessage(tc.Function.Arguments),
				},
			})
		}
		msgs = append(msgs, msg)
	}

	position := p.Position
	if position == "" {
		position = "append"
	}

	h.engMu.Lock()
	h.eng.Inject(msgs, position)
	total := len(h.eng.History())
	h.engMu.Unlock()

	return protocol.SessionInjectResult{Injected: len(msgs), Total: total}, nil
}

// handleContextSummarize は会話履歴を LLM で要約して返す。
func (h *Handlers) handleContextSummarize(ctx context.Context, _ json.RawMessage) (any, *protocol.RPCError) {
	h.engMu.Lock()
	eng := h.eng
	h.engMu.Unlock()

	summary, err := eng.Summarize(ctx)
	if err != nil {
		return nil, &protocol.RPCError{
			Code:    protocol.ErrCodeInternal,
			Message: fmt.Sprintf("context.summarize: %s", err),
		}
	}
	return protocol.ContextSummarizeResult{Summary: summary, Length: len(summary)}, nil
}
