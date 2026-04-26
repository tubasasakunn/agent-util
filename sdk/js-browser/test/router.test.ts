import { describe, expect, it } from 'vitest';
import { parseRouterResponse, routerStep, RouterError } from '../src/engine/router.js';
import type { ChatRequest, ChatResponse, Completer } from '../src/llm/completer.js';

function mockCompleter(content: string): Completer {
  return {
    async chatCompletion(_req: ChatRequest): Promise<ChatResponse> {
      return {
        choices: [
          {
            index: 0,
            message: { role: 'assistant', content },
            finish_reason: 'stop',
          },
        ],
      };
    },
  };
}

describe('parseRouterResponse', () => {
  it('parses a clean JSON object', () => {
    const r = parseRouterResponse(
      '{"tool":"echo","arguments":{"x":1},"reasoning":"because"}',
    );
    expect(r.tool).toBe('echo');
    expect(r.arguments).toEqual({ x: 1 });
    expect(r.reasoning).toBe('because');
  });

  it('falls through to "none" when tool is missing/empty', () => {
    const r = parseRouterResponse('{"tool":"","arguments":{}}');
    expect(r.tool).toBe('none');
  });

  it('repairs broken JSON via fixJson', () => {
    const r = parseRouterResponse(
      "```json\n{'tool':'echo','arguments':{null},'reasoning':'x',}\n```",
    );
    expect(r.tool).toBe('echo');
    expect(r.arguments).toEqual({});
    expect(r.reasoning).toBe('x');
  });

  it('throws RouterError on non-object output', () => {
    expect(() => parseRouterResponse('"hello"')).toThrow(RouterError);
  });

  it('reparses arguments string when SLM emits JSON-encoded args', () => {
    const r = parseRouterResponse(
      '{"tool":"echo","arguments":"{\\"x\\":2}","reasoning":""}',
    );
    expect(r.arguments).toEqual({ x: 2 });
  });
});

describe('routerStep', () => {
  it('hits the completer in JSON mode', async () => {
    let captured: ChatRequest | null = null;
    const llm: Completer = {
      async chatCompletion(req): Promise<ChatResponse> {
        captured = req;
        return {
          choices: [
            {
              index: 0,
              message: {
                role: 'assistant',
                content: '{"tool":"none","arguments":{},"reasoning":"r"}',
              },
              finish_reason: 'stop',
            },
          ],
        };
      },
    };
    const r = await routerStep(llm, 'sys', [
      { role: 'user', content: 'hi' },
    ]);
    expect(r.tool).toBe('none');
    expect(captured).not.toBeNull();
    expect(captured!.response_format).toEqual({ type: 'json_object' });
    // System prompt is prepended.
    expect(captured!.messages[0]).toEqual({ role: 'system', content: 'sys' });
    expect(captured!.messages[1]).toEqual({ role: 'user', content: 'hi' });
  });

  it('treats malformed JSON as RouterError', async () => {
    const llm = mockCompleter('not json at all');
    await expect(
      routerStep(llm, 'sys', [{ role: 'user', content: 'hi' }]),
    ).rejects.toThrow(RouterError);
  });
});
