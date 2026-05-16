/**
 * Configuration types for `agent.configure`.
 *
 * Mirrors `pkg/protocol.AgentConfigureParams` and the nested config structs.
 * Field names are kept in `snake_case` so they pass straight through to the
 * Go core without translation, matching the Python SDK.
 *
 * JSON-Schema sources (docs/schemas/*.json):
 *
 * - `AgentConfigureParams.json`
 * - `DelegateConfig.json`
 * - `CoordinatorConfig.json`
 * - `CompactionConfig.json`
 * - `PermissionConfig.json`
 * - `GuardsConfig.json`
 * - `VerifyConfig.json`
 * - `ToolScopeConfig.json`
 * - `ReminderConfig.json`
 * - `StreamingConfig.json`
 *
 * `undefined` fields are stripped before serialisation so they behave like
 * Go's `omitempty` (the core keeps existing defaults).
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
  /** "" or "llm" */
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

/**
 * Main LLM driver config.
 *
 * `mode: 'remote'` forwards every ChatCompletion to the wrapper via the
 * `llm.execute` reverse RPC. Install the handler with
 * {@link Agent.setLLMHandler} before calling `configure`.
 */
export interface LLMConfig {
  mode?: 'http' | 'remote';
  timeout_seconds?: number;
}

export interface AgentConfig {
  max_turns?: number;
  system_prompt?: string;
  token_limit?: number;
  work_dir?: string;
  delegate?: DelegateConfig;
  coordinator?: CoordinatorConfig;
  compaction?: CompactionConfig;
  permission?: PermissionConfig;
  guards?: GuardsConfig;
  verify?: VerifyConfig;
  tool_scope?: ToolScopeConfig;
  reminder?: ReminderConfig;
  streaming?: StreamingConfig;
  llm?: LLMConfig;
}

/**
 * Recursively drop `undefined` values so the JSON output mimics Go's
 * `omitempty`. `null` values are kept as-is. Arrays are preserved
 * element-for-element (a `null` element stays a `null` element).
 */
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

/**
 * Convert an {@link AgentConfig} into the JSON-RPC params dict.
 *
 * Fields whose value is `undefined` are omitted entirely, matching how the
 * Go core treats absent fields (keep existing defaults). Nested config blocks
 * are recursively cleaned the same way.
 */
export function configToParams(config: AgentConfig): Record<string, unknown> {
  return stripUndefined(config) as Record<string, unknown>;
}
