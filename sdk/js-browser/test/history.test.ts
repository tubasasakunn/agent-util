import { describe, expect, it } from 'vitest';
import {
  History,
  estimateMessageTokens,
  estimateTextTokens,
  toolCallMessage,
  userMessage,
} from '../src/engine/history.js';

describe('estimateTextTokens', () => {
  it('returns 0 for empty input', () => {
    expect(estimateTextTokens('')).toBe(0);
  });

  it('returns at least 1 for any non-empty input', () => {
    expect(estimateTextTokens('a')).toBeGreaterThanOrEqual(1);
  });

  it('counts CJK heavier than ASCII', () => {
    const ascii = estimateTextTokens('hello world this is a long sentence');
    const cjk = estimateTextTokens('こんにちは世界'); // 7 chars
    // CJK should beat ASCII per-character.
    expect(cjk / 7).toBeGreaterThan(ascii / 35);
  });
});

describe('estimateMessageTokens', () => {
  it('includes per-message overhead', () => {
    const empty = estimateMessageTokens({ role: 'user', content: '' });
    expect(empty).toBeGreaterThanOrEqual(4);
  });

  it('charges for tool_calls metadata', () => {
    const plain = estimateMessageTokens({ role: 'assistant', content: '' });
    const withCalls = estimateMessageTokens(
      toolCallMessage('id_1', 'echo', { x: 1 }),
    );
    expect(withCalls).toBeGreaterThan(plain);
  });
});

describe('History', () => {
  it('appends and tracks token count', () => {
    const h = new History(1000);
    expect(h.tokenCount()).toBe(0);
    h.add(userMessage('hello'));
    expect(h.tokenCount()).toBeGreaterThan(0);
    expect(h.size()).toBe(1);
  });

  it('reports usage ratio against limit', () => {
    const h = new History(100);
    h.add(userMessage('x'.repeat(500)));
    expect(h.usageRatio()).toBeGreaterThan(0);
  });

  it('snipTo drops the oldest entries', () => {
    const h = new History(10000);
    for (let i = 0; i < 10; i++) h.add(userMessage(`m${i}`));
    const dropped = h.snipTo(3);
    expect(dropped).toBe(7);
    expect(h.size()).toBe(3);
    expect(h.messages()[0].content).toBe('m7');
  });

  it('reserved tokens add to the count', () => {
    const h = new History(1000);
    h.setReservedTokens(50);
    expect(h.tokenCount()).toBe(50);
    h.add(userMessage('hi'));
    expect(h.tokenCount()).toBeGreaterThan(50);
  });
});
