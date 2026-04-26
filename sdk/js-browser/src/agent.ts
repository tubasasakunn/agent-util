/**
 * High-level {@link Agent} API for browser use.
 *
 * Mirrors the Node SDK's surface as closely as possible — same method names,
 * same argument order, same `snake_case` config keys — except that there is no
 * subprocess. The agent loop runs in-process and talks to whatever
 * {@link Completer} you plug in (typically `WebLLMCompleter`).
 *
 * ```ts
 * import { Agent, tool } from '@ai-agent/browser';
 * import { WebLLMCompleter } from '@ai-agent/browser/llm';
 *
 * const llm = new WebLLMCompleter({ model: 'gemma-2-2b-it-q4f16_1-MLC' });
 * await llm.load((p) => console.log(p));
 *
 * const agent = new Agent({ llm });
 * await agent.configure({ max_turns: 6, guards: { input: ['prompt_injection'] } });
 * agent.registerTools(myTool);
 *
 * const r = await agent.run('Hi');
 * console.log(r.response);
 * ```
 */

import {
  type AgentConfig,
  type GuardsConfig,
  type PermissionConfig,
  type StreamingConfig,
  type VerifyConfig,
  configToParams,
} from './config.js';
import {
  builtinGuardFactories,
  builtinVerifierFactories,
} from './builtins/index.js';
import {
  AgentLoop,
  type AgentResult,
  type LoopEvent,
  type LoopEventHandler,
  type UsageInfo,
} from './engine/loop.js';
import {
  type GuardDefinition,
  STAGE_INPUT,
  STAGE_TOOL_CALL,
  STAGE_OUTPUT,
  type VerifierDefinition,
} from './guards.js';
import type { Completer } from './llm/completer.js';
import { type ToolDefinition } from './tool.js';
import { AgentBusy, AgentError } from './errors.js';
import { type PermissionPolicy } from './permission.js';

export type { AgentResult, UsageInfo, LoopEvent, LoopEventHandler };

export interface AgentOptions {
  /** LLM backend implementing the {@link Completer} interface. */
  llm: Completer;
}

export interface RunOptions {
  /** Override the configured max_turns just for this run. */
  maxTurns?: number;
  /** Per-token streaming callback (chat step). */
  onDelta?: (text: string, turn: number) => void | Promise<void>;
  /** All low-level loop events (router, tool, guard, ...). */
  onEvent?: LoopEventHandler;
  /** Optional abort signal. */
  signal?: AbortSignal;
}

export type StreamEvent =
  | { kind: 'delta'; text: string; turn: number }
  | { kind: 'event'; event: LoopEvent }
  | { kind: 'end'; result: AgentResult };

const DEFAULT_MAX_TURNS = 8;
const DEFAULT_TOKEN_LIMIT = 8192;
const DEFAULT_MAX_CONSECUTIVE_FAILURES = 3;

export class Agent {
  private readonly llm: Completer;

  // Registries
  private readonly toolMap = new Map<string, ToolDefinition>();
  private readonly customGuards = new Map<string, GuardDefinition>();
  private readonly customVerifiers = new Map<string, VerifierDefinition>();

  // Configuration (mutated by `configure`).
  private maxTurns = DEFAULT_MAX_TURNS;
  private systemPrompt = '';
  private tokenLimit = DEFAULT_TOKEN_LIMIT;
  private maxConsecutiveFailures = DEFAULT_MAX_CONSECUTIVE_FAILURES;
  private minToolKinds = 0;
  private temperature: number | undefined;
  private streaming: StreamingConfig = { enabled: false };
  private permission: PermissionConfig = {
    enabled: false,
    allow: [],
    deny: [],
  };
  private guards: GuardsConfig = { input: [], tool_call: [], output: [] };
  private verifyCfg: VerifyConfig = { verifiers: [] };

  private runInProgress = false;

  constructor(opts: AgentOptions) {
    if (!opts?.llm) throw new TypeError('Agent({ llm }) is required');
    this.llm = opts.llm;
  }

  /** Underlying LLM — exposed for advanced callers. */
  get completer(): Completer {
    return this.llm;
  }

  // --- configuration ---------------------------------------------------

  /**
   * Apply an {@link AgentConfig}. Returns the list of keys that were applied
   * (matches the Node SDK semantics so callers can diff what got dropped).
   *
   * Keys that the browser SDK currently ignores (because the underlying
   * feature isn't supported in-browser): `delegate`, `coordinator`,
   * `work_dir`, `tool_scope`, `reminder`, `compaction.summarizer === 'llm'`.
   * They are silently no-ops and not reported in the result.
   */
  async configure(config: AgentConfig): Promise<string[]> {
    const params = configToParams(config) as Record<string, unknown>;
    const applied: string[] = [];

    if (typeof params.max_turns === 'number') {
      this.maxTurns = Math.max(1, Math.floor(params.max_turns));
      applied.push('max_turns');
    }
    if (typeof params.system_prompt === 'string') {
      this.systemPrompt = params.system_prompt;
      applied.push('system_prompt');
    }
    if (typeof params.token_limit === 'number') {
      this.tokenLimit = Math.max(256, Math.floor(params.token_limit));
      applied.push('token_limit');
    }
    if (typeof params.min_tool_kinds === 'number') {
      this.minToolKinds = Math.max(0, Math.floor(params.min_tool_kinds));
      applied.push('min_tool_kinds');
    }
    if (params.streaming && typeof params.streaming === 'object') {
      this.streaming = { ...(params.streaming as StreamingConfig) };
      applied.push('streaming');
    }
    if (params.permission && typeof params.permission === 'object') {
      this.permission = { ...(params.permission as PermissionConfig) };
      applied.push('permission');
    }
    if (params.guards && typeof params.guards === 'object') {
      this.guards = { ...(params.guards as GuardsConfig) };
      applied.push('guards');
    }
    if (params.verify && typeof params.verify === 'object') {
      this.verifyCfg = { ...(params.verify as VerifyConfig) };
      if (typeof this.verifyCfg.max_consecutive_failures === 'number') {
        this.maxConsecutiveFailures = Math.max(
          1,
          Math.floor(this.verifyCfg.max_consecutive_failures),
        );
      }
      applied.push('verify');
    }

    return applied;
  }

  /**
   * Set or override the chat temperature used for both router and chat steps.
   * Pass `undefined` to clear and let the backend's default apply.
   */
  setTemperature(t: number | undefined): void {
    this.temperature = t;
  }

  // --- registration ----------------------------------------------------

  registerTools(...tools: ToolDefinition[]): number {
    for (const t of tools) this.toolMap.set(t.name, t);
    return this.toolMap.size;
  }

  unregisterTool(name: string): boolean {
    return this.toolMap.delete(name);
  }

  registerGuards(...guards: GuardDefinition[]): number {
    for (const g of guards) this.customGuards.set(g.name, g);
    return this.customGuards.size;
  }

  registerVerifiers(...verifiers: VerifierDefinition[]): number {
    for (const v of verifiers) this.customVerifiers.set(v.name, v);
    return this.customVerifiers.size;
  }

  /** Snapshot of the registered tools (in insertion order). */
  tools(): ToolDefinition[] {
    return Array.from(this.toolMap.values());
  }

  // --- run -------------------------------------------------------------

  async run(prompt: string, options: RunOptions = {}): Promise<AgentResult> {
    if (this.runInProgress) {
      throw new AgentBusy('agent.run already in progress on this Agent');
    }
    this.runInProgress = true;

    const tools = Array.from(this.toolMap.values());
    const inputGuards = this.collectGuards(this.guards.input ?? [], STAGE_INPUT);
    const toolCallGuards = this.collectGuards(this.guards.tool_call ?? [], STAGE_TOOL_CALL);
    const outputGuards = this.collectGuards(this.guards.output ?? [], STAGE_OUTPUT);
    const verifiers = this.collectVerifiers(this.verifyCfg.verifiers ?? []);
    const permission: PermissionPolicy = {
      enabled: this.permission.enabled ?? false,
      allow: this.permission.allow ?? [],
      deny: this.permission.deny ?? [],
    };

    const onDelta = options.onDelta;
    const userOnEvent = options.onEvent;

    const onEvent: LoopEventHandler = async (e) => {
      if (e.kind === 'delta' && onDelta) {
        await onDelta(e.text, e.turn);
      }
      if (userOnEvent) await userOnEvent(e);
    };

    const loop = new AgentLoop({
      llm: this.llm,
      tools,
      guards: {
        input: inputGuards,
        toolCall: toolCallGuards,
        output: outputGuards,
      },
      verifiers,
      permission,
      systemPrompt: this.systemPrompt,
      maxTurns: options.maxTurns ?? this.maxTurns,
      tokenLimit: this.tokenLimit,
      maxConsecutiveFailures: this.maxConsecutiveFailures,
      streaming: !!this.streaming.enabled,
      minToolKinds: this.minToolKinds,
      ...(options.signal !== undefined ? { signal: options.signal } : {}),
      onEvent,
      ...(this.temperature !== undefined ? { temperature: this.temperature } : {}),
    });

    try {
      return await loop.run(prompt);
    } finally {
      this.runInProgress = false;
    }
  }

  /**
   * Stream the run as an async iterable. Each iteration yields a
   * `delta`/`event`/`end` event so demo UIs can render router decisions and
   * tool calls in real time.
   */
  async *runStream(
    prompt: string,
    options: RunOptions = {},
  ): AsyncIterable<StreamEvent> {
    type Q =
      | { kind: 'delta'; text: string; turn: number }
      | { kind: 'event'; event: LoopEvent };
    const queue: Q[] = [];
    let waiting: ((v: IteratorResult<Q>) => void) | null = null;
    let done = false;

    const push = (q: Q): void => {
      if (waiting) {
        const w = waiting;
        waiting = null;
        w({ value: q, done: false });
      } else {
        queue.push(q);
      }
    };

    const userOnDelta = options.onDelta;
    const userOnEvent = options.onEvent;

    const merged: RunOptions = {
      ...options,
      onDelta: async (text, turn) => {
        push({ kind: 'delta', text, turn });
        if (userOnDelta) await userOnDelta(text, turn);
      },
      onEvent: async (ev) => {
        if (ev.kind !== 'delta') push({ kind: 'event', event: ev });
        if (userOnEvent) await userOnEvent(ev);
      },
    };

    const runPromise = this.run(prompt, merged).finally(() => {
      done = true;
      if (waiting) {
        const w = waiting;
        waiting = null;
        w({ value: undefined as unknown as Q, done: true });
      }
    });

    try {
      while (true) {
        if (queue.length > 0) {
          yield queue.shift()!;
          continue;
        }
        if (done) break;
        const ev = await new Promise<IteratorResult<Q>>((resolve) => {
          waiting = resolve;
        });
        if (ev.done) break;
        yield ev.value;
      }
      const result = await runPromise;
      yield { kind: 'end', result };
    } catch (err) {
      await runPromise.catch(() => undefined);
      throw err;
    }
  }

  // --- internals -------------------------------------------------------

  private collectGuards(
    names: ReadonlyArray<string>,
    expectedStage: 'input' | 'tool_call' | 'output',
  ): GuardDefinition[] {
    const out: GuardDefinition[] = [];
    for (const name of names) {
      const custom = this.customGuards.get(name);
      if (custom) {
        if (custom.stage !== expectedStage) {
          throw new AgentError(
            `guard "${name}" is registered for stage "${custom.stage}" but used as "${expectedStage}"`,
          );
        }
        out.push(custom);
        continue;
      }
      const factory = builtinGuardFactories[name];
      if (factory) {
        const built = factory();
        if (built.stage !== expectedStage) {
          throw new AgentError(
            `built-in guard "${name}" has stage "${built.stage}", not "${expectedStage}"`,
          );
        }
        out.push(built);
        continue;
      }
      throw new AgentError(
        `guard not found: "${name}" (built-ins: ${Object.keys(builtinGuardFactories).join(', ')})`,
      );
    }
    return out;
  }

  private collectVerifiers(names: ReadonlyArray<string>): VerifierDefinition[] {
    const out: VerifierDefinition[] = [];
    for (const name of names) {
      const custom = this.customVerifiers.get(name);
      if (custom) {
        out.push(custom);
        continue;
      }
      const factory = builtinVerifierFactories[name];
      if (factory) {
        out.push(factory());
        continue;
      }
      throw new AgentError(
        `verifier not found: "${name}" (built-ins: ${Object.keys(builtinVerifierFactories).join(', ')})`,
      );
    }
    return out;
  }
}
