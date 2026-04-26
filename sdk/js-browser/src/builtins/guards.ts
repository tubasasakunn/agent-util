/**
 * Built-in guards. Behaviour matches `internal/engine/builtin/guards.go`
 * regex-for-regex (case-insensitive where the Go version is).
 *
 *  - `prompt_injection`  : input  — flags ignore/disregard/system: patterns
 *  - `max_length`        : input  — denies when the input exceeds 50000 chars
 *                                   (or the explicit limit you pass in)
 *  - `dangerous_shell`   : tool   — denies destructive shell commands
 *  - `secret_leak`       : output — denies obvious API key formats
 */

import {
  inputGuard,
  outputGuard,
  toolCallGuard,
  type GuardDefinition,
  type GuardResult,
} from '../guards.js';

const INJECTION_PATTERNS: ReadonlyArray<RegExp> = [
  /ignore\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?|context)/i,
  /disregard\s+(\w+\s+)?(previous|prior|above)/i,
  /you\s+are\s+now\s+a\s+/i,
  /^\s*system\s*[:：]\s*/i,
  /reveal\s+(your\s+)?system\s+prompt/i,
];

export function promptInjectionGuard(): GuardDefinition {
  return inputGuard('prompt_injection', (input) => {
    for (const p of INJECTION_PATTERNS) {
      if (p.test(input)) {
        return {
          decision: 'deny',
          reason: 'potential prompt injection pattern detected',
        };
      }
    }
    return { decision: 'allow' };
  });
}

export function maxLengthGuard(max = 50000): GuardDefinition {
  return inputGuard('max_length', (input) => {
    if (max > 0 && input.length > max) {
      return {
        decision: 'deny',
        reason: `input exceeds max length (${input.length} > ${max})`,
      };
    }
    return { decision: 'allow' };
  });
}

const DANGEROUS_SHELL_PATTERNS: ReadonlyArray<RegExp> = [
  /rm\s+-rf?\s+(\/|~|\$HOME|\*)/,
  /:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}/, // fork bomb
  /\bmkfs\./,
  /\bdd\s+if=.*of=\/dev\//,
  /chmod\s+-R\s+777\s+\//,
  />\s*\/dev\/sd[a-z]/,
  /\bshutdown\b/,
  /\breboot\b/,
];

function looksLikeShellTool(name: string): boolean {
  const n = name.toLowerCase();
  return n === 'bash' || n === 'sh' || n.includes('shell') || n.includes('exec');
}

function pickStringField(
  args: Record<string, unknown>,
  ...keys: string[]
): string {
  for (const k of keys) {
    const v = args[k];
    if (typeof v === 'string') return v;
  }
  return '';
}

export function dangerousShellGuard(): GuardDefinition {
  return toolCallGuard('dangerous_shell', (toolName, args): GuardResult => {
    if (!looksLikeShellTool(toolName)) return { decision: 'allow' };
    const cmd = pickStringField(args, 'command', 'cmd', 'script');
    for (const p of DANGEROUS_SHELL_PATTERNS) {
      if (p.test(cmd)) {
        return { decision: 'deny', reason: 'dangerous shell pattern detected' };
      }
    }
    return { decision: 'allow' };
  });
}

const SECRET_PATTERNS: ReadonlyArray<RegExp> = [
  /sk-[A-Za-z0-9]{20,}/,
  /sk-ant-[A-Za-z0-9_\-]{20,}/,
  /AKIA[0-9A-Z]{16}/,
  /ghp_[A-Za-z0-9]{30,}/,
  /xox[baprs]-[A-Za-z0-9-]{10,}/,
  /-----BEGIN (RSA|OPENSSH|EC|PGP|DSA) PRIVATE KEY-----/,
];

export function secretLeakGuard(): GuardDefinition {
  return outputGuard('secret_leak', (output) => {
    for (const p of SECRET_PATTERNS) {
      if (p.test(output)) {
        return { decision: 'deny', reason: 'potential secret leak detected' };
      }
    }
    return { decision: 'allow' };
  });
}

/** Lookup table for `Agent.configure({ guards: { input: ['prompt_injection'] } })`. */
export const builtinGuardFactories: Record<string, () => GuardDefinition> = {
  prompt_injection: promptInjectionGuard,
  max_length: () => maxLengthGuard(),
  dangerous_shell: dangerousShellGuard,
  secret_leak: secretLeakGuard,
};
