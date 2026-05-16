/**
 * End-to-end test against a real `agent --rpc` subprocess.
 *
 * Only runs when the `AGENT_BINARY` environment variable points to a built
 * agent binary; otherwise the suite is skipped.
 *
 *   go build -o agent ./cmd/agent/
 *   AGENT_BINARY=$(pwd)/agent npm test
 *
 * The test does not actually call out to a real SLLM (which would require a
 * running model server); it just exercises `configure` and `abort` to confirm
 * the binary speaks JSON-RPC properly.
 */

import { describe, it, expect } from 'vitest';
import { existsSync } from 'node:fs';
import { Agent } from '../src/client.js';

const AGENT_BINARY = process.env.AGENT_BINARY;
const RUN = !!AGENT_BINARY && existsSync(AGENT_BINARY);

describe.skipIf(!RUN)('e2e', () => {
  it('configure round-trips against the real binary', async () => {
    const agent = new Agent({ binaryPath: AGENT_BINARY! });
    await agent.start();
    try {
      const applied = await agent.configure({
        max_turns: 3,
        streaming: { enabled: true, context_status: true },
      });
      expect(applied).toContain('max_turns');
      expect(applied).toContain('streaming');
    } finally {
      await agent.close();
    }
  });

  it('abort returns false when no run is in progress', async () => {
    const agent = new Agent({ binaryPath: AGENT_BINARY! });
    await agent.start();
    try {
      const ok = await agent.abort('test');
      expect(ok).toBe(false);
    } finally {
      await agent.close();
    }
  });

  it('llm.mode=remote routes ChatCompletion to setLLMHandler', async () => {
    const agent = new Agent({
      binaryPath: AGENT_BINARY!,
      env: { SLLM_ENDPOINT: 'http://127.0.0.1:1/nonexistent' },
    });
    await agent.start();

    const calls: Array<Record<string, unknown>> = [];
    agent.setLLMHandler((req) => {
      calls.push(req);
      const messages = (req.messages ?? []) as Array<{ role?: string; content?: string }>;
      const lastUser =
        [...messages].reverse().find((m) => m.role === 'user')?.content ?? '(none)';
      const responseFormat = (req.response_format ?? null) as { type?: string } | null;
      const content =
        responseFormat?.type === 'json_object'
          ? '{"tool":"none","arguments":{},"reasoning":"fake"}'
          : `JS-FAKE: ${lastUser}`;
      return {
        id: 'fake-js',
        object: 'chat.completion',
        created: 0,
        model: String(req.model ?? 'fake'),
        choices: [
          { index: 0, message: { role: 'assistant', content }, finish_reason: 'stop' },
        ],
        usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
      };
    });

    try {
      await agent.configure({ max_turns: 2, llm: { mode: 'remote' } });
      const result = await agent.run('ping');
      expect(calls.length).toBeGreaterThan(0);
      expect(result.response).toContain('JS-FAKE');
    } finally {
      await agent.close();
    }
  });
});
