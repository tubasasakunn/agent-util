/**
 * Internal JSON-RPC 2.0 client over newline-delimited JSON streams.
 *
 * The transport is intentionally abstracted via {@link JsonRpcTransport} so
 * the client works equally well against:
 *
 * - a real `child_process` spawned by {@link connectSubprocess}, and
 * - an in-process pair of streams used by tests.
 *
 * The class supports both directions of the protocol:
 *
 * - **wrapper -> core**: `call(method, params)` returns the response result
 *   (or rejects with an {@link AgentError}).
 * - **core -> wrapper**: handlers registered via {@link setRequestHandler}
 *   are invoked when the core sends a request such as `tool.execute`,
 *   `guard.execute` or `verifier.execute`. Handlers are async and return the
 *   result object.
 * - **core -> wrapper notifications**: handlers registered via
 *   {@link setNotificationHandler} for `stream.delta` / `stream.end` /
 *   `context.status`.
 */

import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { createInterface, type Interface as ReadlineInterface } from 'node:readline';
import { Buffer } from 'node:buffer';
import { AgentError, fromRpcError } from './errors.js';

export const JSONRPC_VERSION = '2.0';

export type JsonRpcId = number | string;

export interface JsonRpcMessage {
  jsonrpc: '2.0';
  id?: JsonRpcId | null;
  method?: string;
  params?: unknown;
  result?: unknown;
  error?: { code: number; message: string; data?: unknown } | null;
}

export type NotificationHandler = (params: Record<string, unknown>) => void | Promise<void>;
export type RequestHandler = (
  params: Record<string, unknown>,
) => Promise<Record<string, unknown>>;

/**
 * Minimal stream contract the client needs.
 *
 * `readLines()` yields one JSON-encoded message per item (no trailing newline).
 * `write(line)` writes a single JSON message; the caller appends `\n`.
 * `close()` should flush and tear down.
 */
export interface JsonRpcTransport {
  readLines(): AsyncIterable<string>;
  write(line: string): Promise<void>;
  close(): Promise<void>;
}

interface PendingEntry {
  resolve: (value: unknown) => void;
  reject: (reason: unknown) => void;
}

/** Build a transport around a Node.js readable+writable stream pair. */
export function nodeStreamsTransport(
  readable: NodeJS.ReadableStream,
  writable: NodeJS.WritableStream,
  options: { onClose?: () => Promise<void> | void } = {},
): JsonRpcTransport {
  let rl: ReadlineInterface | null = null;
  return {
    async *readLines() {
      rl = createInterface({ input: readable, crlfDelay: Infinity });
      for await (const line of rl) {
        if (line.length === 0) continue;
        yield line;
      }
    },
    async write(line: string): Promise<void> {
      await new Promise<void>((resolve, reject) => {
        const ok = writable.write(Buffer.from(line + '\n', 'utf8'), (err) => {
          if (err) reject(err);
          else resolve();
        });
        if (!ok) {
          // Backpressure: wait for drain. The callback above still resolves us
          // once the chunk is flushed, so nothing extra to do here.
        }
      });
    },
    async close(): Promise<void> {
      try {
        rl?.close();
      } catch {
        /* ignore */
      }
      try {
        if (typeof (writable as { end?: (cb?: () => void) => void }).end === 'function') {
          await new Promise<void>((resolve) => {
            (writable as { end: (cb?: () => void) => void }).end(() => resolve());
          });
        }
      } catch {
        /* ignore */
      }
      if (options.onClose) await options.onClose();
    },
  };
}

export class JsonRpcClient {
  private transport: JsonRpcTransport | null = null;
  private readLoopPromise: Promise<void> | null = null;
  private nextId = 0;
  private readonly pending = new Map<JsonRpcId, PendingEntry>();
  private readonly notifHandlers = new Map<string, NotificationHandler>();
  private readonly requestHandlers = new Map<string, RequestHandler>();
  private writeChain: Promise<void> = Promise.resolve();
  private closed = false;
  private stderrBuffer: string[] = [];

  // -- lifecycle -------------------------------------------------------

  /** Spawn `binaryPath` as a subprocess and start the read loop. */
  async connectSubprocess(
    binaryPath: string,
    options: {
      args?: string[];
      env?: Record<string, string>;
      cwd?: string;
      stderr?: 'inherit' | 'pipe' | 'ignore';
    } = {},
  ): Promise<void> {
    if (this.transport) throw new AgentError('JsonRpcClient already connected');
    const args = options.args ?? ['--rpc'];
    const env = { ...process.env, ...(options.env ?? {}) };
    const stderrMode = options.stderr ?? 'pipe';

    const proc = spawn(binaryPath, args, {
      stdio: ['pipe', 'pipe', stderrMode],
      env,
      cwd: options.cwd,
    }) as ChildProcessWithoutNullStreams;

    if (stderrMode === 'pipe' && proc.stderr) {
      proc.stderr.setEncoding('utf8');
      proc.stderr.on('data', (chunk: string) => {
        this.stderrBuffer.push(chunk);
      });
    }

    this.attach(
      nodeStreamsTransport(proc.stdout, proc.stdin, {
        onClose: async () => {
          if (proc.exitCode === null && proc.signalCode === null) {
            // Best-effort wait for graceful exit, then SIGTERM/KILL.
            const exited = await Promise.race([
              new Promise<boolean>((resolve) => proc.once('exit', () => resolve(true))),
              new Promise<boolean>((resolve) => setTimeout(() => resolve(false), 5000)),
            ]);
            if (!exited) {
              proc.kill('SIGTERM');
              await Promise.race([
                new Promise<void>((resolve) => proc.once('exit', () => resolve())),
                new Promise<void>((resolve) => setTimeout(resolve, 2000)),
              ]);
              if (proc.exitCode === null) proc.kill('SIGKILL');
            }
          }
        },
      }),
    );
  }

  /** Attach to an existing transport (used in tests and by `connectSubprocess`). */
  attach(transport: JsonRpcTransport): void {
    if (this.transport) throw new AgentError('JsonRpcClient already attached');
    this.transport = transport;
    this.readLoopPromise = this.readLoop().catch(() => {
      /* swallowed; failures already settle pending promises */
    });
  }

  /** Tear down the transport and reject any in-flight calls. */
  async close(): Promise<void> {
    if (this.closed) return;
    this.closed = true;
    const transport = this.transport;
    this.transport = null;
    if (transport) {
      try {
        await transport.close();
      } catch {
        /* ignore */
      }
    }
    if (this.readLoopPromise) {
      try {
        await this.readLoopPromise;
      } catch {
        /* ignore */
      }
      this.readLoopPromise = null;
    }
    for (const entry of this.pending.values()) {
      entry.reject(new AgentError('connection closed'));
    }
    this.pending.clear();
  }

  /** Captured stderr from the spawned subprocess (for debugging). */
  get stderrOutput(): string {
    return this.stderrBuffer.join('');
  }

  // -- handler registration -------------------------------------------

  setNotificationHandler(method: string, handler: NotificationHandler): void {
    this.notifHandlers.set(method, handler);
  }

  setRequestHandler(method: string, handler: RequestHandler): void {
    this.requestHandlers.set(method, handler);
  }

  // -- RPC primitives -------------------------------------------------

  /**
   * Send a wrapper -> core request and await its result.
   *
   * Rejects with an {@link AgentError} subclass on JSON-RPC error responses,
   * with `Error('timeout')` on timeout, or `AgentError` on transport failure.
   */
  async call(
    method: string,
    params: Record<string, unknown> = {},
    options: { timeoutMs?: number } = {},
  ): Promise<unknown> {
    if (!this.transport) throw new AgentError('not connected');
    const id = ++this.nextId;
    const message: JsonRpcMessage = {
      jsonrpc: JSONRPC_VERSION,
      method,
      params,
      id,
    };

    const promise = new Promise<unknown>((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
    });

    try {
      await this.writeMessage(message);
    } catch (err) {
      this.pending.delete(id);
      throw new AgentError(`failed to send ${method}: ${(err as Error).message}`);
    }

    if (options.timeoutMs !== undefined) {
      let timer: ReturnType<typeof setTimeout> | null = null;
      const timed = new Promise<never>((_, reject) => {
        timer = setTimeout(() => {
          this.pending.delete(id);
          reject(new AgentError(`timeout after ${options.timeoutMs}ms calling ${method}`));
        }, options.timeoutMs);
      });
      try {
        return await Promise.race([promise, timed]);
      } finally {
        if (timer) clearTimeout(timer);
      }
    }
    return await promise;
  }

  /** Send a notification (no `id`, no response expected). */
  async notify(method: string, params: Record<string, unknown> = {}): Promise<void> {
    if (!this.transport) throw new AgentError('not connected');
    const message: JsonRpcMessage = { jsonrpc: JSONRPC_VERSION, method, params };
    await this.writeMessage(message);
  }

  // -- internal -------------------------------------------------------

  private async writeMessage(message: JsonRpcMessage): Promise<void> {
    const transport = this.transport;
    if (!transport) throw new AgentError('not connected');
    const line = JSON.stringify(message);
    // Serialise writes so concurrent callers don't interleave output.
    const next = this.writeChain.then(() => transport.write(line));
    this.writeChain = next.catch(() => {
      /* swallow so future writes can still proceed */
    });
    await next;
  }

  private async readLoop(): Promise<void> {
    const transport = this.transport;
    if (!transport) return;
    try {
      for await (const line of transport.readLines()) {
        let message: JsonRpcMessage;
        try {
          message = JSON.parse(line) as JsonRpcMessage;
        } catch {
          // The core MUST NOT send invalid JSON; ignore but don't crash.
          continue;
        }
        await this.dispatch(message);
      }
    } catch {
      /* transport dead */
    } finally {
      for (const entry of this.pending.values()) {
        entry.reject(new AgentError('connection closed'));
      }
      this.pending.clear();
    }
  }

  private async dispatch(message: JsonRpcMessage): Promise<void> {
    // Response: has id, no method.
    if (message.method === undefined && message.id !== undefined && message.id !== null) {
      const entry = this.pending.get(message.id);
      if (!entry) return;
      this.pending.delete(message.id);
      if (message.error) {
        entry.reject(
          fromRpcError(
            message.error.code,
            message.error.message ?? 'unknown error',
            message.error.data,
          ),
        );
      } else {
        entry.resolve(message.result);
      }
      return;
    }

    if (!message.method) return;
    const params = (message.params ?? {}) as Record<string, unknown>;

    if (message.id === undefined || message.id === null) {
      // Notification
      const handler = this.notifHandlers.get(message.method);
      if (!handler) return;
      try {
        await handler(params);
      } catch {
        /* notifications don't fail back to the peer */
      }
      return;
    }

    // core -> wrapper request
    const handler = this.requestHandlers.get(message.method);
    if (!handler) {
      await this.sendError(message.id, -32601, `method not found: ${message.method}`);
      return;
    }
    try {
      const result = await handler(params);
      await this.writeMessage({
        jsonrpc: JSONRPC_VERSION,
        id: message.id,
        result,
      });
    } catch (err) {
      const code = err instanceof AgentError && err.code !== null ? err.code : -32603;
      await this.sendError(message.id, code, err instanceof Error ? err.message : String(err));
    }
  }

  private async sendError(id: JsonRpcId, code: number, message: string): Promise<void> {
    await this.writeMessage({
      jsonrpc: JSONRPC_VERSION,
      id,
      error: { code, message },
    });
  }
}
