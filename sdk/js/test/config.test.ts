/** Tests for `configToParams` (omitempty behaviour) and `stripUndefined`. */

import { describe, it, expect } from 'vitest';
import { configToParams, stripUndefined } from '../src/config.js';

describe('configToParams', () => {
  it('returns {} for an empty config', () => {
    expect(configToParams({})).toEqual({});
  });

  it('passes top-level scalars through', () => {
    expect(
      configToParams({ max_turns: 8, system_prompt: 'hello', token_limit: 4096 }),
    ).toEqual({
      max_turns: 8,
      system_prompt: 'hello',
      token_limit: 4096,
    });
  });

  it('drops undefined fields', () => {
    expect(configToParams({ max_turns: undefined, system_prompt: 'x' })).toEqual({
      system_prompt: 'x',
    });
  });

  it('serialises nested streaming config', () => {
    expect(
      configToParams({
        streaming: { enabled: true, context_status: true },
      }),
    ).toEqual({ streaming: { enabled: true, context_status: true } });
  });

  it('strips undefined inside nested config', () => {
    expect(configToParams({ streaming: { enabled: false } })).toEqual({
      streaming: { enabled: false },
    });
  });

  it('serialises guards lists', () => {
    expect(
      configToParams({ guards: { input: ['no_secrets'], output: ['pii'] } }),
    ).toEqual({
      guards: { input: ['no_secrets'], output: ['pii'] },
    });
  });

  it('serialises permission lists', () => {
    expect(
      configToParams({
        permission: { enabled: true, allow: ['read_file'], deny: ['*'] },
      }),
    ).toEqual({
      permission: { enabled: true, allow: ['read_file'], deny: ['*'] },
    });
  });

  it('serialises compaction with floats', () => {
    expect(
      configToParams({
        compaction: { enabled: true, target_ratio: 0.5, summarizer: 'llm' },
      }),
    ).toEqual({
      compaction: { enabled: true, target_ratio: 0.5, summarizer: 'llm' },
    });
  });

  it('serialises verify block', () => {
    expect(
      configToParams({ verify: { verifiers: ['v1'], max_step_retries: 3 } }),
    ).toEqual({ verify: { verifiers: ['v1'], max_step_retries: 3 } });
  });

  it('matches the OpenRPC enable-streaming example', () => {
    expect(
      configToParams({
        max_turns: 10,
        streaming: { enabled: true, context_status: true },
      }),
    ).toEqual({
      max_turns: 10,
      streaming: { enabled: true, context_status: true },
    });
  });
});

describe('stripUndefined', () => {
  it('keeps null values', () => {
    expect(stripUndefined({ a: null, b: undefined, c: 1 })).toEqual({
      a: null,
      c: 1,
    });
  });

  it('recurses into arrays', () => {
    expect(stripUndefined([{ a: undefined, b: 2 }])).toEqual([{ b: 2 }]);
  });
});
