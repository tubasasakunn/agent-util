package protocol

import "encoding/json"

// メソッド定数。
const (
	MethodAgentRun       = "agent.run"
	MethodAgentAbort     = "agent.abort"
	MethodAgentConfigure = "agent.configure"
	MethodToolRegister   = "tool.register"
	MethodToolExecute    = "tool.execute"
	MethodMCPRegister    = "mcp.register"
	MethodStreamDelta    = "stream.delta"
	MethodStreamEnd      = "stream.end"
	MethodContextStatus  = "context.status"
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

// --- agent.configure ---

// AgentConfigureParams は agent.configure のパラメータ。
// agent.run の前に1回だけ呼び出してハーネスの各機能を有効化/調整する。
// 未指定フィールド（nil）はデフォルト維持、ネスト構造体内の Enabled で機能 on/off を切り替える。
type AgentConfigureParams struct {
	MaxTurns     *int    `json:"max_turns,omitempty"`
	SystemPrompt *string `json:"system_prompt,omitempty"`
	TokenLimit   *int    `json:"token_limit,omitempty"`
	WorkDir      *string `json:"work_dir,omitempty"`

	Delegate    *DelegateConfig    `json:"delegate,omitempty"`
	Coordinator *CoordinatorConfig `json:"coordinator,omitempty"`
	Compaction  *CompactionConfig  `json:"compaction,omitempty"`
	Permission  *PermissionConfig  `json:"permission,omitempty"`
	Guards      *GuardsConfig      `json:"guards,omitempty"`
	Verify      *VerifyConfig      `json:"verify,omitempty"`
	ToolScope   *ToolScopeConfig   `json:"tool_scope,omitempty"`
	Reminder    *ReminderConfig    `json:"reminder,omitempty"`
}

// DelegateConfig は delegate_task サブエージェントの設定。
type DelegateConfig struct {
	Enabled  *bool `json:"enabled,omitempty"`
	MaxChars *int  `json:"max_chars,omitempty"`
}

// CoordinatorConfig は coordinate_tasks 並列サブエージェントの設定。
type CoordinatorConfig struct {
	Enabled  *bool `json:"enabled,omitempty"`
	MaxChars *int  `json:"max_chars,omitempty"`
}

// CompactionConfig はコンテキスト縮約カスケードの設定。
type CompactionConfig struct {
	Enabled        *bool    `json:"enabled,omitempty"`
	BudgetMaxChars *int     `json:"budget_max_chars,omitempty"`
	KeepLast       *int     `json:"keep_last,omitempty"`
	TargetRatio    *float64 `json:"target_ratio,omitempty"`
	// Summarizer はビルトインサマライザー名。"llm" のみ対応（空文字なら Summarizer なし）。
	Summarizer string `json:"summarizer,omitempty"`
}

// PermissionConfig はパーミッションパイプラインの設定。
type PermissionConfig struct {
	Enabled *bool    `json:"enabled,omitempty"`
	Deny    []string `json:"deny,omitempty"`  // ツール名の deny リスト（"*" で全拒否）
	Allow   []string `json:"allow,omitempty"` // ツール名の allow リスト（"*" で全許可）
}

// GuardsConfig は3層ガードレールの設定。各値はビルトイン名のリスト。
type GuardsConfig struct {
	Input    []string `json:"input,omitempty"`
	ToolCall []string `json:"tool_call,omitempty"`
	Output   []string `json:"output,omitempty"`
}

// VerifyConfig は検証ループとエラー回復の設定。
type VerifyConfig struct {
	Verifiers              []string `json:"verifiers,omitempty"`
	MaxStepRetries         *int     `json:"max_step_retries,omitempty"`
	MaxConsecutiveFailures *int     `json:"max_consecutive_failures,omitempty"`
}

// ToolScopeConfig はツールスコーピングの設定。
type ToolScopeConfig struct {
	MaxTools      *int     `json:"max_tools,omitempty"`
	IncludeAlways []string `json:"include_always,omitempty"`
}

// ReminderConfig はシステムリマインダーの設定。
type ReminderConfig struct {
	Threshold *int   `json:"threshold,omitempty"`
	Content   string `json:"content,omitempty"`
}

// AgentConfigureResult は agent.configure の結果。適用された設定の概要を返す。
type AgentConfigureResult struct {
	Applied []string `json:"applied"` // 適用されたフィールド名のリスト
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
