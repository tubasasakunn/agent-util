/**
 * Tests for the JSON-RPC client / Agent surface using an in-memory peer.
 *
 * Rather than mocking subprocess, we drive the SDK against an in-memory
 * stdio peer that speaks the same newline-delimited JSON-RPC the real Go
 * server uses. This exercises the full client-side state machine without
 * needing the agent binary built.
 */

import { describe, it, expect } from 'vitest';
import { Agent } from '../src/client.js';
import { JsonRpcClient, type JsonRpcTransport } from '../src/jsonrpc.js';
import { AgentBusy, ToolError } from '../src/errors.js';
import { tool } from '../src/tool.js';
import { inputGuard } from '../src/guard.js';
import { verifier } from '../src/guard.js';

/** Bi-directional in-memory pipe representing the Go core peer. */
class FakePeer {
  // client <- peer (data the peer sends to the client)
  private toClient: string[] = [];
  private toClientWaiters: Array<(line: string | null) => void> = [];
  private toClientClosed = false;

  // client -> peer (data the client sends to the peer)
  private toPeer: string[] = [];
  private toPeerWaiters: Array<(line: string | null) => void> = [];
  private toPeerClosed = false;

  private feedToClient(line: string): void {
    if (this.toClientWaiters.length > 0) {
      const w = this.toClientWaiters.shift()!;
      w(line);
    } else {
      this.toClient.push(line);
    }
  }

  private feedToPeer(line: string): void {
    if (this.toPeerWaiters.length > 0) {
      const w = this.toPeerWaiters.shift()!;
      w(line);
    } else {
      this.toPeer.push(line);
    }
  }

  /** Transport handed to the JsonRpcClient (it reads from->client, writes->peer). */
  clientTransport(): JsonRpcTransport {
    return {
      readLines: () => this.makeReadable('client'),
      write: async (line: string) => {
        this.feedToPeer(line);
      },
      close: async () => {
        this.toClientClosed = true;
        for (const w of this.toClientWaiters) w(null);
        this.toClientWaiters = [];
      },
    };
  }

  private async *makeReadable(side: 'client' | 'peer'): AsyncIterable<string> {
    while (true) {
      const next = await this.takeNext(side);
      if (next === null) return;
      yield next;
    }
  }

  private async takeNext(side: 'client' | 'peer'): Promise<string | null> {
    if (side === 'client') {
      if (this.toClient.length > 0) return this.toClient.shift()!;
      if (this.toClientClosed) return null;
      return new Promise<string | null>((resolve) => this.toClientWaiters.push(resolve));
    } else {
      if (this.toPeer.length > 0) return this.toPeer.shift()!;
      if (this.toPeerClosed) return null;
      return new Promise<string | null>((resolve) => this.toPeerWaiters.push(resolve));
    }
  }

  async sendToClient(message: object): Promise<void> {
    this.feedToClient(JSON.stringify(message));
  }

  async readFromClient(): Promise<Record<string, unknown>> {
    const line = await this.takeNext('peer');
    if (line === null) throw new Error('client closed');
    return JSON.parse(line) as Record<string, unknown>;
  }

  closeToClient(): void {
    this.toClientClosed = true;
    for (const w of this.toClientWaiters) w(null);
    this.toClientWaiters = [];
  }
}

// ---------------------------------------------------------------------------
// Low-level JsonRpcClient round-trip
// ---------------------------------------------------------------------------

describe('JsonRpcClient', () => {
  it('round-trips a request/response', async () => {
    const peer = new FakePeer();
    const client = new JsonRpcClient();
    client.attach(peer.clientTransport());

    const serverP = (async () => {
      const msg = await peer.readFromClient();
      expect(msg.jsonrpc).toBe('2.0');
      expect(msg.method).toBe('agent.run');
      await peer.sendToClient({
        jsonrpc: '2.0',
        id: msg.id,
        result: {
          response: 'ok',
          reason: 'completed',
          turns: 1,
          usage: { prompt_tokens: 1, completion_tokens: 2, total_tokens: 3 },
        },
      });
    })();

    const result = (await client.call('agent.run', { prompt: 'hi' })) as {
      response: string;
    };
    await serverP;
    expect(result.response).toBe('ok');
    await client.close();
  });

  it('propagates RPC error responses as typed errors', async () => {
    const peer = new FakePeer();
    const client = new JsonRpcClient();
    client.attach(peer.clientTransport());

    const serverP = (async () => {
      const msg = await peer.readFromClient();
      await peer.sendToClient({
        jsonrpc: '2.0',
        id: msg.id,
        error: { code: -32002, message: 'agent already running' },
      });
    })();

    await expect(client.call('agent.run', { prompt: 'hi' })).rejects.toBeInstanceOf(
      AgentBusy,
    );
    await serverP;
    await client.close();
  });

  it('dispatches a core->wrapper request to the registered handler', async () => {
    const peer = new FakePeer();
    const client = new JsonRpcClient();
    client.attach(peer.clientTransport());

    client.setRequestHandler('tool.execute', async (params) => {
      const args = (params.args ?? {}) as { x?: number };
      return { content: `echo:${args.x}` };
    });

    await peer.sendToClient({
      jsonrpc: '2.0',
      id: 99,
      method: 'tool.execute',
      params: { name: 'echo', args: { x: 1 } },
    });

    const reply = await peer.readFromClient();
    expect(reply).toEqual({
      jsonrpc: '2.0',
      id: 99,
      result: { content: 'echo:1' },
    });
    await client.close();
  });

  it('dispatches notifications to the registered handler', async () => {
    const peer = new FakePeer();
    const client = new JsonRpcClient();
    client.attach(peer.clientTransport());

    const received: Record<string, unknown>[] = [];
    client.setNotificationHandler('stream.delta', (params) => {
      received.push(params);
    });

    await peer.sendToClient({
      jsonrpc: '2.0',
      method: 'stream.delta',
      params: { text: 'hello', turn: 1 },
    });

    // give the read loop a tick
    for (let i = 0; i < 50 && received.length === 0; i++) {
      await new Promise((r) => setImmediate(r));
    }
    expect(received).toEqual([{ text: 'hello', turn: 1 }]);
    await client.close();
  });
});

// ---------------------------------------------------------------------------
// Agent (high-level) using the same fake peer
// ---------------------------------------------------------------------------

async function makeAttachedAgent(): Promise<{ agent: Agent; peer: FakePeer }> {
  const peer = new FakePeer();
  const agent = new Agent({ binaryPath: 'unused' });
  agent._rpc.attach(peer.clientTransport());
  agent._wireHandlers();
  return { agent, peer };
}

describe('Agent', () => {
  it('returns a typed result from agent.run', async () => {
    const { agent, peer } = await makeAttachedAgent();

    const serverP = (async () => {
      const msg = await peer.readFromClient();
      expect(msg.method).toBe('agent.run');
      expect(msg.params).toEqual({ prompt: 'hi' });
      await peer.sendToClient({
        jsonrpc: '2.0',
        id: msg.id,
        result: {
          response: 'Hello!',
          reason: 'completed',
          turns: 2,
          usage: {
            prompt_tokens: 10,
            completion_tokens: 5,
            total_tokens: 15,
          },
        },
      });
    })();

    const result = await agent.run('hi');
    await serverP;
    expect(result.response).toBe('Hello!');
    expect(result.reason).toBe('completed');
    expect(result.turns).toBe(2);
    expect(result.usage.total_tokens).toBe(15);
    await agent.close();
  });

  it('configure sends omitempty params (undefined fields stripped)', async () => {
    const { agent, peer } = await makeAttachedAgent();

    const serverP = (async () => {
      const msg = await peer.readFromClient();
      expect(msg.method).toBe('agent.configure');
      expect(msg.params).toEqual({
        max_turns: 5,
        streaming: { enabled: true },
      });
      await peer.sendToClient({
        jsonrpc: '2.0',
        id: msg.id,
        result: { applied: ['max_turns', 'streaming'] },
      });
    })();

    const applied = await agent.configure({
      max_turns: 5,
      streaming: { enabled: true },
    });
    await serverP;
    expect(applied).toEqual(['max_turns', 'streaming']);
    await agent.close();
  });

  it('registers a tool then handles tool.execute', async () => {
    const { agent, peer } = await makeAttachedAgent();

    const add = tool<{ a: number; b: number }>({
      name: 'add',
      description: 'add',
      parameters: {
        type: 'object',
        properties: { a: { type: 'integer' }, b: { type: 'integer' } },
        required: ['a', 'b'],
      },
      readOnly: true,
      handler: ({ a, b }) => String(a + b),
    });

    const registerP = (async () => {
      const msg = await peer.readFromClient();
      expect(msg.method).toBe('tool.register');
      const params = msg.params as { tools: Array<{ name: string }> };
      expect(params.tools[0].name).toBe('add');
      await peer.sendToClient({
        jsonrpc: '2.0',
        id: msg.id,
        result: { registered: 1 },
      });
    })();

    const n = await agent.registerTools(add);
    await registerP;
    expect(n).toBe(1);

    await peer.sendToClient({
      jsonrpc: '2.0',
      id: 7,
      method: 'tool.execute',
      params: { name: 'add', args: { a: 2, b: 3 } },
    });

    const reply = await peer.readFromClient();
    expect(reply.id).toBe(7);
    const res = reply.result as { content: string; is_error?: boolean };
    expect(res.content).toBe('5');
    expect(res.is_error).toBe(false);

    await agent.close();
  });

  it('registers a guard and dispatches guard.execute (deny)', async () => {
    const { agent, peer } = await makeAttachedAgent();

    const banned = inputGuard('banned', (input) => {
      if (input.includes('evil')) return { decision: 'deny', reason: 'evil keyword' };
      return { decision: 'allow' };
    });

    const registerP = (async () => {
      const msg = await peer.readFromClient();
      expect(msg.method).toBe('guard.register');
      await peer.sendToClient({
        jsonrpc: '2.0',
        id: msg.id,
        result: { registered: 1 },
      });
    })();
    await agent.registerGuards(banned);
    await registerP;

    await peer.sendToClient({
      jsonrpc: '2.0',
      id: 11,
      method: 'guard.execute',
      params: { name: 'banned', stage: 'input', input: 'this is evil' },
    });
    const reply = await peer.readFromClient();
    expect(reply.id).toBe(11);
    expect((reply.result as { decision: string }).decision).toBe('deny');

    await agent.close();
  });

  it('registers a verifier and dispatches verifier.execute', async () => {
    const { agent, peer } = await makeAttachedAgent();

    const nonEmpty = verifier('non_empty', (_t, _a, result) => {
      if (!result.trim()) return { passed: false, summary: 'empty' };
      return { passed: true, summary: 'ok' };
    });

    const regP = (async () => {
      const msg = await peer.readFromClient();
      expect(msg.method).toBe('verifier.register');
      await peer.sendToClient({
        jsonrpc: '2.0',
        id: msg.id,
        result: { registered: 1 },
      });
    })();
    await agent.registerVerifiers(nonEmpty);
    await regP;

    await peer.sendToClient({
      jsonrpc: '2.0',
      id: 21,
      method: 'verifier.execute',
      params: { name: 'non_empty', tool_name: 'x', result: 'hello' },
    });
    const r1 = await peer.readFromClient();
    expect((r1.result as { passed: boolean }).passed).toBe(true);

    await peer.sendToClient({
      jsonrpc: '2.0',
      id: 22,
      method: 'verifier.execute',
      params: { name: 'non_empty', tool_name: 'x', result: ' ' },
    });
    const r2 = await peer.readFromClient();
    expect((r2.result as { passed: boolean }).passed).toBe(false);

    await agent.close();
  });

  it('invokes the streaming callback during run', async () => {
    const { agent, peer } = await makeAttachedAgent();
    const received: Array<[string, number]> = [];

    const serverP = (async () => {
      const msg = await peer.readFromClient();
      expect(msg.method).toBe('agent.run');
      await peer.sendToClient({
        jsonrpc: '2.0',
        method: 'stream.delta',
        params: { text: 'hello ', turn: 1 },
      });
      await peer.sendToClient({
        jsonrpc: '2.0',
        method: 'stream.delta',
        params: { text: 'world', turn: 1 },
      });
      // wait until the callback has seen both deltas
      for (let i = 0; i < 50 && received.length < 2; i++) {
        await new Promise((r) => setImmediate(r));
      }
      await peer.sendToClient({
        jsonrpc: '2.0',
        id: msg.id,
        result: {
          response: 'hello world',
          reason: 'completed',
          turns: 1,
          usage: { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 },
        },
      });
    })();

    const result = await agent.run('hi', {
      onDelta: (text, turn) => {
        received.push([text, turn]);
      },
    });
    await serverP;
    expect(result.response).toBe('hello world');
    expect(received).toEqual([
      ['hello ', 1],
      ['world', 1],
    ]);
    await agent.close();
  });

  it('runStream yields delta and end events as an async iterable', async () => {
    const { agent, peer } = await makeAttachedAgent();

    const serverP = (async () => {
      const msg = await peer.readFromClient();
      await peer.sendToClient({
        jsonrpc: '2.0',
        method: 'stream.delta',
        params: { text: 'A', turn: 1 },
      });
      await peer.sendToClient({
        jsonrpc: '2.0',
        method: 'stream.delta',
        params: { text: 'B', turn: 1 },
      });
      // Give the iterator a chance to drain the deltas.
      await new Promise((r) => setImmediate(r));
      await new Promise((r) => setImmediate(r));
      await peer.sendToClient({
        jsonrpc: '2.0',
        id: msg.id,
        result: {
          response: 'AB',
          reason: 'completed',
          turns: 1,
          usage: { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 },
        },
      });
    })();

    const events: Array<{ kind: string }> = [];
    for await (const ev of agent.runStream('hi')) {
      events.push(ev);
      if (ev.kind === 'end') break;
    }
    await serverP;

    const kinds = events.map((e) => e.kind);
    expect(kinds[kinds.length - 1]).toBe('end');
    const deltas = events.filter((e) => e.kind === 'delta');
    expect(deltas.length).toBe(2);
    await agent.close();
  });

  it('surfaces a -32001 error as ToolError', async () => {
    const { agent, peer } = await makeAttachedAgent();

    const serverP = (async () => {
      const msg = await peer.readFromClient();
      await peer.sendToClient({
        jsonrpc: '2.0',
        id: msg.id,
        error: { code: -32001, message: 'tool execution failed: x' },
      });
    })();

    await expect(agent.run('hi')).rejects.toBeInstanceOf(ToolError);
    await serverP;
    await agent.close();
  });
});
