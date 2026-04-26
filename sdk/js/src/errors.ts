/**
 * Exception types for the ai-agent JS SDK.
 *
 * The SDK wraps low-level transport / RPC errors into a small hierarchy so
 * that user code can use `instanceof AgentError` for "anything from the SDK"
 * and still match more precise subclasses for known JSON-RPC error codes.
 *
 * Mapping to the JSON-RPC errors defined in `pkg/protocol/errors.go`:
 *
 * - `-32700` Parse error           -> AgentError
 * - `-32600` Invalid request       -> AgentError
 * - `-32601` Method not found      -> AgentError
 * - `-32602` Invalid params        -> AgentError
 * - `-32603` Internal error        -> AgentError
 * - `-32000` Tool not found        -> ToolError
 * - `-32001` Tool execution failed -> ToolError
 * - `-32002` Agent already running -> AgentBusy
 * - `-32003` Aborted               -> AgentAborted
 * - `-32004` Message too large     -> AgentError
 *
 * Guard "deny"/"tripwire" decisions surface as `GuardDenied`.
 */

export class AgentError extends Error {
  public readonly code: number | null;
  public readonly data: unknown;

  constructor(message: string, opts: { code?: number | null; data?: unknown } = {}) {
    super(message);
    this.name = 'AgentError';
    this.code = opts.code ?? null;
    this.data = opts.data;
    // Restore prototype chain for instanceof to work across realms.
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class AgentBusy extends AgentError {
  constructor(message: string, opts: { code?: number | null; data?: unknown } = {}) {
    super(message, opts);
    this.name = 'AgentBusy';
  }
}

export class AgentAborted extends AgentError {
  constructor(message: string, opts: { code?: number | null; data?: unknown } = {}) {
    super(message, opts);
    this.name = 'AgentAborted';
  }
}

export class ToolError extends AgentError {
  constructor(message: string, opts: { code?: number | null; data?: unknown } = {}) {
    super(message, opts);
    this.name = 'ToolError';
  }
}

export class GuardDenied extends AgentError {
  public readonly decision: string;
  public readonly reason: string;

  constructor(
    message: string,
    opts: {
      decision?: string;
      reason?: string;
      code?: number | null;
      data?: unknown;
    } = {},
  ) {
    super(message, { code: opts.code ?? null, data: opts.data });
    this.name = 'GuardDenied';
    this.decision = opts.decision ?? 'deny';
    this.reason = opts.reason ?? '';
  }
}

const CODE_TO_CLASS: Record<number, new (msg: string, opts?: { code?: number; data?: unknown }) => AgentError> = {
  [-32000]: ToolError,
  [-32001]: ToolError,
  [-32002]: AgentBusy,
  [-32003]: AgentAborted,
};

/** Convert a JSON-RPC error tuple into the most specific SDK exception. */
export function fromRpcError(code: number, message: string, data?: unknown): AgentError {
  const Cls = CODE_TO_CLASS[code] ?? AgentError;
  return new Cls(message, { code, data });
}
