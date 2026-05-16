/**
 * `@ai-agent/sdk` — TypeScript SDK for the ai-agent Go harness.
 *
 * Thin JSON-RPC client over stdio. See `README.md` for Quickstart.
 */

export {
  Agent,
  type AgentOptions,
  type AgentResult,
  type UsageInfo,
  type RunOptions,
  type MCPOptions,
  type StreamCallback,
  type StatusCallback,
  type StreamEvent,
  type LLMHandler,
} from './client.js';

export {
  tool,
  type ToolDefinition,
  type ToolHandler,
  type ToolOptions,
  type ToolReturn,
  type ToolExecuteResult,
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
} from './guard.js';

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
  LLMConfig,
} from './config.js';

export { configToParams, stripUndefined } from './config.js';

export {
  AgentError,
  AgentBusy,
  AgentAborted,
  ToolError,
  GuardDenied,
} from './errors.js';

export { JsonRpcClient, nodeStreamsTransport } from './jsonrpc.js';
export type {
  JsonRpcTransport,
  JsonRpcMessage,
  JsonRpcId,
  NotificationHandler,
  RequestHandler,
} from './jsonrpc.js';

export const VERSION = '0.1.0';
