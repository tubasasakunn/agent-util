/** Register an HTTP fetch tool and let the agent call it. */

import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

import { Agent, tool } from '@ai-agent/sdk';

const HERE = dirname(fileURLToPath(import.meta.url));
const AGENT_BINARY = resolve(HERE, '../../../agent');

const env = {
  SLLM_ENDPOINT: process.env.SLLM_ENDPOINT ?? 'http://localhost:8080/v1/chat/completions',
  SLLM_API_KEY: process.env.SLLM_API_KEY ?? 'sk-gemma4',
};

// One read-only tool: fetch a URL and return up to 4 KiB of the body.
const fetchUrl = tool({
  name: 'fetch_url',
  description: 'Download a URL over HTTPS and return up to 4 KiB of the body.',
  parameters: {
    type: 'object',
    properties: { url: { type: 'string' } },
    required: ['url'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: async (args) => {
    const url = String(args.url ?? '');
    if (!/^https?:\/\//.test(url)) throw new Error(`unsupported scheme: ${url}`);
    const res = await fetch(url, { redirect: 'follow' });
    const body = await res.text();
    return body.slice(0, 4096);
  },
});

async function main(): Promise<void> {
  await using agent = await Agent.open({ binaryPath: AGENT_BINARY, env });
  await agent.configure({
    max_turns: 6,
    permission: { enabled: true, allow: ['fetch_url'] },
  });
  await agent.registerTools(fetchUrl);

  const result = await agent.run(
    'Fetch https://example.com and tell me the page title in one sentence.',
  );
  console.log('response:', result.response.trim());
  console.log('---');
  console.log('reason:', result.reason, '| turns:', result.turns);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
