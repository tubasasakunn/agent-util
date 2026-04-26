/**
 * Router step.
 *
 * Calls the LLM in JSON mode with the router system prompt + history, then
 * parses the response into `{ tool, arguments, reasoning }`. Uses
 * {@link fixJson} as a fallback when the small model emits slightly broken
 * JSON (single quotes, trailing comma, code fence, etc.).
 */

import type { ChatMessage, Completer } from '../llm/completer.js';
import { fixJson } from './jsonfix.js';

export interface RouterDecision {
  tool: string;
  arguments: Record<string, unknown>;
  reasoning: string;
}

export interface RouterOptions {
  /** Optional bound on tokens, forwarded to the completer. */
  maxTokens?: number;
  /** Optional temperature, forwarded to the completer. */
  temperature?: number;
}

export class RouterError extends Error {
  constructor(message: string, public readonly raw?: string) {
    super(message);
    this.name = 'RouterError';
  }
}

export async function routerStep(
  llm: Completer,
  systemPrompt: string,
  history: ChatMessage[],
  opts: RouterOptions = {},
): Promise<RouterDecision> {
  const messages: ChatMessage[] = [
    { role: 'system', content: systemPrompt },
    ...history,
  ];
  const resp = await llm.chatCompletion({
    messages,
    response_format: { type: 'json_object' },
    ...(opts.temperature !== undefined ? { temperature: opts.temperature } : {}),
    ...(opts.maxTokens !== undefined ? { max_tokens: opts.maxTokens } : {}),
  });
  const raw = resp.choices?.[0]?.message?.content ?? '';
  return parseRouterResponse(raw);
}

/** Parse the router LLM response into a {@link RouterDecision}. */
export function parseRouterResponse(raw: string): RouterDecision {
  const fixed = fixJson(raw);
  let parsed: unknown;
  try {
    parsed = JSON.parse(fixed);
  } catch (err) {
    throw new RouterError(
      `router output is not valid JSON: ${(err as Error).message}`,
      raw,
    );
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new RouterError('router output is not a JSON object', raw);
  }
  const obj = parsed as Record<string, unknown>;

  let tool = typeof obj.tool === 'string' ? obj.tool.trim() : '';
  if (!tool) tool = 'none';

  let argsObj: Record<string, unknown> = {};
  const argsRaw = obj.arguments;
  if (argsRaw && typeof argsRaw === 'object' && !Array.isArray(argsRaw)) {
    argsObj = argsRaw as Record<string, unknown>;
  } else if (typeof argsRaw === 'string') {
    // Some SLMs emit arguments as a JSON-encoded string.
    try {
      const reparsed = JSON.parse(fixJson(argsRaw));
      if (reparsed && typeof reparsed === 'object' && !Array.isArray(reparsed)) {
        argsObj = reparsed as Record<string, unknown>;
      }
    } catch {
      /* keep empty */
    }
  }

  const reasoning = typeof obj.reasoning === 'string' ? obj.reasoning : '';
  return { tool, arguments: argsObj, reasoning };
}
