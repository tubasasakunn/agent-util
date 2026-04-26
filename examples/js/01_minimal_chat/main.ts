/** Smallest possible ai-agent run in TypeScript. */

import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

import { Agent } from '@ai-agent/sdk';

// Resolve ../../../agent relative to this file (examples/js/<name>/main.ts).
const HERE = dirname(fileURLToPath(import.meta.url));
const AGENT_BINARY = resolve(HERE, '../../../agent');

const env = {
  SLLM_ENDPOINT: process.env.SLLM_ENDPOINT ?? 'http://localhost:8080/v1/chat/completions',
  SLLM_API_KEY: process.env.SLLM_API_KEY ?? 'sk-gemma4',
};

async function main(): Promise<void> {
  // `await using` ensures close() runs even on error.
  await using agent = await Agent.open({ binaryPath: AGENT_BINARY, env });
  const result = await agent.run('こんにちは。今日の天気はどう？一文で答えて。');
  console.log('response:', result.response);
  console.log('reason:  ', result.reason);
  console.log('turns:   ', result.turns);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
