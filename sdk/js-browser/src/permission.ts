/**
 * Minimal permission pipeline for the browser.
 *
 * Pipeline order (matches `internal/engine/permission.go`):
 *
 *   1. Deny rule match -> `denied` (early exit, fail-closed)
 *   2. Allow rule match -> `allowed`
 *   3. Tool is read-only -> `allowed` (auto-approve)
 *   4. Otherwise -> `denied` (no `ask` UI in the browser SDK)
 *
 * `'*'` is supported as a wildcard tool name in both lists.
 *
 * Browsers have no good universal interactive-approval surface, so the SDK
 * skips the `ask` step and falls through to fail-closed. Callers who want a
 * confirmation dialog can implement it themselves inside a `tool_call`
 * guard.
 */

export type PermissionDecision = 'allowed' | 'denied';

export interface PermissionPolicy {
  enabled: boolean;
  deny: ReadonlyArray<string>;
  allow: ReadonlyArray<string>;
}

export interface PermissionEvent {
  toolName: string;
  decision: PermissionDecision;
  reason: string;
  isReadOnly: boolean;
}

export class PermissionChecker {
  constructor(
    private readonly policy: PermissionPolicy,
    private readonly onEvent?: (e: PermissionEvent) => void,
  ) {}

  /**
   * Decide whether `toolName` may be executed. When the policy is disabled
   * everything is allowed (matches the Go core's nil checker behaviour).
   */
  check(toolName: string, isReadOnly: boolean): PermissionDecision {
    if (!this.policy.enabled) {
      return this.emit(toolName, 'allowed', 'permission disabled', isReadOnly);
    }
    for (const rule of this.policy.deny) {
      if (rule === toolName || rule === '*') {
        return this.emit(toolName, 'denied', `deny rule matched (${rule})`, isReadOnly);
      }
    }
    for (const rule of this.policy.allow) {
      if (rule === toolName || rule === '*') {
        return this.emit(toolName, 'allowed', `allow rule matched (${rule})`, isReadOnly);
      }
    }
    if (isReadOnly) {
      return this.emit(toolName, 'allowed', 'read_only auto-approve', isReadOnly);
    }
    return this.emit(toolName, 'denied', 'fail-closed (no allow rule)', isReadOnly);
  }

  private emit(
    toolName: string,
    decision: PermissionDecision,
    reason: string,
    isReadOnly: boolean,
  ): PermissionDecision {
    if (this.onEvent) {
      try {
        this.onEvent({ toolName, decision, reason, isReadOnly });
      } catch {
        /* swallow */
      }
    }
    return decision;
  }
}
