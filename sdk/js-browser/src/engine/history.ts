/**
 * Conversation history with character-based token estimation.
 *
 * Roughly mirrors `internal/context/manager.go` but only the bits the browser
 * SDK uses: append, snapshot, drop tail, recompute usage. CJK characters
 * count as ~1.5 tokens, ASCII as ~0.35 (matches the Go estimator's heuristic).
 *
 * We keep the system prompt and tool-definition reservation outside the
 * history; callers update it via {@link setReservedTokens}.
 */

import type { ChatMessage, Role } from '../llm/completer.js';

const PER_MESSAGE_OVERHEAD = 4;
const TOOLCALL_OVERHEAD = 4;

export interface HistoryEntry {
  message: ChatMessage;
  tokens: number;
}

export class History {
  private entries: HistoryEntry[] = [];
  private reservedTokens = 0;
  private cachedCount = 0;

  constructor(private readonly tokenLimit: number) {}

  add(msg: ChatMessage): void {
    const tokens = estimateMessageTokens(msg);
    this.entries.push({ message: msg, tokens });
    this.cachedCount += tokens;
  }

  /** Append a system message at index 0 (used very rarely, mostly tests). */
  prependSystem(content: string): void {
    const msg: ChatMessage = { role: 'system', content };
    const tokens = estimateMessageTokens(msg);
    this.entries.unshift({ message: msg, tokens });
    this.cachedCount += tokens;
  }

  messages(): ChatMessage[] {
    return this.entries.map((e) => e.message);
  }

  /** Discard entries from `keepIdx` onward (used by snip-style compaction). */
  truncateFrom(keepIdx: number): void {
    if (keepIdx >= this.entries.length) return;
    this.entries.splice(keepIdx);
    this.recompute();
  }

  /** Drop the oldest non-system entries until at most `keepLast` remain. */
  snipTo(keepLast: number): number {
    if (keepLast <= 0) return 0;
    if (this.entries.length <= keepLast) return 0;
    const drop = this.entries.length - keepLast;
    this.entries.splice(0, drop);
    this.recompute();
    return drop;
  }

  setReservedTokens(n: number): void {
    this.reservedTokens = Math.max(0, n);
  }

  reserved(): number {
    return this.reservedTokens;
  }

  tokenCount(): number {
    return this.cachedCount + this.reservedTokens;
  }

  limit(): number {
    return this.tokenLimit;
  }

  usageRatio(): number {
    if (this.tokenLimit <= 0) return 0;
    return this.tokenCount() / this.tokenLimit;
  }

  size(): number {
    return this.entries.length;
  }

  /** Find the index of the last message with the given role, or -1. */
  lastIndexOfRole(role: Role): number {
    for (let i = this.entries.length - 1; i >= 0; i--) {
      if (this.entries[i].message.role === role) return i;
    }
    return -1;
  }

  private recompute(): void {
    let total = 0;
    for (const e of this.entries) total += e.tokens;
    this.cachedCount = total;
  }
}

// ---------------------------------------------------------------------------
// Token estimation
// ---------------------------------------------------------------------------

export function estimateMessageTokens(msg: ChatMessage): number {
  let tokens = PER_MESSAGE_OVERHEAD;
  tokens += estimateTextTokens(msg.content ?? '');
  if (msg.tool_calls) {
    for (const tc of msg.tool_calls) {
      tokens += estimateTextTokens(tc.function.name);
      tokens += estimateTextTokens(tc.function.arguments);
      tokens += TOOLCALL_OVERHEAD;
    }
  }
  if (msg.tool_call_id) tokens += estimateTextTokens(msg.tool_call_id);
  if (msg.name) tokens += estimateTextTokens(msg.name);
  return tokens;
}

/**
 * Crude character-class estimator. CJK / Hangul characters are ~1.5
 * tokens each; everything else (ASCII, etc.) is ~0.35 tokens. Returns at
 * least 1 for any non-empty string.
 */
export function estimateTextTokens(text: string): number {
  if (!text) return 0;
  let total = 0;
  for (const ch of text) {
    if (isCJK(ch.codePointAt(0)!)) total += 1.5;
    else total += 0.35;
  }
  const result = Math.floor(total);
  return result === 0 ? 1 : result;
}

function isCJK(cp: number): boolean {
  // Hiragana
  if (cp >= 0x3040 && cp <= 0x309f) return true;
  // Katakana (incl. phonetic extensions)
  if (cp >= 0x30a0 && cp <= 0x30ff) return true;
  if (cp >= 0x31f0 && cp <= 0x31ff) return true;
  // CJK Unified Ideographs (BMP)
  if (cp >= 0x4e00 && cp <= 0x9fff) return true;
  // CJK Extension A
  if (cp >= 0x3400 && cp <= 0x4dbf) return true;
  // CJK Compatibility Ideographs
  if (cp >= 0xf900 && cp <= 0xfaff) return true;
  // Hangul syllables
  if (cp >= 0xac00 && cp <= 0xd7a3) return true;
  // Hangul Jamo
  if (cp >= 0x1100 && cp <= 0x11ff) return true;
  // CJK Unified Ideographs Extension B (supplementary)
  if (cp >= 0x20000 && cp <= 0x2a6df) return true;
  return false;
}

// ---------------------------------------------------------------------------
// Convenience message constructors
// ---------------------------------------------------------------------------

export function userMessage(content: string): ChatMessage {
  return { role: 'user', content };
}

export function systemMessage(content: string): ChatMessage {
  return { role: 'system', content };
}

export function assistantMessage(content: string): ChatMessage {
  return { role: 'assistant', content };
}

export function toolResultMessage(callId: string, content: string): ChatMessage {
  // 多くのオープンモデル（Qwen2.5 / Llama 3.2 / Gemma 2 など WebLLM の prebuilt）は
  // OpenAI 形式の role: "tool" を受け付けない。互換性のため user メッセージに包む。
  // call_id は metadata として残しておく（streaming UI が紐付けに使える）。
  return {
    role: 'user',
    content: `[tool_result name=${callId}]\n${content}`,
    tool_call_id: callId,
  };
}

export function toolCallMessage(
  callId: string,
  name: string,
  args: Record<string, unknown>,
): ChatMessage {
  return {
    role: 'assistant',
    content: '',
    tool_calls: [
      {
        id: callId,
        type: 'function',
        function: { name, arguments: JSON.stringify(args ?? {}) },
      },
    ],
  };
}
