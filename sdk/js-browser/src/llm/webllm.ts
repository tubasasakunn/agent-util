/**
 * WebLLM adapter — wraps `@mlc-ai/web-llm`'s `MLCEngine` so it can drive the
 * browser engine via the {@link Completer} interface.
 *
 * `@mlc-ai/web-llm` is a peer dependency. Importing this module in a Node
 * test environment without WebGPU is fine as long as you don't actually call
 * `load()`; the dynamic `import('@mlc-ai/web-llm')` is deferred to that point.
 *
 * Models are downloaded and cached by the WebLLM runtime in IndexedDB; first
 * load is hundreds of MB, subsequent loads are instant.
 */

import type {
  ChatRequest,
  ChatResponse,
  Completer,
  StreamEvent,
} from './completer.js';
import { LLMNotLoaded } from '../errors.js';

/** Subset of `@mlc-ai/web-llm`'s `InitProgressReport`. */
export interface ProgressReport {
  progress: number;
  text: string;
  timeElapsed?: number;
}

export interface WebLLMOptions {
  /** Model ID, e.g. `"gemma-2-2b-it-q4f16_1-MLC"`. */
  model: string;
  /** Optional default temperature applied when callers don't pass one. */
  temperature?: number;
  /** Forwarded to `CreateMLCEngine` (`appConfig`, `logLevel`, ...). */
  engineConfig?: Record<string, unknown>;
}

/**
 * Minimal structural type for the slice of `@mlc-ai/web-llm` we touch. Lets
 * us avoid a hard import dependency at type-check time and lets tests stub
 * the engine easily.
 */
interface MinimalMLCEngine {
  chat: {
    completions: {
      create(req: Record<string, unknown>): Promise<unknown>;
    };
  };
  unload?(): Promise<void>;
}

export type WebLLMProgressCallback = (r: ProgressReport) => void;

export class WebLLMCompleter implements Completer {
  private engine: MinimalMLCEngine | null = null;
  private loadingPromise: Promise<void> | null = null;
  public ready = false;

  constructor(private readonly opts: WebLLMOptions) {
    if (!opts?.model) throw new TypeError('WebLLMCompleter: { model } is required');
  }

  /**
   * Load (download + initialise) the model. Safe to call concurrently — the
   * second caller awaits the same in-flight load.
   *
   * In SSR / Node test environments without WebGPU this throws when the
   * underlying `CreateMLCEngine` rejects.
   */
  async load(onProgress?: WebLLMProgressCallback): Promise<void> {
    if (this.ready) return;
    if (this.loadingPromise) return this.loadingPromise;

    this.loadingPromise = (async () => {
      // Dynamic import so consumers without WebLLM (eg tests) can import this
      // file just for its types. We hide the specifier from TypeScript's
      // module resolution because `@mlc-ai/web-llm` is only an optional peer
      // dep (and may not be installed in CI / Node test environments).
      const specifier = '@mlc-ai/web-llm';
      const mod = (await import(/* @vite-ignore */ specifier)) as {
        CreateMLCEngine: (
          model: string,
          cfg?: Record<string, unknown>,
        ) => Promise<MinimalMLCEngine>;
      };
      const cfg: Record<string, unknown> = { ...(this.opts.engineConfig ?? {}) };
      if (onProgress) cfg.initProgressCallback = onProgress;
      const engine = await mod.CreateMLCEngine(this.opts.model, cfg);
      this.engine = engine;
      this.ready = true;
    })();

    try {
      await this.loadingPromise;
    } finally {
      this.loadingPromise = null;
    }
  }

  /** Tear down the engine (if any). */
  async unload(): Promise<void> {
    const eng = this.engine;
    this.engine = null;
    this.ready = false;
    if (eng?.unload) {
      try {
        await eng.unload();
      } catch {
        /* ignore */
      }
    }
  }

  /** Inject a pre-built engine — useful for tests and advanced callers. */
  attachEngine(engine: MinimalMLCEngine): void {
    this.engine = engine;
    this.ready = true;
  }

  async chatCompletion(req: ChatRequest): Promise<ChatResponse> {
    const engine = this.requireEngine();
    const payload = this.toPayload(req, false);
    const raw = await engine.chat.completions.create(payload);
    return raw as ChatResponse;
  }

  async *chatCompletionStream(req: ChatRequest): AsyncIterable<StreamEvent> {
    const engine = this.requireEngine();
    const payload = this.toPayload(req, true);
    const stream = (await engine.chat.completions.create(payload)) as AsyncIterable<{
      choices?: Array<{
        delta?: { content?: string };
        finish_reason?: string | null;
      }>;
    }>;
    for await (const chunk of stream) {
      const choice = chunk.choices?.[0];
      const delta = choice?.delta?.content ?? '';
      const finish = choice?.finish_reason ?? '';
      if (delta || finish) {
        yield { delta, finish_reason: finish || undefined };
      }
    }
  }

  private requireEngine(): MinimalMLCEngine {
    if (!this.engine || !this.ready) {
      throw new LLMNotLoaded(
        'WebLLMCompleter not loaded — call `await completer.load()` first',
      );
    }
    return this.engine;
  }

  private toPayload(req: ChatRequest, stream: boolean): Record<string, unknown> {
    const out: Record<string, unknown> = {
      messages: req.messages.map((m) => {
        const o: Record<string, unknown> = { role: m.role, content: m.content };
        if (m.name) o.name = m.name;
        if (m.tool_call_id) o.tool_call_id = m.tool_call_id;
        if (m.tool_calls) o.tool_calls = m.tool_calls;
        return o;
      }),
      stream,
    };
    if (req.temperature !== undefined) out.temperature = req.temperature;
    else if (this.opts.temperature !== undefined) out.temperature = this.opts.temperature;
    if (req.max_tokens !== undefined) out.max_tokens = req.max_tokens;
    if (req.response_format) out.response_format = req.response_format;
    return out;
  }
}
