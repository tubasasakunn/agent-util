/**
 * Configuration types for `Agent.configure`.
 *
 * Field names use `snake_case` to match the Node SDK / Go core protocol so
 * users can copy-paste configs across SDKs. Fields whose value is `undefined`
 * are stripped before being applied (mimicking Go's `omitempty`).
 *
 * Most options are honoured (max_turns, system_prompt, token_limit,
 * permission, guards, verify, streaming, reminder, compaction). Some fields
 * that only make sense in the Node/Go subprocess world (delegate,
 * coordinator, work_dir, tool_scope) are accepted but currently ignored —
 * `Agent.configure` reports which keys were applied so callers can detect
 * silent drops.
 */

export interface DelegateConfig {
  enabled?: boolean;
  max_chars?: number;
}

export interface CoordinatorConfig {
  enabled?: boolean;
  max_chars?: number;
}

export interface CompactionConfig {
  enabled?: boolean;
  budget_max_chars?: number;
  keep_last?: number;
  target_ratio?: number;
  /** "" or "llm". The browser SDK only implements stages 1-3 ("" / non-LLM). */
  summarizer?: string;
}

export interface PermissionConfig {
  enabled?: boolean;
  deny?: string[];
  allow?: string[];
}

export interface GuardsConfig {
  input?: string[];
  tool_call?: string[];
  output?: string[];
}

export interface VerifyConfig {
  verifiers?: string[];
  max_step_retries?: number;
  max_consecutive_failures?: number;
}

export interface ToolScopeConfig {
  max_tools?: number;
  include_always?: string[];
}

export interface ReminderConfig {
  threshold?: number;
  content?: string;
}

export interface StreamingConfig {
  enabled?: boolean;
  context_status?: boolean;
}

export interface AgentConfig {
  max_turns?: number;
  system_prompt?: string;
  token_limit?: number;
  /** Accepted for cross-SDK compatibility but not used in the browser. */
  work_dir?: string;
  /** Accepted but ignored — the browser engine has no subagent. */
  delegate?: DelegateConfig;
  /** Accepted but ignored — the browser engine has no coordinator. */
  coordinator?: CoordinatorConfig;
  compaction?: CompactionConfig;
  permission?: PermissionConfig;
  guards?: GuardsConfig;
  verify?: VerifyConfig;
  tool_scope?: ToolScopeConfig;
  reminder?: ReminderConfig;
  streaming?: StreamingConfig;
}

/** Recursively drop `undefined` values. `null` values are preserved. */
export function stripUndefined(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((v) => (v === undefined ? null : stripUndefined(v)));
  }
  if (value !== null && typeof value === 'object') {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      if (v === undefined) continue;
      out[k] = stripUndefined(v);
    }
    return out;
  }
  return value;
}

/** Same shape as the Node SDK helper; provided for symmetry. */
export function configToParams(config: AgentConfig): Record<string, unknown> {
  return stripUndefined(config) as Record<string, unknown>;
}
