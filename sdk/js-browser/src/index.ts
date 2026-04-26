/**
 * `@ai-agent/browser` — pure-TypeScript browser SDK for ai-agent.
 *
 * Runs the full agent loop (router -> tools -> guards -> verifiers -> output)
 * in the browser, against any LLM backend you plug in (typically WebLLM).
 *
 * See `README.md` for Quickstart and the README's "Differences vs Node SDK"
 * section for the features that aren't supported in-browser
 * (delegate / coordinator / MCP / LLM-summarizer compaction).
 */

export {
  Agent,
  type AgentOptions,
  type AgentResult,
  type RunOptions,
  type StreamEvent,
  type UsageInfo,
  type LoopEvent,
  type LoopEventHandler,
} from './agent.js';

export {
  tool,
  type ToolDefinition,
  type ToolHandler,
  type ToolOptions,
  type ToolReturn,
  type ToolExecuteResult,
  formatToolsForPrompt,
} from './tool.js';

export {
  inputGuard,
  toolCallGuard,
  outputGuard,
  verifier,
  STAGE_INPUT,
  STAGE_TOOL_CALL,
  STAGE_OUTPUT,
  type GuardDefinition,
  type GuardResult,
  type GuardDecision,
  type GuardStage,
  type InputGuardFn,
  type ToolCallGuardFn,
  type OutputGuardFn,
  type VerifierDefinition,
  type VerifierResult,
  type VerifierFn,
} from './guards.js';

export type {
  AgentConfig,
  GuardsConfig,
  PermissionConfig,
  VerifyConfig,
  CompactionConfig,
  StreamingConfig,
  ReminderConfig,
  DelegateConfig,
  CoordinatorConfig,
  ToolScopeConfig,
} from './config.js';
export { configToParams, stripUndefined } from './config.js';

export {
  AgentError,
  AgentBusy,
  AgentAborted,
  ToolError,
  GuardDenied,
  LLMNotLoaded,
} from './errors.js';

export { fixJson } from './engine/jsonfix.js';
export { parseRouterResponse } from './engine/router.js';
export {
  History,
  estimateTextTokens,
  estimateMessageTokens,
  userMessage,
  systemMessage,
  assistantMessage,
  toolResultMessage,
  toolCallMessage,
} from './engine/history.js';

export {
  promptInjectionGuard,
  maxLengthGuard,
  dangerousShellGuard,
  secretLeakGuard,
  nonEmptyVerifier,
  jsonValidVerifier,
  builtinGuardFactories,
  builtinVerifierFactories,
} from './builtins/index.js';

export type {
  Completer,
  ChatMessage,
  ChatRequest,
  ChatResponse,
  ChatChoice,
  Role,
  ResponseFormat,
  Usage,
} from './llm/completer.js';

export const VERSION = '0.1.0';
