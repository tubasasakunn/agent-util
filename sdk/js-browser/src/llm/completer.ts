/**
 * LLM completer interface.
 *
 * The browser engine talks to whatever you plug in here — WebLLM, an OpenAI
 * proxy, a mocked completer in tests. The interface is intentionally tiny:
 *
 *  - `chatCompletion(req)` -> single response
 *  - `chatCompletionStream(req)` -> async iterable of `{ delta, finish_reason }`
 *
 * Streaming is optional; if a backend can't do it the engine falls back to
 * `chatCompletion` and synthesises a single chunk.
 */

export type Role = 'system' | 'user' | 'assistant' | 'tool';

export interface ChatMessage {
  role: Role;
  content: string;
  /** Optional tool-call metadata (assistant). Mostly unused in the browser. */
  tool_calls?: Array<{
    id: string;
    type: 'function';
    function: { name: string; arguments: string };
  }>;
  /** Tool result message wires up to its triggering call_id. */
  tool_call_id?: string;
  /** Optional name for the message (e.g. tool name). */
  name?: string;
}

export interface ResponseFormat {
  /** Either `'text'` or `'json_object'`. */
  type: 'text' | 'json_object';
  /**
   * Optional JSON Schema. When set together with `type: 'json_object'`, backends
   * that support grammar-constrained decoding (e.g. WebLLM) restrict the output
   * to match this schema. May be a JSON Schema object or a pre-stringified one.
   */
  schema?: Record<string, unknown> | string;
}

export interface ChatRequest {
  messages: ChatMessage[];
  response_format?: ResponseFormat;
  temperature?: number;
  max_tokens?: number;
  /** Optional caller-provided abort signal forwarded to the backend. */
  signal?: AbortSignal;
}

export interface Usage {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

export interface ChatChoice {
  index: number;
  message: ChatMessage;
  finish_reason: string;
}

export interface ChatResponse {
  id?: string;
  model?: string;
  choices: ChatChoice[];
  usage?: Usage;
}

export interface StreamEvent {
  delta: string;
  finish_reason?: string;
}

export interface Completer {
  chatCompletion(req: ChatRequest): Promise<ChatResponse>;
  /** Optional streaming entry point. */
  chatCompletionStream?(req: ChatRequest): AsyncIterable<StreamEvent>;
  /** True once `load()` (or equivalent) has finished. */
  readonly ready?: boolean;
}

/** Pull `content` out of the first choice, defaulting to `""`. */
export function firstContent(resp: ChatResponse): string {
  const c = resp.choices?.[0]?.message?.content;
  return typeof c === 'string' ? c : '';
}
