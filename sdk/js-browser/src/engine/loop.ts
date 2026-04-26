/**
 * Main agent loop.
 *
 * Per-turn flow (matches `internal/engine/engine.go`'s shape, minus
 * delegate/coordinator/MCP):
 *
 *   1. (turn 0 only) input guards on the raw prompt
 *   2. Maybe compact history if over the threshold
 *   3. Router step -> `{ tool, arguments, reasoning }`
 *   4. If tool === "none" -> chat step -> output guards -> Terminal
 *   5. Else: permission check -> tool_call guards -> tool execute ->
 *      verifiers -> Continue
 *   6. Stop if `max_turns` reached, `max_consecutive_failures` reached, or
 *      a tripwire fires.
 *
 * The loop emits structured events via the `onEvent` callback so demo UIs
 * can render router decisions, tool calls, guard verdicts in real time.
 */

import type {
  ChatMessage,
  ChatRequest,
  Completer,
  StreamEvent,
} from '../llm/completer.js';
import { type GuardDefinition, type GuardResult, type VerifierDefinition } from '../guards.js';
import {
  type ToolDefinition,
  coerceToolResult,
  type ToolExecuteResult,
} from '../tool.js';
import { type PermissionPolicy, PermissionChecker } from '../permission.js';
import {
  History,
  assistantMessage,
  toolCallMessage,
  toolResultMessage,
  userMessage,
} from './history.js';
import { buildChatSystemPrompt, buildRouterSystemPrompt } from './prompt.js';
import { routerStep, type RouterDecision, RouterError } from './router.js';
import { AgentAborted, AgentError, GuardDenied } from '../errors.js';

export interface UsageInfo {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

export interface AgentResult {
  response: string;
  reason: string;
  turns: number;
  usage: UsageInfo;
}

export type LoopEvent =
  | { kind: 'turn_start'; turn: number }
  | { kind: 'router'; turn: number; decision: RouterDecision }
  | { kind: 'router_error'; turn: number; error: string; raw?: string }
  | {
      kind: 'tool_call';
      turn: number;
      name: string;
      args: Record<string, unknown>;
    }
  | {
      kind: 'tool_result';
      turn: number;
      name: string;
      result: ToolExecuteResult;
    }
  | {
      kind: 'guard';
      turn: number;
      stage: 'input' | 'tool_call' | 'output';
      name: string;
      result: GuardResult;
    }
  | {
      kind: 'permission';
      turn: number;
      tool: string;
      decision: 'allowed' | 'denied';
      reason: string;
    }
  | {
      kind: 'verify';
      turn: number;
      tool: string;
      passed: boolean;
      summary: string;
    }
  | { kind: 'delta'; turn: number; text: string }
  | { kind: 'end'; result: AgentResult };

export type LoopEventHandler = (e: LoopEvent) => void | Promise<void>;

export interface LoopConfig {
  llm: Completer;
  tools: ReadonlyArray<ToolDefinition>;
  guards: {
    input: ReadonlyArray<GuardDefinition>;
    toolCall: ReadonlyArray<GuardDefinition>;
    output: ReadonlyArray<GuardDefinition>;
  };
  verifiers: ReadonlyArray<VerifierDefinition>;
  permission: PermissionPolicy;
  systemPrompt: string;
  maxTurns: number;
  tokenLimit: number;
  maxConsecutiveFailures: number;
  streaming: boolean;
  signal?: AbortSignal;
  onEvent?: LoopEventHandler;
  /** Optional default temperature forwarded to chat / router calls. */
  temperature?: number;
}

const FAILURE_REASONS = new Set([
  'tool_error',
  'tool_not_found',
  'verify_failed',
  'permission_denied',
  'guard_blocked',
]);

let callIdCounter = 0;
function generateCallId(): string {
  callIdCounter = (callIdCounter + 1) & 0xffff;
  return `call_${Date.now().toString(36)}_${callIdCounter.toString(36)}`;
}

export class AgentLoop {
  private readonly history: History;
  private readonly perms: PermissionChecker;
  private totalUsage: UsageInfo = {
    prompt_tokens: 0,
    completion_tokens: 0,
    total_tokens: 0,
  };

  constructor(private readonly cfg: LoopConfig) {
    this.history = new History(cfg.tokenLimit);
    this.perms = new PermissionChecker(cfg.permission, (e) =>
      this.emit({
        kind: 'permission',
        turn: this.currentTurn,
        tool: e.toolName,
        decision: e.decision,
        reason: e.reason,
      }),
    );
  }

  /** Append a previously-generated message (e.g. for multi-turn sessions). */
  addMessage(msg: ChatMessage): void {
    this.history.add(msg);
  }

  messages(): ChatMessage[] {
    return this.history.messages();
  }

  usageRatio(): number {
    return this.history.usageRatio();
  }

  private currentTurn = 0;

  async run(prompt: string): Promise<AgentResult> {
    this.checkAbort();

    // 1. Input guard (once, on the raw prompt)
    for (const g of this.cfg.guards.input) {
      const r = await g.call({ input: prompt });
      await this.emit({
        kind: 'guard',
        turn: 0,
        stage: 'input',
        name: g.name,
        result: r,
      });
      if (r.decision === 'tripwire') {
        throw new GuardDenied(`input tripwire: ${r.reason ?? ''}`, {
          decision: 'tripwire',
          reason: r.reason ?? '',
          stage: 'input',
        });
      }
      if (r.decision === 'deny') {
        const result: AgentResult = {
          response: `Input rejected: ${r.reason ?? g.name}`,
          reason: 'input_denied',
          turns: 0,
          usage: this.totalUsage,
        };
        await this.emit({ kind: 'end', result });
        return result;
      }
    }

    this.history.add(userMessage(prompt));

    let consecutiveFailures = 0;

    for (let turn = 0; turn < this.cfg.maxTurns; turn++) {
      this.checkAbort();
      this.currentTurn = turn + 1;
      await this.emit({ kind: 'turn_start', turn: turn + 1 });

      const stepResult = await this.step(turn + 1);

      if (stepResult.terminal) {
        const result: AgentResult = {
          response: stepResult.response ?? '',
          reason: stepResult.reason,
          turns: turn + 1,
          usage: this.totalUsage,
        };
        await this.emit({ kind: 'end', result });
        return result;
      }

      if (FAILURE_REASONS.has(stepResult.reason)) {
        consecutiveFailures++;
        if (consecutiveFailures >= this.cfg.maxConsecutiveFailures) {
          const result: AgentResult = {
            response: `Stopped: ${consecutiveFailures} consecutive failures`,
            reason: 'max_consecutive_failures',
            turns: turn + 1,
            usage: this.totalUsage,
          };
          await this.emit({ kind: 'end', result });
          return result;
        }
      } else {
        consecutiveFailures = 0;
      }
    }

    const result: AgentResult = {
      response: '',
      reason: 'max_turns',
      turns: this.cfg.maxTurns,
      usage: this.totalUsage,
    };
    await this.emit({ kind: 'end', result });
    return result;
  }

  // --------------------------------------------------------------------
  // Single step
  // --------------------------------------------------------------------

  private async step(turn: number): Promise<{
    terminal: boolean;
    reason: string;
    response?: string;
  }> {
    const tools = this.cfg.tools;
    if (tools.length === 0) {
      // No tools registered -> straight to chat.
      const text = await this.chatStep(turn);
      return { terminal: true, reason: 'completed', response: text };
    }

    let decision: RouterDecision;
    try {
      decision = await routerStep(
        this.cfg.llm,
        buildRouterSystemPrompt({
          systemPrompt: this.cfg.systemPrompt,
          tools,
        }),
        this.history.messages(),
        {
          toolNames: tools.map((t) => t.name),
          ...(this.cfg.temperature !== undefined ? { temperature: this.cfg.temperature } : {}),
        },
      );
    } catch (err) {
      const re = err as RouterError;
      await this.emit({
        kind: 'router_error',
        turn,
        error: re.message,
        ...(re.raw !== undefined ? { raw: re.raw } : {}),
      });
      // Treat as "go straight to chat" rather than fail the run.
      const text = await this.chatStep(turn);
      return { terminal: true, reason: 'completed', response: text };
    }

    await this.emit({ kind: 'router', turn, decision });

    if (decision.tool === 'none' || decision.tool === '') {
      const text = await this.chatStep(turn);
      return { terminal: true, reason: 'completed', response: text };
    }

    return await this.executeTool(turn, decision);
  }

  private async executeTool(
    turn: number,
    decision: RouterDecision,
  ): Promise<{ terminal: boolean; reason: string; response?: string }> {
    const t = this.cfg.tools.find((x) => x.name === decision.tool);
    if (!t) {
      const callId = generateCallId();
      this.history.add(toolCallMessage(callId, decision.tool, decision.arguments));
      const errMsg = `Error: tool "${decision.tool}" not found. Available tools: ${this.cfg.tools.map((x) => x.name).join(', ')}`;
      this.history.add(toolResultMessage(callId, errMsg));
      await this.emit({
        kind: 'tool_result',
        turn,
        name: decision.tool,
        result: { content: errMsg, is_error: true },
      });
      return { terminal: false, reason: 'tool_not_found' };
    }

    // Permission
    const permResult = this.perms.check(t.name, t.readOnly);
    if (permResult === 'denied') {
      const callId = generateCallId();
      this.history.add(toolCallMessage(callId, t.name, decision.arguments));
      const msg = `Permission denied: tool "${t.name}" is not allowed by the current policy.`;
      this.history.add(toolResultMessage(callId, msg));
      await this.emit({
        kind: 'tool_result',
        turn,
        name: t.name,
        result: { content: msg, is_error: true },
      });
      return { terminal: false, reason: 'permission_denied' };
    }

    // Tool-call guards
    for (const g of this.cfg.guards.toolCall) {
      const r = await g.call({ toolName: t.name, args: decision.arguments });
      await this.emit({
        kind: 'guard',
        turn,
        stage: 'tool_call',
        name: g.name,
        result: r,
      });
      if (r.decision === 'tripwire') {
        throw new GuardDenied(`tool_call tripwire: ${r.reason ?? ''}`, {
          decision: 'tripwire',
          reason: r.reason ?? '',
          stage: 'tool_call',
        });
      }
      if (r.decision === 'deny') {
        const callId = generateCallId();
        this.history.add(toolCallMessage(callId, t.name, decision.arguments));
        const msg = `Blocked by guard: ${r.reason ?? g.name}`;
        this.history.add(toolResultMessage(callId, msg));
        await this.emit({
          kind: 'tool_result',
          turn,
          name: t.name,
          result: { content: msg, is_error: true },
        });
        return { terminal: false, reason: 'guard_blocked' };
      }
    }

    // Execute
    await this.emit({
      kind: 'tool_call',
      turn,
      name: t.name,
      args: decision.arguments,
    });

    const callId = generateCallId();
    this.history.add(toolCallMessage(callId, t.name, decision.arguments));

    let result: ToolExecuteResult;
    try {
      const raw = await t.handler(decision.arguments);
      result = coerceToolResult(raw);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      result = {
        content: `Error executing tool "${t.name}": ${message}`,
        is_error: true,
      };
    }

    this.history.add(toolResultMessage(callId, result.content));
    await this.emit({ kind: 'tool_result', turn, name: t.name, result });

    if (result.is_error) {
      return { terminal: false, reason: 'tool_error' };
    }

    // Verifiers
    for (const v of this.cfg.verifiers) {
      const vr = await v.call({
        toolName: t.name,
        args: decision.arguments,
        result: result.content,
      });
      await this.emit({
        kind: 'verify',
        turn,
        tool: t.name,
        passed: !!vr.passed,
        summary: vr.summary ?? '',
      });
      if (!vr.passed) {
        const verifyMsg = `[Verification Failed]\n${vr.summary ?? ''}\nPlease fix the issues and try again.`;
        this.history.add(userMessage(verifyMsg));
        return { terminal: false, reason: 'verify_failed' };
      }
    }

    return { terminal: false, reason: 'tool_use' };
  }

  // --------------------------------------------------------------------
  // Chat step (final response)
  // --------------------------------------------------------------------

  private async chatStep(turn: number): Promise<string> {
    const messages: ChatMessage[] = [];
    const sys = buildChatSystemPrompt({
      systemPrompt: this.cfg.systemPrompt,
      tools: this.cfg.tools,
    });
    if (sys) messages.push({ role: 'system', content: sys });
    for (const m of this.history.messages()) messages.push(m);

    const req: ChatRequest = {
      messages,
      ...(this.cfg.temperature !== undefined ? { temperature: this.cfg.temperature } : {}),
    };

    let text = '';
    if (
      this.cfg.streaming &&
      typeof this.cfg.llm.chatCompletionStream === 'function'
    ) {
      const stream = this.cfg.llm.chatCompletionStream!(req);
      for await (const chunk of stream as AsyncIterable<StreamEvent>) {
        if (chunk.delta) {
          text += chunk.delta;
          await this.emit({ kind: 'delta', turn, text: chunk.delta });
        }
      }
    } else {
      const resp = await this.cfg.llm.chatCompletion(req);
      text = resp.choices?.[0]?.message?.content ?? '';
      if (resp.usage) {
        this.totalUsage.prompt_tokens += resp.usage.prompt_tokens ?? 0;
        this.totalUsage.completion_tokens += resp.usage.completion_tokens ?? 0;
        this.totalUsage.total_tokens += resp.usage.total_tokens ?? 0;
      }
    }

    // Output guards
    for (const g of this.cfg.guards.output) {
      const r = await g.call({ output: text });
      await this.emit({
        kind: 'guard',
        turn,
        stage: 'output',
        name: g.name,
        result: r,
      });
      if (r.decision === 'tripwire') {
        throw new GuardDenied(`output tripwire: ${r.reason ?? ''}`, {
          decision: 'tripwire',
          reason: r.reason ?? '',
          stage: 'output',
        });
      }
      if (r.decision === 'deny') {
        const safe = 'I cannot provide that response.';
        this.history.add(assistantMessage(safe));
        return safe;
      }
    }

    this.history.add(assistantMessage(text));
    return text;
  }

  // --------------------------------------------------------------------
  // Helpers
  // --------------------------------------------------------------------

  private async emit(ev: LoopEvent): Promise<void> {
    if (!this.cfg.onEvent) return;
    try {
      await this.cfg.onEvent(ev);
    } catch (err) {
      // Don't let listener crashes kill the loop.
      // eslint-disable-next-line no-console
      console.error('[agent] event handler threw:', err);
    }
  }

  private checkAbort(): void {
    if (this.cfg.signal?.aborted) {
      throw new AgentAborted('agent run aborted', {});
    }
  }
}

export { AgentError };
