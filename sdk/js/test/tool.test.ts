/** Tests for the `tool()` helper and `coerceToolResult` normalisation. */

import { describe, it, expect } from 'vitest';
import {
  tool,
  toolToProtocolDict,
  coerceToolResult,
  type ToolDefinition,
} from '../src/tool.js';

describe('tool()', () => {
  it('builds a ToolDefinition with the supplied fields', () => {
    const def = tool({
      name: 'read_file',
      description: 'Read a file',
      parameters: {
        type: 'object',
        properties: { path: { type: 'string' } },
        required: ['path'],
      },
      readOnly: true,
      handler: ({ path }) => `contents of ${path as string}`,
    });
    expect(def.name).toBe('read_file');
    expect(def.description).toBe('Read a file');
    expect(def.readOnly).toBe(true);
    expect(typeof def.handler).toBe('function');
  });

  it('defaults readOnly to false', () => {
    const def = tool({
      name: 'noop',
      description: '',
      parameters: { type: 'object' },
      handler: () => 'ok',
    });
    expect(def.readOnly).toBe(false);
  });

  it('throws if name is missing', () => {
    expect(() =>
      // @ts-expect-error -- intentional bad input
      tool({ description: 'x', parameters: {}, handler: () => '' }),
    ).toThrow(TypeError);
  });

  it('throws if handler is not a function', () => {
    expect(() =>
      tool({
        name: 'bad',
        description: '',
        parameters: { type: 'object' },
        // @ts-expect-error -- intentional bad input
        handler: 42,
      }),
    ).toThrow(TypeError);
  });

  it('toolToProtocolDict produces the wire shape', () => {
    const def: ToolDefinition = tool({
      name: 'echo',
      description: 'd',
      parameters: { type: 'object' },
      readOnly: true,
      handler: () => 'ok',
    });
    const proto = toolToProtocolDict(def);
    expect(Object.keys(proto).sort()).toEqual(
      ['description', 'name', 'parameters', 'read_only'].sort(),
    );
    expect(proto.read_only).toBe(true);
  });
});

describe('coerceToolResult', () => {
  it('wraps a string', () => {
    expect(coerceToolResult('hi')).toEqual({ content: 'hi', is_error: false });
  });

  it('passes through a structured result', () => {
    expect(coerceToolResult({ content: 'x', is_error: true })).toEqual({
      content: 'x',
      is_error: true,
    });
  });

  it('preserves metadata when present', () => {
    expect(
      coerceToolResult({ content: 'x', metadata: { source: 'fs' } }),
    ).toEqual({ content: 'x', metadata: { source: 'fs' } });
  });

  it('coerces numbers/booleans to strings', () => {
    expect(coerceToolResult(42)).toEqual({ content: '42', is_error: false });
    expect(coerceToolResult(true)).toEqual({ content: 'true', is_error: false });
  });

  it('handles null/undefined', () => {
    expect(coerceToolResult(null)).toEqual({ content: '', is_error: false });
    expect(coerceToolResult(undefined)).toEqual({ content: '', is_error: false });
  });
});
