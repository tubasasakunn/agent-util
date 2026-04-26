/**
 * High-level {@link Agent} client.
 *
 * Spawns the Go `agent --rpc` binary and exposes `configure`, `run`, `abort`
 * and registration helpers (`registerTools`, `registerGuards`,
 * `registerVerifiers`, `registerMCP`) as `async` methods.
 *
 * Use it with explicit lifecycle management:
 *
 * ```ts
 * const agent = new Agent({ binaryPath: './agent' });
 * await agent.start();
 * try {
 *   await agent.configure({ max_turns: 5 });
 *   const result = await agent.run('hello');
 *   console.log(result.response);
 * } finally {
 *   await agent.close();
 * }
 * ```
 *
 * Or with TC39 explicit-resource-management (`using`):
 *
 * ```ts
 * await using agent = await Agent.open({ binaryPath: './agent' });
 * await agent.run('hello');
 * ```
 */

import { type AgentConfig, configToParams } from './config.js';
import {
  type GuardDefinition,
  type VerifierDefinition,
  guardToProtocolDict,
  verifierToProtocolDict,
} from './guard.js';
import { JsonRpcClient } from './jsonrpc.js';
import {
  type ToolDefinition,
  coerceToolResult,
  toolToProtocolDict,
} from './tool.js';
import { AgentError } from './errors.js';

// JSON-RPC method names (mirrors `pkg/protocol/methods.go`).
const M_AGENT_RUN = 'agent.run';
const M_AGENT_ABORT = 'agent.abort';
const M_AGENT_CONFIGURE = 'agent.configure';
const M_TOOL_REGISTER = 'tool.register';
const M_TOOL_EXECUTE = 'tool.execute';
const M_MCP_REGISTER = 'mcp.register';
const M_GUARD_REGISTER = 'guard.register';
const M_GUARD_EXECUTE = 'guard.execute';
const M_VERIFIER_REGISTER = 'verifier.register';
const M_VERIFIER_EXECUTE = 'verifier.execute';
const N_STREAM_DELTA = 'stream.delta';
const N_STREAM_END = 'stream.end';
const N_CONTEXT_STATUS = 'context.status';

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

export type StreamCallback = (text: string, turn: number) => void | Promise<void>;
export type StatusCallback = (
  usageRatio: number,
  tokenCount: number,
  tokenLimit: number,
) => void | Promise<void>;

export interface AgentOptions {
  /** Path to the compiled `agent` binary. Defaults to `"agent"` (PATH lookup). */
  binaryPath?: string;
  /** Extra environment variables for the subprocess (merged with `process.env`). */
  env?: Record<string, string>;
  /** Working directory for the subprocess. */
  cwd?: string;
  /** How to handle the subprocess `stderr`. Defaults to `"pipe"` (captured). */
  stderr?: 'inherit' | 'pipe' | 'ignore';
}

export interface RunOptions {
  maxTurns?: number;
  onDelta?: StreamCallback;
  onStatus?: StatusCallback;
  timeoutMs?: number;
}

export interface MCPOptions {
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  transport?: 'stdio' | 'sse';
  url?: string;
}

export type StreamEvent =
  | { kind: 'delta'; text: string; turn: number }
  | { kind: 'status'; usageRatio: number; tokenCount: number; tokenLimit: number }
  | { kind: 'end'; result: AgentResult };

export class Agent {
  private readonly binaryPath: string;
  private readonly env: Record<string, string> | undefined;
  private readonly cwd: string | undefined;
  private readonly stderr: 'inherit' | 'pipe' | 'ignore';
  private readonly rpc: JsonRpcClient;

  private readonly tools = new Map<string, ToolDefinition>();
  private readonly guards = new Map<string, GuardDefinition>();
  private readonly verifiers = new Map<string, VerifierDefinition>();

  private streamCallback: StreamCallback | undefined;
  private statusCallback: StatusCallback | undefined;
  private runInProgress = false;

  constructor(options: AgentOptions = {}) {
    this.binaryPath = options.binaryPath ?? 'agent';
    this.env = options.env;
    this.cwd = options.cwd;
    this.stderr = options.stderr ?? 'pipe';
    this.rpc = new JsonRpcClient();
  }

  // -- lifecycle ------------------------------------------------------

  /** Spawn the agent subprocess and wire up callbacks. */
  async start(): Promise<void> {
    await this.rpc.connectSubprocess(this.binaryPath, {
      args: ['--rpc'],
      env: this.env,
      cwd: this.cwd,
      stderr: this.stderr,
    });
    this.wireHandlers();
  }

  /** Terminate the subprocess and release resources. */
  async close(): Promise<void> {
    await this.rpc.close();
  }

  /** Convenience: start and return the agent ready to use. */
  static async open(options: AgentOptions = {}): Promise<Agent> {
    const agent = new Agent(options);
    await agent.start();
    return agent;
  }

  /**
   * Async-dispose hook for TC39 Explicit Resource Management.
   *
   * Enables `await using agent = await Agent.open(...)`.
   */
  async [Symbol.asyncDispose](): Promise<void> {
    await this.close();
  }

  /** Captured stderr from the subprocess (handy for debugging). */
  get stderrOutput(): string {
    return this.rpc.stderrOutput;
  }

  /** Internal: expose the underlying RPC client (used by tests). */
  get _rpc(): JsonRpcClient {
    return this.rpc;
  }

  /** Internal: wire callbacks; exposed for tests that bypass `start()`. */
  _wireHandlers(): void {
    this.wireHandlers();
  }

  // -- configuration --------------------------------------------------

  async configure(config: AgentConfig): Promise<string[]> {
    const params = configToParams(config);
    const result = (await this.rpc.call(M_AGENT_CONFIGURE, params)) as
      | { applied?: string[] }
      | null;
    return Array.isArray(result?.applied) ? [...result!.applied] : [];
  }

  // -- run / abort ----------------------------------------------------

  async run(prompt: string, options: RunOptions = {}): Promise<AgentResult> {
    if (this.runInProgress) {
      throw new AgentError('agent.run already in progress on this client');
    }
    this.runInProgress = true;
    const previousStream = this.streamCallback;
    const previousStatus = this.statusCallback;
    if (options.onDelta !== undefined) this.streamCallback = options.onDelta;
    if (options.onStatus !== undefined) this.statusCallback = options.onStatus;
    try {
      const params: Record<string, unknown> = { prompt };
      if (options.maxTurns !== undefined) params.max_turns = options.maxTurns;
      const raw = (await this.rpc.call(M_AGENT_RUN, params, {
        timeoutMs: options.timeoutMs,
      } as { timeoutMs?: number })) as Record<string, unknown> | null;
      return this.parseAgentResult(raw);
    } finally {
      this.streamCallback = previousStream;
      this.statusCallback = previousStatus;
      this.runInProgress = false;
    }
  }

  /** Abort an in-flight `run`. Returns `true` if a run was actually cancelled. */
  async abort(reason = ''): Promise<boolean> {
    const params: Record<string, unknown> = {};
    if (reason) params.reason = reason;
    const raw = (await this.rpc.call(M_AGENT_ABORT, params)) as
      | { aborted?: boolean }
      | null;
    return !!raw?.aborted;
  }

  /**
   * Stream the run as an async iterable.
   *
   * Each iteration yields one of:
   * - `{kind: 'delta', text, turn}` for `stream.delta` notifications
   * - `{kind: 'status', usageRatio, tokenCount, tokenLimit}` for `context.status`
   * - `{kind: 'end', result}` exactly once when the run completes
   *
   * Streaming must also be enabled via `configure({ streaming: { enabled: true } })`
   * for delta events to arrive.
   *
   * @example
   * ```ts
   * for await (const ev of agent.runStream('hello')) {
   *   if (ev.kind === 'delta') process.stdout.write(ev.text);
   *   else if (ev.kind === 'end') console.log(ev.result.response);
   * }
   * ```
   */
  async *runStream(prompt: string, options: RunOptions = {}): AsyncIterable<StreamEvent> {
    type Event =
      | { kind: 'delta'; text: string; turn: number }
      | { kind: 'status'; usageRatio: number; tokenCount: number; tokenLimit: number };
    const queue: Event[] = [];
    let waiting: ((value: IteratorResult<Event>) => void) | null = null;
    let done = false;

    const push = (ev: Event): void => {
      if (waiting) {
        const w = waiting;
        waiting = null;
        w({ value: ev, done: false });
      } else {
        queue.push(ev);
      }
    };

    const userOnDelta = options.onDelta;
    const userOnStatus = options.onStatus;

    const merged: RunOptions = {
      ...options,
      onDelta: async (text, turn) => {
        push({ kind: 'delta', text, turn });
        if (userOnDelta) await userOnDelta(text, turn);
      },
      onStatus: async (ratio, count, limit) => {
        push({
          kind: 'status',
          usageRatio: ratio,
          tokenCount: count,
          tokenLimit: limit,
        });
        if (userOnStatus) await userOnStatus(ratio, count, limit);
      },
    };

    const runPromise = this.run(prompt, merged).finally(() => {
      done = true;
      if (waiting) {
        const w = waiting;
        waiting = null;
        w({ value: undefined as unknown as Event, done: true });
      }
    });

    try {
      while (true) {
        if (queue.length > 0) {
          yield queue.shift()!;
          continue;
        }
        if (done) break;
        const ev = await new Promise<IteratorResult<Event>>((resolve) => {
          waiting = resolve;
        });
        if (ev.done) break;
        yield ev.value;
      }
      const result = await runPromise;
      yield { kind: 'end', result };
    } catch (err) {
      // Make sure the run promise resolves before we throw.
      await runPromise.catch(() => undefined);
      throw err;
    }
  }

  // -- registration ---------------------------------------------------

  async registerTools(...tools: ToolDefinition[]): Promise<number> {
    for (const def of tools) this.tools.set(def.name, def);
    const params = { tools: tools.map((t) => toolToProtocolDict(t)) };
    const raw = (await this.rpc.call(M_TOOL_REGISTER, params)) as
      | { registered?: number }
      | null;
    return Number(raw?.registered ?? 0);
  }

  async registerGuards(...guards: GuardDefinition[]): Promise<number> {
    for (const def of guards) this.guards.set(def.name, def);
    const params = { guards: guards.map((g) => guardToProtocolDict(g)) };
    const raw = (await this.rpc.call(M_GUARD_REGISTER, params)) as
      | { registered?: number }
      | null;
    return Number(raw?.registered ?? 0);
  }

  async registerVerifiers(...verifiers: VerifierDefinition[]): Promise<number> {
    for (const def of verifiers) this.verifiers.set(def.name, def);
    const params = { verifiers: verifiers.map((v) => verifierToProtocolDict(v)) };
    const raw = (await this.rpc.call(M_VERIFIER_REGISTER, params)) as
      | { registered?: number }
      | null;
    return Number(raw?.registered ?? 0);
  }

  async registerMCP(options: MCPOptions): Promise<string[]> {
    const params: Record<string, unknown> = {
      transport: options.transport ?? 'stdio',
    };
    if (options.command !== undefined) params.command = options.command;
    if (options.args && options.args.length > 0) params.args = [...options.args];
    if (options.env) params.env = { ...options.env };
    if (options.url !== undefined) params.url = options.url;
    const raw = (await this.rpc.call(M_MCP_REGISTER, params)) as
      | { tools?: string[] }
      | null;
    return Array.isArray(raw?.tools) ? [...raw!.tools] : [];
  }

  // -- internal: handler wiring ---------------------------------------

  private wireHandlers(): void {
    this.rpc.setRequestHandler(M_TOOL_EXECUTE, (p) => this.handleToolExecute(p));
    this.rpc.setRequestHandler(M_GUARD_EXECUTE, (p) => this.handleGuardExecute(p));
    this.rpc.setRequestHandler(M_VERIFIER_EXECUTE, (p) =>
      this.handleVerifierExecute(p),
    );
    this.rpc.setNotificationHandler(N_STREAM_DELTA, (p) => this.handleStreamDelta(p));
    this.rpc.setNotificationHandler(N_STREAM_END, () => undefined);
    this.rpc.setNotificationHandler(N_CONTEXT_STATUS, (p) =>
      this.handleContextStatus(p),
    );
  }

  private async handleToolExecute(
    params: Record<string, unknown>,
  ): Promise<Record<string, unknown>> {
    const name = String(params.name ?? '');
    const args = (params.args ?? {}) as Record<string, unknown>;
    const def = this.tools.get(name);
    if (!def) {
      return { content: `tool not found: ${name}`, is_error: true };
    }
    try {
      const result = await def.handler(args);
      return coerceToolResult(result) as unknown as Record<string, unknown>;
    } catch (err) {
      return {
        content: `tool execution failed: ${err instanceof Error ? err.message : String(err)}`,
        is_error: true,
      };
    }
  }

  private async handleGuardExecute(
    params: Record<string, unknown>,
  ): Promise<Record<string, unknown>> {
    const name = String(params.name ?? '');
    const stage = String(params.stage ?? '');
    const def = this.guards.get(name);
    if (!def || def.stage !== stage) {
      return { decision: 'deny', reason: `guard not found: ${name}/${stage}` };
    }
    try {
      const out = await def.call({
        input: typeof params.input === 'string' ? params.input : '',
        toolName: typeof params.tool_name === 'string' ? params.tool_name : '',
        args: (params.args ?? {}) as Record<string, unknown>,
        output: typeof params.output === 'string' ? params.output : '',
      });
      return { decision: out.decision, reason: out.reason ?? '' };
    } catch (err) {
      return {
        decision: 'deny',
        reason: `guard error: ${err instanceof Error ? err.message : String(err)}`,
      };
    }
  }

  private async handleVerifierExecute(
    params: Record<string, unknown>,
  ): Promise<Record<string, unknown>> {
    const name = String(params.name ?? '');
    const def = this.verifiers.get(name);
    if (!def) {
      return { passed: false, summary: `verifier not found: ${name}` };
    }
    try {
      const out = await def.call({
        toolName: String(params.tool_name ?? ''),
        args: (params.args ?? {}) as Record<string, unknown>,
        result: String(params.result ?? ''),
      });
      return { passed: out.passed, summary: out.summary ?? '' };
    } catch (err) {
      return {
        passed: false,
        summary: `verifier error: ${err instanceof Error ? err.message : String(err)}`,
      };
    }
  }

  private async handleStreamDelta(params: Record<string, unknown>): Promise<void> {
    const cb = this.streamCallback;
    if (!cb) return;
    const text = String(params.text ?? '');
    const turn = Number(params.turn ?? 0);
    await cb(text, turn);
  }

  private async handleContextStatus(params: Record<string, unknown>): Promise<void> {
    const cb = this.statusCallback;
    if (!cb) return;
    const ratio = Number(params.usage_ratio ?? 0);
    const count = Number(params.token_count ?? 0);
    const limit = Number(params.token_limit ?? 0);
    await cb(ratio, count, limit);
  }

  // -- result parsing -------------------------------------------------

  private parseAgentResult(raw: Record<string, unknown> | null): AgentResult {
    const usageRaw = (raw?.usage ?? {}) as Record<string, unknown>;
    return {
      response: String(raw?.response ?? ''),
      reason: String(raw?.reason ?? ''),
      turns: Number(raw?.turns ?? 0),
      usage: {
        prompt_tokens: Number(usageRaw.prompt_tokens ?? 0),
        completion_tokens: Number(usageRaw.completion_tokens ?? 0),
        total_tokens: Number(usageRaw.total_tokens ?? 0),
      },
    };
  }
}
