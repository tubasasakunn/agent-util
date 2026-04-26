/**
 * Built-in verifiers, mirroring `internal/engine/builtin/verifiers.go`.
 *
 *  - `non_empty`  : fails when the trimmed result is empty
 *  - `json_valid` : when the trimmed result starts with `{` or `[`, requires
 *                   it to be valid JSON; otherwise skips
 */

import { type VerifierDefinition, verifier } from '../guards.js';

export function nonEmptyVerifier(): VerifierDefinition {
  return verifier('non_empty', (_t, _a, result) => {
    if (!result || !result.trim()) {
      return { passed: false, summary: 'tool result is empty' };
    }
    return { passed: true, summary: 'result is non-empty' };
  });
}

export function jsonValidVerifier(): VerifierDefinition {
  return verifier('json_valid', (_t, _a, result) => {
    const trimmed = (result ?? '').trim();
    if (!trimmed) return { passed: true, summary: 'empty, skipped' };
    const first = trimmed[0];
    if (first !== '{' && first !== '[') {
      return { passed: true, summary: 'not JSON-shaped, skipped' };
    }
    try {
      JSON.parse(trimmed);
      return { passed: true, summary: 'valid JSON' };
    } catch (err) {
      return {
        passed: false,
        summary: `result is not valid JSON: ${(err as Error).message}`,
      };
    }
  });
}

export const builtinVerifierFactories: Record<string, () => VerifierDefinition> = {
  non_empty: nonEmptyVerifier,
  json_valid: jsonValidVerifier,
};
