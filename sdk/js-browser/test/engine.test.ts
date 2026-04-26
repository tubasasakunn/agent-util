/**
 * End-to-end loop tests against a scripted mock completer.
 *
 * The mock returns whatever the test queues up. Router-stage requests are
 * detected by `response_format: 'json_object'`; chat-stage requests by its
 * absence. This lets one test drive a full router -> tool -> chat cycle.
 */

import { describe, expect, it } from 'vitest';
import { Agent } from '../src/agent.js';
import { tool } from '../src/tool.js';
import { inputGuard, outputGuard, verifier } from '../src/guards.js';
import type {
  ChatRequest,
  ChatResponse,
  Completer,
} from '../src/llm/completer.js';
import type { LoopEvent } from '../src/engine/loop.js';

interface ScriptedReply {
  isRouter: boolean;
  content: string;
}

class ScriptedCompleter implements Completer {
  private queue: ScriptedReply[] = [];
  public readonly seen: ChatRequest[] = [];
  ready = true;

  push(reply: ScriptedReply): void {
    this.queue.push(reply);
  }

  pushRouter(decision: {
    tool: string;
    arguments?: Record<string, unknown>;
    reasoning?: string;
  }): void {
    this.push({
      isRouter: true,
      content: JSON.stringify({
        tool: decision.tool,
        arguments: decision.arguments ?? {},
        reasoning: decision.reasoning ?? '',
      }),
    });
  }

  pushChat(text: string): void {
    this.push({ isRouter: false, content: text });
  }

  async chatCompletion(req: ChatRequest): Promise<ChatResponse> {
    this.seen.push(req);
    const isRouter = req.response_format?.type === 'json_object';
    let next: ScriptedReply | undefined;
    for (let i = 0; i < this.queue.length; i++) {
      if (this.queue[i].isRouter === isRouter) {
        next = this.queue.splice(i, 1)[0];
        break;
      }
    }
    if (!next) {
      throw new Error(
        `ScriptedCompleter: no scripted reply for ${isRouter ? 'router' : 'chat'} step`,
      );
    }
    return {
      choices: [
        {
          index: 0,
          message: { role: 'assistant', content: next.content },
          finish_reason: 'stop',
        },
      ],
      usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
    };
  }
}

describe('Agent end-to-end', () => {
  it('chat-only path (no tools registered)', async () => {
    const llm = new ScriptedCompleter();
    llm.pushChat('hi there');
    const agent = new Agent({ llm });
    const r = await agent.run('hello');
    expect(r.response).toBe('hi there');
    expect(r.reason).toBe('completed');
    expect(r.turns).toBe(1);
  });

  it('router selects tool then chat finalises', async () => {
    const llm = new ScriptedCompleter();
    const echo = tool<{ message: string }>({
      name: 'echo',
      description: 'echoes input',
      parameters: {
        type: 'object',
        properties: { message: { type: 'string' } },
        required: ['message'],
      },
      readOnly: true,
      handler: ({ message }) => `echoed: ${message}`,
    });

    const agent = new Agent({ llm });
    agent.registerTools(echo);

    llm.pushRouter({ tool: 'echo', arguments: { message: 'hi' } });
    llm.pushRouter({ tool: 'none' });
    llm.pushChat('done!');

    const r = await agent.run('please echo hi');
    expect(r.response).toBe('done!');
    expect(r.reason).toBe('completed');
    // 2 turns: turn 1 ran the tool, turn 2 finalised via chat.
    expect(r.turns).toBe(2);
  });

  it('input guard deny short-circuits', async () => {
    const llm = new ScriptedCompleter();
    const banned = inputGuard('banned', (input) => {
      if (input.includes('evil')) return { decision: 'deny', reason: 'no' };
      return { decision: 'allow' };
    });
    const agent = new Agent({ llm });
    agent.registerGuards(banned);
    await agent.configure({ guards: { input: ['banned'] } });
    const r = await agent.run('this is evil');
    expect(r.reason).toBe('input_denied');
  });

  it('output guard deny replaces the response', async () => {
    const llm = new ScriptedCompleter();
    llm.pushChat('here is sk-1234567890abcdef1234567890abcdef');
    const agent = new Agent({ llm });
    await agent.configure({ guards: { output: ['secret_leak'] } });
    const r = await agent.run('say something');
    expect(r.response).toBe('I cannot provide that response.');
  });

  it('permission denial blocks a non-readonly tool', async () => {
    const llm = new ScriptedCompleter();
    const writer = tool({
      name: 'write_file',
      description: 'writes',
      parameters: { type: 'object' },
      readOnly: false,
      handler: () => 'wrote',
    });
    const agent = new Agent({ llm });
    agent.registerTools(writer);
    await agent.configure({
      max_turns: 4,
      permission: { enabled: true, allow: [], deny: [] },
    });
    llm.pushRouter({ tool: 'write_file', arguments: {} });
    llm.pushRouter({ tool: 'none' });
    llm.pushChat('finished');
    const events: LoopEvent[] = [];
    const r = await agent.run('do it', { onEvent: (e) => void events.push(e) });
    expect(r.response).toBe('finished');
    const perm = events.find((e) => e.kind === 'permission');
    expect(perm).toBeDefined();
    expect((perm as { decision: string }).decision).toBe('denied');
  });

  it('verifier failure causes a retry turn', async () => {
    const llm = new ScriptedCompleter();
    const t = tool({
      name: 'fetch',
      description: '',
      parameters: { type: 'object' },
      readOnly: true,
      handler: () => '   ', // whitespace -> non_empty fails
    });
    const agent = new Agent({ llm });
    agent.registerTools(t);
    await agent.configure({
      max_turns: 5,
      verify: { verifiers: ['non_empty'], max_consecutive_failures: 5 },
    });

    llm.pushRouter({ tool: 'fetch' });
    llm.pushRouter({ tool: 'none' });
    llm.pushChat('giving up');

    const r = await agent.run('do');
    expect(r.response).toBe('giving up');
  });

  it('runStream yields router and end events', async () => {
    const llm = new ScriptedCompleter();
    const echo = tool({
      name: 'echo',
      description: 'echo',
      parameters: { type: 'object' },
      readOnly: true,
      handler: () => 'pong',
    });
    const agent = new Agent({ llm });
    agent.registerTools(echo);

    llm.pushRouter({ tool: 'echo' });
    llm.pushRouter({ tool: 'none' });
    llm.pushChat('all done');

    const events: string[] = [];
    let final = '';
    for await (const ev of agent.runStream('hi')) {
      if (ev.kind === 'event') events.push(ev.event.kind);
      if (ev.kind === 'end') final = ev.result.response;
    }
    expect(final).toBe('all done');
    expect(events).toContain('router');
    expect(events).toContain('tool_call');
    expect(events).toContain('tool_result');
  });

  it('custom verifier is wired through the agent', async () => {
    const llm = new ScriptedCompleter();
    const t = tool({
      name: 'fetch',
      description: '',
      parameters: { type: 'object' },
      readOnly: true,
      handler: () => 'good content',
    });
    const v = verifier('contains_good', (_n, _a, result) =>
      result.includes('good')
        ? { passed: true, summary: 'ok' }
        : { passed: false, summary: 'no good' },
    );
    const agent = new Agent({ llm });
    agent.registerTools(t);
    agent.registerVerifiers(v);
    await agent.configure({ verify: { verifiers: ['contains_good'] } });

    llm.pushRouter({ tool: 'fetch' });
    llm.pushRouter({ tool: 'none' });
    llm.pushChat('chat done');

    const r = await agent.run('hi');
    expect(r.response).toBe('chat done');
  });

  it('rejects invalid guard names at run time', async () => {
    const agent = new Agent({ llm: new ScriptedCompleter() });
    await agent.configure({ guards: { input: ['no_such_guard'] } });
    await expect(agent.run('hi')).rejects.toThrow(/guard not found/);
  });

  it('output guard wraps an agent without tools too (no router)', async () => {
    const llm = new ScriptedCompleter();
    llm.pushChat('clean text');
    const trip = outputGuard('always_trip', () => ({ decision: 'tripwire', reason: 'x' }));
    const agent = new Agent({ llm });
    agent.registerGuards(trip);
    await agent.configure({ guards: { output: ['always_trip'] } });
    await expect(agent.run('hi')).rejects.toMatchObject({ name: 'GuardDenied' });
  });
});
