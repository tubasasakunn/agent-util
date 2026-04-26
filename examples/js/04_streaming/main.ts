/** Use runStream() AsyncIterable to render deltas as they arrive. */

import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

import { Agent } from '@ai-agent/sdk';

const HERE = dirname(fileURLToPath(import.meta.url));
const AGENT_BINARY = resolve(HERE, '../../../agent');

const env = {
  SLLM_ENDPOINT: process.env.SLLM_ENDPOINT ?? 'http://localhost:8080/v1/chat/completions',
  SLLM_API_KEY: process.env.SLLM_API_KEY ?? 'sk-gemma4',
};

async function main(): Promise<void> {
  await using agent = await Agent.open({ binaryPath: AGENT_BINARY, env });
  await agent.configure({
    max_turns: 4,
    streaming: { enabled: true, context_status: true },
  });

  // for-await over StreamEvent: 'delta' | 'status' | 'end'.
  for await (const ev of agent.runStream('100文字くらいで自己紹介して。')) {
    if (ev.kind === 'delta') {
      process.stdout.write(ev.text);
    } else if (ev.kind === 'status') {
      process.stderr.write(
        `\n[ctx ${ev.tokenCount}/${ev.tokenLimit} = ${(ev.usageRatio * 100).toFixed(0)}%]\n`,
      );
    } else if (ev.kind === 'end') {
      console.log('\n---');
      console.log('final reason:', ev.result.reason, '| turns:', ev.result.turns);
    }
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
