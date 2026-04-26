/** Wrapper-side input guard that denies inputs containing 'internal-only'. */

import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

import { Agent, GuardDenied, inputGuard } from '@ai-agent/sdk';

const HERE = dirname(fileURLToPath(import.meta.url));
const AGENT_BINARY = resolve(HERE, '../../../agent');

const env = {
  SLLM_ENDPOINT: process.env.SLLM_ENDPOINT ?? 'http://localhost:8080/v1/chat/completions',
  SLLM_API_KEY: process.env.SLLM_API_KEY ?? 'sk-gemma4',
};

// inputGuard returns a GuardDefinition; the core calls back into us per run.
const internalKeyword = inputGuard('internal_keyword', (input) => {
  if (input.toLowerCase().includes('internal-only')) {
    return { decision: 'deny', reason: "input contains the 'internal-only' marker" };
  }
  return { decision: 'allow' };
});

async function main(): Promise<void> {
  await using agent = await Agent.open({ binaryPath: AGENT_BINARY, env });
  await agent.registerGuards(internalKeyword);
  await agent.configure({
    max_turns: 3,
    guards: { input: ['internal_keyword'] },
  });

  // Allowed: regular prompt.
  const ok = await agent.run('Tell me a fun fact about octopuses in one sentence.');
  console.log('[1] allowed:', ok.response.trim());

  // Denied: prompt contains the forbidden marker.
  try {
    await agent.run('This is internal-only material — summarise it.');
    console.log('[2] denied: NOT blocked (unexpected)');
  } catch (err) {
    if (err instanceof GuardDenied) console.log('[2] denied:', err.message);
    else throw err;
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
