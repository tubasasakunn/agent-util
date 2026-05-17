package protocol

import "encoding/json"

// メソッド定数。
const (
	MethodAgentRun         = "agent.run"
	MethodAgentAbort       = "agent.abort"
	MethodAgentConfigure   = "agent.configure"
	MethodToolRegister     = "tool.register"
	MethodToolExecute      = "tool.execute"
	MethodMCPRegister      = "mcp.register"
	MethodGuardRegister    = "guard.register"
	MethodGuardExecute     = "guard.execute"
	MethodVerifierRegister = "verifier.register"
	MethodVerifierExecute  = "verifier.execute"
	MethodStreamDelta      = "stream.delta"
	MethodStreamEnd        = "stream.end"
	MethodContextStatus    = "context.status"

	// LLM 実行をラッパー側に委譲する逆 RPC（コア → ラッパー）
	MethodLLMExecute = "llm.execute"

	// サーバー情報取得（バージョン / 対応メソッド / 機能フラグ）
	// ラッパー起動時のハンドシェイクに使う。
	MethodServerInfo = "server.info"

	// セッション管理（会話履歴のエクスポート・注入・スナップショット）
	MethodSessionHistory = "session.history"
	MethodSessionInject  = "session.inject"

	// コンテキスト操作
	MethodContextSummarize = "context.summarize"

	// ゴール判定（ラッパー → コア登録、コア → ラッパー評価）
	MethodJudgeRegister = "judge.register"
	MethodJudgeEvaluate = "judge.evaluate"
)

// LoopConfig.Type に指定できる値。
const (
	LoopTypeReact = "react" // デフォルト: ルーター→ツール→レスポンスの ReAct ループ
	LoopTypeReaf  = "reaf"  // ReAF: ルーター→ツール→評価ファンクション→レスポンスのループ
)

// LLMConfig.Mode に指定できる値。
const (
	LLMModeHTTP   = "http"   // デフォルト: 内蔵 HTTP クライアント (OpenAI 互換)
	LLMModeRemote = "remote" // ラッパーに llm.execute で委譲
)

// ガードのステージ識別子（GuardDefinition.Stage / GuardExecuteParams.Stage）。
const (
	GuardStageInput    = "input"
	GuardStageToolCall = "tool_call"
	GuardStageOutput   = "output"
)

// ガードの判定識別子（GuardExecuteResult.Decision）。
const (
	GuardDecisionAllow    = "allow"
	GuardDecisionDeny     = "deny"
	GuardDecisionTripwire = "tripwire"
)

// --- server.info ---

// ServerInfoResult は server.info の結果。
// バイナリのバージョンと、対応する RPC メソッド・機能フラグを返す。
// ラッパーは起動時にこれを呼び、SDK バージョンとの互換性を確認できる。
type ServerInfoResult struct {
	// LibraryVersion はバイナリの semver (例: "0.2.1")。
	LibraryVersion string `json:"library_version"`
	// ProtocolVersion は JSON-RPC 自体のバージョン (固定で "2.0")。
	ProtocolVersion string `json:"protocol_version"`
	// Methods は server がサポートする RPC メソッド名一覧。
	Methods []string `json:"methods"`
	// Features は機能フラグの map (例: {"llm.execute": true, "context.summarize": true})。
	// SDK は未対応機能を呼ぶ前にここで判定できる。
	Features map[string]bool `json:"features"`
}

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
	Streaming   *StreamingConfig   `json:"streaming,omitempty"`
	// ループパターンと拡張コンポーネント
	Loop   *LoopConfig   `json:"loop,omitempty"`
	Router *RouterConfig `json:"router,omitempty"`
	Judge  *JudgeConfig  `json:"judge,omitempty"`
	LLM    *LLMConfig    `json:"llm,omitempty"`
}

// LLMConfig はメイン LLM のドライバ設定。
// Mode="remote" のとき、すべての ChatCompletion 呼び出しが llm.execute 経由で
// ラッパーに委譲される。任意 API 形式 (Anthropic / Bedrock / ollama 等) に対応するための拡張ポイント。
// Mode="" または "http" の場合は内蔵 HTTP クライアント (OpenAI 互換) を使う。
// TimeoutSeconds は llm.execute 1 回あたりのタイムアウト (0 ならデフォルト)。
type LLMConfig struct {
	Mode           string `json:"mode,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// LoopConfig はエージェントの実行ループパターンを設定する。
type LoopConfig struct {
	// Type は "react"（デフォルト）または "reaf"。
	Type string `json:"type"`
}

// RouterConfig はルーターステップ専用 LLM の接続設定。
// 未設定時はメイン LLM（SLLM_ENDPOINT 等）をルーターにも使用する。
type RouterConfig struct {
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
}

// JudgeConfig は agent.configure でゴール判定器を有効化する設定。
//
// 1. Name を指定すると judge.register で登録した判定器が使われる。
// 2. Name が空で Builtin が指定されると内蔵判定器が使われる (A2)。
// 3. 両方未指定なら判定器なし (旧挙動)。
type JudgeConfig struct {
	Name string `json:"name,omitempty"`
	// Builtin は内蔵判定器の名前 (A2)。例:
	//   "min_length:30" — assistant の応答が 30 文字以上なら done
	//   "min_length:120" — 120 文字以上で done
	// Name より優先度が低い。
	Builtin string `json:"builtin,omitempty"`
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
	// ToolBudget は同一ツールの呼び出し回数上限 (A1/A4)。
	// 例: {"shell": 1} なら shell は 1 回呼んだら以後選べない。
	// 未指定のツールは上限なし。0 を入れると「最初から禁止」に近い動作になる。
	ToolBudget map[string]int `json:"tool_budget,omitempty"`
}

// ReminderConfig はシステムリマインダーの設定。
type ReminderConfig struct {
	Threshold *int   `json:"threshold,omitempty"`
	Content   string `json:"content,omitempty"`
}

// StreamingConfig はストリーミング通知の設定。
// Enabled が true で stream.delta、ContextStatus が true で context.status の通知を送る。
type StreamingConfig struct {
	Enabled       *bool `json:"enabled,omitempty"`
	ContextStatus *bool `json:"context_status,omitempty"`
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

// --- llm.execute (コア → ラッパー) ---

// LLMExecuteParams は llm.execute のパラメータ。
// LLM 呼び出しをラッパー側に委譲し、任意の API 形式 (OpenAI / Anthropic /
// Bedrock / ollama / mock 等) に変換させるための逆 RPC。
//
// Request はコアが組み立てた OpenAI 互換の ChatRequest を表す JSON。
// ラッパー側はこれを各バックエンドの形式に変換して呼び出し、結果を Response に詰めて返す。
// json.RawMessage で透過させることで、将来 ChatRequest にフィールドが増えても
// プロトコル定義を変えずに渡せる。
type LLMExecuteParams struct {
	Request json.RawMessage `json:"request"`
}

// LLMExecuteResult は llm.execute の結果。
// Response は OpenAI 互換の ChatResponse 形式の JSON。
// ラッパー側は最低限 `choices[0].message.content` (または `tool_calls`) を含めて返す。
type LLMExecuteResult struct {
	Response json.RawMessage `json:"response"`
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

// --- guard.register (ラッパー → コア) ---

// GuardRegisterParams は guard.register のパラメータ。
// ラッパー側で実装した名前付きガードをコアに登録する。
// 登録した名前は agent.configure の guards.input/tool_call/output から参照できる。
type GuardRegisterParams struct {
	Guards []GuardDefinition `json:"guards"`
}

// GuardDefinition はラッパーから登録されるガード定義。
// Stage は GuardStageInput/GuardStageToolCall/GuardStageOutput のいずれか。
type GuardDefinition struct {
	Name  string `json:"name"`
	Stage string `json:"stage"`
}

// GuardRegisterResult は guard.register の結果。
type GuardRegisterResult struct {
	Registered int `json:"registered"`
}

// --- guard.execute (コア → ラッパー) ---

// GuardExecuteParams は guard.execute のパラメータ。
// Stage に応じて Input / ToolName+Args / Output のいずれかが入る。
type GuardExecuteParams struct {
	Name     string          `json:"name"`
	Stage    string          `json:"stage"`
	Input    string          `json:"input,omitempty"`
	ToolName string          `json:"tool_name,omitempty"`
	Args     json.RawMessage `json:"args,omitempty"`
	Output   string          `json:"output,omitempty"`
}

// GuardExecuteResult は guard.execute の結果。
// Decision は GuardDecisionAllow/GuardDecisionDeny/GuardDecisionTripwire のいずれか。
type GuardExecuteResult struct {
	Decision string   `json:"decision"`
	Reason   string   `json:"reason,omitempty"`
	Details  []string `json:"details,omitempty"`
}

// --- verifier.register (ラッパー → コア) ---

// VerifierRegisterParams は verifier.register のパラメータ。
type VerifierRegisterParams struct {
	Verifiers []VerifierDefinition `json:"verifiers"`
}

// VerifierDefinition はラッパーから登録される Verifier 定義。
type VerifierDefinition struct {
	Name string `json:"name"`
}

// VerifierRegisterResult は verifier.register の結果。
type VerifierRegisterResult struct {
	Registered int `json:"registered"`
}

// --- verifier.execute (コア → ラッパー) ---

// VerifierExecuteParams は verifier.execute のパラメータ。
type VerifierExecuteParams struct {
	Name     string          `json:"name"`
	ToolName string          `json:"tool_name"`
	Args     json.RawMessage `json:"args,omitempty"`
	Result   string          `json:"result"`
}

// VerifierExecuteResult は verifier.execute の結果。
type VerifierExecuteResult struct {
	Passed  bool     `json:"passed"`
	Summary string   `json:"summary,omitempty"`
	Details []string `json:"details,omitempty"`
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
//
// ラッパー側 (SDK) は UsageRatio / TokenCount / TokenLimit でゲージを描き、
// LastEvent / LastMessageRole / CompactionDelta で「今何が起きたか」を表示できる
// (C1/C2/C3/C5)。LastEvent が "compacted" なら CompactionDelta に縮約された
// トークン数が入る。
type ContextStatusParams struct {
	UsageRatio float64 `json:"usage_ratio"`
	TokenCount int     `json:"token_count"`
	TokenLimit int     `json:"token_limit"`
	// LastEvent は直近で発生した文脈イベント。可能な値:
	//   "user_added"      ユーザーメッセージが履歴に追加された
	//   "assistant_added" assistant メッセージが履歴に追加された
	//   "tool_added"      tool 結果が履歴に追加された
	//   "compacted"       compaction カスケードが走った
	//   ""                未指定 (周期的なゲージ更新)
	LastEvent string `json:"last_event,omitempty"`
	// LastMessageRole は直近に追加されたメッセージの role。
	// "user" / "assistant" / "tool" 等。LastEvent と紐づく。
	LastMessageRole string `json:"last_message_role,omitempty"`
	// CompactionDelta は LastEvent == "compacted" の場合、縮約で削減された
	// トークン数。
	CompactionDelta int `json:"compaction_delta,omitempty"`
}

// --- session.history ---

// SessionMessage は会話履歴の1メッセージ。role/content のフラットな表現。
type SessionMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	ToolCalls  []SessionToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

// SessionToolCall はアシスタントが発行したツール呼び出し。
type SessionToolCall struct {
	ID       string                  `json:"id"`
	Function SessionToolCallFunction `json:"function"`
}

// SessionToolCallFunction はツール呼び出しの関数名と引数。
type SessionToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// SessionHistoryResult は session.history の結果。
type SessionHistoryResult struct {
	Messages []SessionMessage `json:"messages"`
	Count    int              `json:"count"`
}

// --- session.inject ---

// SessionInjectParams は session.inject のパラメータ。
// Position は "prepend"（先頭挿入）/ "append"（末尾追加）/ "replace"（全置換）。
type SessionInjectParams struct {
	Messages []SessionMessage `json:"messages"`
	Position string           `json:"position,omitempty"` // default: "append"
}

// SessionInjectResult は session.inject の結果。
type SessionInjectResult struct {
	Injected int `json:"injected"`
	Total    int `json:"total"`
}

// --- context.summarize ---

// ContextSummarizeResult は context.summarize の結果。
type ContextSummarizeResult struct {
	Summary string `json:"summary"`
	Length  int    `json:"length"`
}

// --- judge.register (ラッパー → コア) ---

// JudgeRegisterParams は judge.register のパラメータ。
// ラッパー側で実装したゴール判定器をコアに登録する。
// 登録した名前は agent.configure の judge.name から参照できる。
type JudgeRegisterParams struct {
	Name string `json:"name"`
}

// JudgeRegisterResult は judge.register の結果。
type JudgeRegisterResult struct {
	Registered int `json:"registered"`
}

// --- judge.evaluate (コア → ラッパー) ---

// JudgeEvaluateParams は judge.evaluate のパラメータ。
// コアが各ターンの "completed" 応答後にラッパーへ送信する。
type JudgeEvaluateParams struct {
	Name     string `json:"name"`
	Response string `json:"response"`
	Turn     int    `json:"turn"`
}

// JudgeEvaluateResult は judge.evaluate の結果。
// ラッパーがゴール達成の判定結果を返す。
type JudgeEvaluateResult struct {
	Terminate bool   `json:"terminate"`
	Reason    string `json:"reason,omitempty"`
}
