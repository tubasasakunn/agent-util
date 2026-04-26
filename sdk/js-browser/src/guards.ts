/**
 * Guard / verifier helpers — same shape as the Node SDK so user code is
 * portable.
 *
 * Three stages, evaluated by the engine in this order:
 *
 *  - `input`     — runs once, on the raw user prompt, before the loop starts
 *  - `tool_call` — runs before each tool invocation, sees `(toolName, args)`
 *  - `output`    — runs once, on the final assistant message
 *
 * A guard returns `{ decision: 'allow' | 'deny' | 'tripwire', reason? }`.
 * `tripwire` immediately aborts the run with a thrown {@link GuardDenied}.
 * `deny` is converted by the engine into a soft refusal response.
 */

export const STAGE_INPUT = 'input' as const;
export const STAGE_TOOL_CALL = 'tool_call' as const;
export const STAGE_OUTPUT = 'output' as const;

export type GuardStage =
  | typeof STAGE_INPUT
  | typeof STAGE_TOOL_CALL
  | typeof STAGE_OUTPUT;

export type GuardDecision = 'allow' | 'deny' | 'tripwire';

const VALID_DECISIONS: ReadonlySet<GuardDecision> = new Set([
  'allow',
  'deny',
  'tripwire',
]);

export interface GuardResult {
  decision: GuardDecision;
  reason?: string;
}

export type InputGuardFn = (input: string) => GuardResult | Promise<GuardResult>;
export type ToolCallGuardFn = (
  toolName: string,
  args: Record<string, unknown>,
) => GuardResult | Promise<GuardResult>;
export type OutputGuardFn = (output: string) => GuardResult | Promise<GuardResult>;

export interface GuardDefinition {
  readonly name: string;
  readonly stage: GuardStage;
  readonly call: (params: {
    input?: string;
    toolName?: string;
    args?: Record<string, unknown>;
    output?: string;
  }) => Promise<GuardResult>;
}

function normalise(result: GuardResult): GuardResult {
  if (!VALID_DECISIONS.has(result.decision)) {
    return {
      decision: 'deny',
      reason: `invalid guard decision ${JSON.stringify(result.decision)}: ${result.reason ?? ''}`,
    };
  }
  return { decision: result.decision, reason: result.reason ?? '' };
}

export function inputGuard(name: string, fn: InputGuardFn): GuardDefinition {
  return {
    name,
    stage: STAGE_INPUT,
    call: async ({ input = '' }) => normalise(await fn(input)),
  };
}

export function toolCallGuard(name: string, fn: ToolCallGuardFn): GuardDefinition {
  return {
    name,
    stage: STAGE_TOOL_CALL,
    call: async ({ toolName = '', args = {} }) => normalise(await fn(toolName, args)),
  };
}

export function outputGuard(name: string, fn: OutputGuardFn): GuardDefinition {
  return {
    name,
    stage: STAGE_OUTPUT,
    call: async ({ output = '' }) => normalise(await fn(output)),
  };
}

// ---------------------------------------------------------------------------
// Verifiers
// ---------------------------------------------------------------------------

export interface VerifierResult {
  passed: boolean;
  summary?: string;
}

export type VerifierFn = (
  toolName: string,
  args: Record<string, unknown>,
  result: string,
) => VerifierResult | Promise<VerifierResult>;

export interface VerifierDefinition {
  readonly name: string;
  readonly call: (params: {
    toolName: string;
    args: Record<string, unknown>;
    result: string;
  }) => Promise<VerifierResult>;
}

export function verifier(name: string, fn: VerifierFn): VerifierDefinition {
  return {
    name,
    call: async ({ toolName, args, result }) => {
      const out = await fn(toolName, args, result);
      return { passed: !!out.passed, summary: out.summary ?? '' };
    },
  };
}
