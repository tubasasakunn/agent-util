package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"ai-agent/internal/engine"
	"ai-agent/internal/mcp"
	"ai-agent/pkg/protocol"
	"ai-agent/pkg/tool"
)

// Handlers は全メソッドハンドラをサーバーに登録する。
type Handlers struct {
	eng      *engine.Engine
	server   *Server
	notifier *Notifier

	runMu      sync.Mutex
	runCancel  context.CancelFunc
	mcpClients []*mcp.Client // 登録された MCP サーバーのクライアント
}

// NewHandlers は Handlers を生成する。
func NewHandlers(eng *engine.Engine, server *Server) *Handlers {
	return &Handlers{
		eng:      eng,
		server:   server,
		notifier: NewNotifier(server),
	}
}

// RegisterAll は全メソッドハンドラをサーバーに登録する。
func (h *Handlers) RegisterAll() {
	h.server.Handle(protocol.MethodAgentRun, h.handleAgentRun)
	h.server.Handle(protocol.MethodAgentAbort, h.handleAgentAbort)
	h.server.Handle(protocol.MethodToolRegister, h.handleToolRegister)
	h.server.Handle(protocol.MethodMCPRegister, h.handleMCPRegister)
}

// CloseAll は登録された MCP サーバーを全て終了する。
func (h *Handlers) CloseAll() {
	for _, c := range h.mcpClients {
		c.Close()
	}
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

	var p protocol.AgentRunParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &protocol.RPCError{
			Code:    protocol.ErrCodeInvalidParams,
			Message: "invalid params: " + err.Error(),
		}
	}

	result, err := h.eng.Run(runCtx, p.Prompt)
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

func (h *Handlers) handleAgentAbort(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	h.runMu.Lock()
	cancel := h.runCancel
	h.runMu.Unlock()

	if cancel == nil {
		return protocol.AgentAbortResult{Aborted: false}, nil
	}
	cancel()
	return protocol.AgentAbortResult{Aborted: true}, nil
}

func (h *Handlers) handleToolRegister(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.ToolRegisterParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &protocol.RPCError{
			Code:    protocol.ErrCodeInvalidParams,
			Message: "invalid params: " + err.Error(),
		}
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
		if err := h.eng.RegisterTool(rt); err != nil {
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
	var p protocol.MCPRegisterParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &protocol.RPCError{
			Code:    protocol.ErrCodeInvalidParams,
			Message: "invalid params: " + err.Error(),
		}
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
		if err := h.eng.RegisterTool(t); err != nil {
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
