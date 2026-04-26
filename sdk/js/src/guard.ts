/**
 * Guard / verifier helpers.
 *
 * A guard is a wrapper-side function the core invokes via `guard.execute` to
 * decide whether a prompt / tool call / output should be allowed, denied, or
 * should trip the safety wire (immediate stop).
 *
 * A verifier is a wrapper-side function the core invokes via `verifier.execute`
 * after a tool produced a result, returning `{passed, summary}`.
 *
 * Stage-specific signatures (sync or async — both are supported):
 *
 * ```ts
 * inputGuard('no_secrets', (input) => ({ decision: 'deny', reason: '...' }));
 * toolCallGuard('fs_root_only', (toolName, args) => ({ decision: 'allow' }));
 * outputGuard('pii_redactor', (output) => ({ decision: 'allow' }));
 * verifier('non_empty', (toolName, args, result) => ({ passed: true }));
 * ```
 */

export const STAGE_INPUT = 'input' as const;
export const STAGE_TOOL_CALL = 'tool_call' as const;
export const STAGE_OUTPUT = 'output' as const;

export type GuardStage = typeof STAGE_INPUT | typeof STAGE_TOOL_CALL | typeof STAGE_OUTPUT;

export type GuardDecision = 'allow' | 'deny' | 'tripwire';

const VALID_DECISIONS: ReadonlySet<GuardDecision> = new Set(['allow', 'deny', 'tripwire']);

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
  /** Wrapper-side handler dispatched from `guard.execute`. */
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

/** Build an input-stage guard. */
export function inputGuard(name: string, fn: InputGuardFn): GuardDefinition {
  return {
    name,
    stage: STAGE_INPUT,
    call: async ({ input = '' }) => normalise(await fn(input)),
  };
}

/** Build a tool-call-stage guard. */
export function toolCallGuard(name: string, fn: ToolCallGuardFn): GuardDefinition {
  return {
    name,
    stage: STAGE_TOOL_CALL,
    call: async ({ toolName = '', args = {} }) => normalise(await fn(toolName, args)),
  };
}

/** Build an output-stage guard. */
export function outputGuard(name: string, fn: OutputGuardFn): GuardDefinition {
  return {
    name,
    stage: STAGE_OUTPUT,
    call: async ({ output = '' }) => normalise(await fn(output)),
  };
}

/** Internal: project a guard into the wire format used by `guard.register`. */
export function guardToProtocolDict(def: GuardDefinition): Record<string, unknown> {
  return { name: def.name, stage: def.stage };
}

// ---------------------------------------------------------------------------
// Verifiers (also wrapper-side; live next to guards because they share shape)
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

/** Build a verifier definition. */
export function verifier(name: string, fn: VerifierFn): VerifierDefinition {
  return {
    name,
    call: async ({ toolName, args, result }) => {
      const out = await fn(toolName, args, result);
      return { passed: !!out.passed, summary: out.summary ?? '' };
    },
  };
}

/** Internal: project a verifier into the wire format. */
export function verifierToProtocolDict(def: VerifierDefinition): Record<string, unknown> {
  return { name: def.name };
}
