/**
 * LLM backends and shared types.
 *
 * Re-exported under the `@ai-agent/browser/llm` subpath. The {@link Completer}
 * interface is the single point of contact between user code and any LLM
 * backend (WebLLM, mock, or custom).
 */

export type {
  Completer,
  ChatRequest,
  ChatResponse,
  ChatMessage,
  ChatChoice,
  Role,
  ResponseFormat,
  StreamEvent,
  Usage,
} from './completer.js';
export { firstContent } from './completer.js';
export {
  WebLLMCompleter,
  type WebLLMOptions,
  type WebLLMProgressCallback,
  type ProgressReport,
} from './webllm.js';
