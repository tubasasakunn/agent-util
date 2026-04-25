package protocol

import "encoding/json"

// メソッド定数。
const (
	MethodAgentRun      = "agent.run"
	MethodAgentAbort    = "agent.abort"
	MethodToolRegister  = "tool.register"
	MethodToolExecute   = "tool.execute"
	MethodMCPRegister   = "mcp.register"
	MethodStreamDelta   = "stream.delta"
	MethodStreamEnd     = "stream.end"
	MethodContextStatus = "context.status"
)

// --- agent.run ---

// AgentRunParams は agent.run のパラメータ。
type AgentRunParams struct {
	Prompt   string `json:"prompt"`
	MaxTurns int    `json:"max_turns,omitempty"`
}

// AgentRunResult は agent.run の結果。
type AgentRunResult struct {
	Response string    `json:"response"`
	Reason   string    `json:"reason"`
	Turns    int       `json:"turns"`
	Usage    UsageInfo `json:"usage"`
}

// UsageInfo はトークン使用量。
type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --- agent.abort ---

// AgentAbortParams は agent.abort のパラメータ。
type AgentAbortParams struct {
	Reason string `json:"reason,omitempty"`
}

// AgentAbortResult は agent.abort の結果。
type AgentAbortResult struct {
	Aborted bool `json:"aborted"`
}

// --- tool.register ---

// ToolRegisterParams は tool.register のパラメータ。
type ToolRegisterParams struct {
	Tools []ToolDefinition `json:"tools"`
}

// ToolDefinition はラッパーから登録されるツール定義。
// pkg/tool.Definition と同じ構造だが、protocol パッケージの独立性のため別型で定義する。
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	ReadOnly    bool            `json:"read_only,omitempty"`
}

// ToolRegisterResult は tool.register の結果。
type ToolRegisterResult struct {
	Registered int `json:"registered"`
}

// --- tool.execute (コア → ラッパー) ---

// ToolExecuteParams は tool.execute のパラメータ。
type ToolExecuteParams struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolExecuteResult は tool.execute の結果。
type ToolExecuteResult struct {
	Content  string         `json:"content"`
	IsError  bool           `json:"is_error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// --- mcp.register ---

// MCPRegisterParams は mcp.register のパラメータ。
type MCPRegisterParams struct {
	// Transport は通信方式。"stdio"（デフォルト）または "sse"。
	Transport string `json:"transport,omitempty"`

	// stdio 用
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// sse 用
	URL string `json:"url,omitempty"`
}

// MCPRegisterResult は mcp.register の結果。
type MCPRegisterResult struct {
	Tools []string `json:"tools"` // 登録されたツール名の一覧
}

// --- stream.delta (通知) ---

// StreamDeltaParams は stream.delta のパラメータ。
type StreamDeltaParams struct {
	Text string `json:"text"`
	Turn int    `json:"turn,omitempty"`
}

// --- stream.end (通知) ---

// StreamEndParams は stream.end のパラメータ。
type StreamEndParams struct {
	Reason string `json:"reason"`
	Turns  int    `json:"turns"`
}

// --- context.status (通知) ---

// ContextStatusParams は context.status のパラメータ。
type ContextStatusParams struct {
	UsageRatio float64 `json:"usage_ratio"`
	TokenCount int     `json:"token_count"`
	TokenLimit int     `json:"token_limit"`
}
