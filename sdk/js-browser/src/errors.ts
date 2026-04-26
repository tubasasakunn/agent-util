/**
 * Error hierarchy for the browser SDK.
 *
 * Mirrors the Node SDK's `errors.ts` so that `instanceof AgentError`
 * works the same way across both packages.
 */

export class AgentError extends Error {
  public readonly code: number | null;
  public readonly data: unknown;

  constructor(message: string, opts: { code?: number | null; data?: unknown } = {}) {
    super(message);
    this.name = 'AgentError';
    this.code = opts.code ?? null;
    this.data = opts.data;
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

/**
 * Raised when a guard returns `deny` or `tripwire`. `tripwire` halts the loop
 * and surfaces as a thrown error; `deny` is converted into a soft response by
 * the engine but the same class is used for both inside the engine internals.
 */
export class GuardDenied extends AgentError {
  public readonly decision: string;
  public readonly reason: string;
  public readonly stage: string;

  constructor(
    message: string,
    opts: {
      decision?: string;
      reason?: string;
      stage?: string;
      code?: number | null;
      data?: unknown;
    } = {},
  ) {
    super(message, { code: opts.code ?? null, data: opts.data });
    this.name = 'GuardDenied';
    this.decision = opts.decision ?? 'deny';
    this.reason = opts.reason ?? '';
    this.stage = opts.stage ?? '';
  }
}

/** Raised when the LLM backend has not been loaded yet. */
export class LLMNotLoaded extends AgentError {
  constructor(message = 'LLM backend has not finished loading') {
    super(message);
    this.name = 'LLMNotLoaded';
  }
}
