/**
 * Mirrors `internal/llm/fixjson_test.go` 1-for-1 to keep the TS port honest.
 */

import { describe, expect, it } from 'vitest';
import {
  fixJson,
  fixNullBraces,
  fixSingleQuotes,
  fixTrailingCommas,
  fixUnmatchedBrackets,
  stripCodeBlock,
} from '../src/engine/jsonfix.js';

describe('fixJson', () => {
  const cases: Array<[string, string, string]> = [
    ['valid_json', '{"key": "value"}', '{"key": "value"}'],
    ['valid_array', '[1, 2, 3]', '[1, 2, 3]'],

    ['null_braces', '{"arguments": {null}}', '{"arguments": null}'],
    [
      'null_braces_multiple',
      '{"a": {null}, "b": {null}}',
      '{"a": null, "b": null}',
    ],

    ["single_quotes", "{'key': 'value'}", '{"key": "value"}'],
    ["single_quotes_nested", "{'a': {'b': 'c'}}", '{"a": {"b": "c"}}'],

    ['trailing_comma_object', '{"a": 1, "b": 2,}', '{"a": 1, "b": 2}'],
    ['trailing_comma_array', '[1, 2, 3,]', '[1, 2, 3]'],
    ['trailing_comma_whitespace', '{"a": 1 , }', '{"a": 1  }'],

    ['missing_close_brace', '{"key": "value"', '{"key": "value"}'],
    ['missing_close_bracket', '[1, 2, 3', '[1, 2, 3]'],
    ['missing_nested', '{"a": [1, 2', '{"a": [1, 2]}'],

    ['null_byte', '{"a": "hello\x00world"}', '{"a": "helloworld"}'],

    [
      'real_router_response',
      '{"tool": "none", "arguments": {null}, "reasoning": "テスト"}',
      '{"tool": "none", "arguments": null, "reasoning": "テスト"}',
    ],

    ['combined_null_and_trailing', '{"a": {null},}', '{"a": null}'],

    [
      'code_block_json',
      '```json\n{"tool": "none", "arguments": {}}\n```',
      '{"tool": "none", "arguments": {}}',
    ],
    [
      'code_block_no_lang',
      '```\n{"tool": "read_file"}\n```',
      '{"tool": "read_file"}',
    ],

    [
      'concatenated_objects',
      '{"tool": "echo", "arguments": {"message": "hello"}}\n{"reasoning": "user wants echo"}',
      '{"arguments":{"message":"hello"},"reasoning":"user wants echo","tool":"echo"}',
    ],
    [
      'single_valid_object',
      '{"tool": "echo", "arguments": {"message": "hello"}, "reasoning": "test"}',
      '{"tool": "echo", "arguments": {"message": "hello"}, "reasoning": "test"}',
    ],
  ];

  for (const [name, input, want] of cases) {
    it(name, () => {
      expect(fixJson(input)).toBe(want);
    });
  }
});

describe('fixNullBraces', () => {
  it('basic', () => expect(fixNullBraces('{null}')).toBe('null'));
  it('in object', () =>
    expect(fixNullBraces('{"a": {null}}')).toBe('{"a": null}'));
  it('no match preserved', () =>
    expect(fixNullBraces('{"a": null}')).toBe('{"a": null}'));
});

describe('fixSingleQuotes', () => {
  it('basic', () =>
    expect(fixSingleQuotes("{'key': 'val'}")).toBe('{"key": "val"}'));
  it('already double', () =>
    expect(fixSingleQuotes('{"key": "val"}')).toBe('{"key": "val"}'));
});

describe('fixTrailingCommas', () => {
  it('object', () => expect(fixTrailingCommas('{"a": 1,}')).toBe('{"a": 1}'));
  it('array', () => expect(fixTrailingCommas('[1,]')).toBe('[1]'));
  it('with space', () =>
    expect(fixTrailingCommas('{"a": 1, }')).toBe('{"a": 1 }'));
  it('no trailing', () =>
    expect(fixTrailingCommas('{"a": 1}')).toBe('{"a": 1}'));
  it('comma in string', () =>
    expect(fixTrailingCommas('{"a": "1,}"}')).toBe('{"a": "1,}"}'));
});

describe('fixUnmatchedBrackets', () => {
  it('missing brace', () =>
    expect(fixUnmatchedBrackets('{"a": 1')).toBe('{"a": 1}'));
  it('missing bracket', () => expect(fixUnmatchedBrackets('[1, 2')).toBe('[1, 2]'));
  it('missing nested', () =>
    expect(fixUnmatchedBrackets('{"a": [1')).toBe('{"a": [1]}'));
  it('balanced', () =>
    expect(fixUnmatchedBrackets('{"a": 1}')).toBe('{"a": 1}'));
  it('bracket in string', () =>
    expect(fixUnmatchedBrackets('{"a": "{"}')).toBe('{"a": "{"}'));
});

describe('stripCodeBlock', () => {
  it('preserves non-fenced input', () =>
    expect(stripCodeBlock('{"a": 1}')).toBe('{"a": 1}'));
  it('strips ```json fence', () =>
    expect(stripCodeBlock('```json\n{"a": 1}\n```')).toBe('{"a": 1}'));
});
