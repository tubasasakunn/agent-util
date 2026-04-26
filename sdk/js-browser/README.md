# @ai-agent/browser

**Pure-TypeScript browser SDK for ai-agent.** Runs the full agent loop
(router -> tools -> guards -> verifiers -> output) in the browser, against
any `Completer` you plug in. The default backend is
[WebLLM](https://github.com/mlc-ai/web-llm), which downloads a small
quantised LLM (Gemma, Llama 3.2, Qwen 2.5, ...) into IndexedDB and runs
it on the user's GPU via WebGPU. **No server. No API key. No subprocess.**

```ts
import { Agent, tool } from '@ai-agent/browser';
import { WebLLMCompleter } from '@ai-agent/browser/llm';

const llm = new WebLLMCompleter({ model: 'gemma-2-2b-it-q4f16_1-MLC' });
await llm.load((p) => console.log(p.text, p.progress));

const agent = new Agent({ llm });
await agent.configure({
  max_turns: 8,
  guards: { input: ['prompt_injection'], output: ['secret_leak'] },
});

agent.registerTools(
  tool({
    name: 'fetch_url',
    description: 'Fetch a URL and return the body text.',
    parameters: {
      type: 'object',
      properties: { url: { type: 'string' } },
      required: ['url'],
      additionalProperties: false,
    },
    readOnly: true,
    handler: async ({ url }) => (await fetch(String(url))).text(),
  }),
);

const result = await agent.run('Summarise https://example.com');
console.log(result.response);
```

## Install

```bash
npm install @ai-agent/browser @mlc-ai/web-llm
```

`@mlc-ai/web-llm` is a peer dependency — installed only if you want the
WebLLM backend. You can substitute any other `Completer` (OpenAI proxy,
mock, ...) without it.

## Requirements

| Capability     | What it needs                                                                |
| -------------- | ---------------------------------------------------------------------------- |
| WebGPU         | Chrome / Edge **113+**, Safari **17.4+** (macOS), or any Chromium with `chrome://flags#enable-unsafe-webgpu` |
| IndexedDB      | All modern browsers (used to cache model weights)                            |
| Memory         | 1-3 GB free RAM depending on model size                                      |
| Bandwidth      | 100 MB-1.5 GB **first download per model**, then cached forever              |

Smaller models (Qwen 2.5 0.5B, Llama 3.2 1B) work on integrated GPUs and
phones; Gemma 2 2B is a comfortable laptop default.

## Quickstart 1 — minimal

```ts
import { Agent } from '@ai-agent/browser';
import { WebLLMCompleter } from '@ai-agent/browser/llm';

const llm = new WebLLMCompleter({ model: 'qwen2.5-0.5b-instruct-q4f16_1-MLC' });
await llm.load();
const agent = new Agent({ llm });
const r = await agent.run('Say hi in 3 words.');
console.log(r.response);
```

## Quickstart 2 — with a tool

```ts
import { Agent, tool } from '@ai-agent/browser';

const calc = tool<{ a: number; b: number }>({
  name: 'add',
  description: 'Add two integers and return the sum.',
  parameters: {
    type: 'object',
    properties: { a: { type: 'integer' }, b: { type: 'integer' } },
    required: ['a', 'b'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: ({ a, b }) => String(a + b),
});

agent.registerTools(calc);
const r = await agent.run('What is 17 + 25?');
console.log(r.response); // -> "42" or a sentence containing it
```

## Quickstart 3 — with guards and streaming

```ts
import { Agent, inputGuard } from '@ai-agent/browser';

agent.registerGuards(
  inputGuard('no_secrets', (input) =>
    input.toLowerCase().includes('password')
      ? { decision: 'deny', reason: 'looks like a secret' }
      : { decision: 'allow' },
  ),
);

await agent.configure({
  max_turns: 6,
  streaming: { enabled: true },
  guards: {
    input: ['prompt_injection', 'no_secrets'],
    output: ['secret_leak'],
    tool_call: ['dangerous_shell'],
  },
});

for await (const ev of agent.runStream('Walk me through the README.')) {
  if (ev.kind === 'delta') document.body.append(ev.text);
  else if (ev.kind === 'event' && ev.event.kind === 'router') {
    console.log('router picked', ev.event.decision.tool);
  } else if (ev.kind === 'end') {
    console.log('reason', ev.result.reason);
  }
}
```

## WebLLM model selection

```ts
new WebLLMCompleter({
  model: 'gemma-2-2b-it-q4f16_1-MLC',
  // optional defaults
  temperature: 0.7,
  // pass-throughs to @mlc-ai/web-llm CreateMLCEngine
  engineConfig: { logLevel: 'INFO' },
});
```

Tested with WebLLM 0.2.x. Recommended starter models:

| Model id (WebLLM)                                | Size  | Best for                                     |
| ------------------------------------------------ | ----- | -------------------------------------------- |
| `qwen2.5-0.5b-instruct-q4f16_1-MLC`              | ~400MB | Low-end laptops, phones, fastest cold start |
| `llama-3.2-1b-instruct-q4f16_1-MLC`              | ~700MB | Balanced quality/size                       |
| `gemma-2-2b-it-q4f16_1-MLC`                      | ~1.5GB | Good default for routing + tool use         |
| `Llama-3.2-3B-Instruct-q4f16_1-MLC`              | ~2GB   | Better instruction following                |

Models are cached in IndexedDB; subsequent loads are near-instant.

## Built-in guards and verifiers

Same names, same behaviour as the Go core. Reference them by string in
`agent.configure({ guards: { ... }, verify: { verifiers: [...] } })`:

| Stage    | Name              | What it does                                                  |
| -------- | ----------------- | ------------------------------------------------------------- |
| input    | `prompt_injection`| Blocks `ignore previous`, `you are now`, `system:` patterns  |
| input    | `max_length`      | Denies inputs over 50000 characters                          |
| tool_call| `dangerous_shell` | Blocks `rm -rf /`, fork bombs, `mkfs`, `dd of=/dev/...`      |
| output   | `secret_leak`     | Denies `sk-...`, `ghp_...`, `AKIA...`, RSA private keys      |
| verifier | `non_empty`       | Fails when the tool result is empty / whitespace             |
| verifier | `json_valid`      | When result starts with `{`/`[`, requires it to be valid JSON|

Custom guards / verifiers register the same way as the Node SDK:

```ts
import { inputGuard, verifier } from '@ai-agent/browser';

const myGuard = inputGuard('no_pii', (input) => /* ... */);
agent.registerGuards(myGuard);
await agent.configure({ guards: { input: ['no_pii'] } });
```

## Differences vs `@ai-agent/sdk` (Node)

| Feature                       | Node SDK | Browser SDK |
| ----------------------------- | -------- | ----------- |
| `Agent.run` / `runStream`     | ✓        | ✓           |
| `Agent.configure`             | ✓        | ✓ (subset)  |
| `tool()`, `inputGuard()` ...  | ✓        | ✓           |
| Built-in guards / verifiers   | ✓        | ✓           |
| `delegate_task` (sub-agents)  | ✓        | not implemented |
| `coordinate_tasks`            | ✓        | not implemented |
| MCP integration               | ✓        | not implemented |
| Worktree / SessionRunner      | ✓        | not implemented |
| LLM-summariser compaction     | ✓        | basic snip only |
| Permission `ask` step         | ✓        | not implemented (fail-closed) |
| Audit log writer              | ✓        | not implemented |

The features marked "not implemented" require either a subprocess
(MCP, worktrees) or an interactive UI (permission `ask`) that doesn't have
a single right shape in the browser. If you need any of them, run the Go
core via the Node SDK on a server and proxy from the browser.

## Architecture

```
Agent  ─┬─ Completer (Completer interface — WebLLM, mock, custom)
        ├─ AgentLoop
        │    ├─ History       (token estimation, snip)
        │    ├─ Router step   (JSON mode, fixJson recovery)
        │    ├─ Tool registry + permission check + tool_call guards
        │    ├─ Tool execute  (your handler runs in the browser tab)
        │    ├─ Verifiers     (non_empty / json_valid / custom)
        │    └─ Chat step     (final answer + output guards + streaming)
        └─ Built-in guard / verifier factories (by name)
```

## Tests

```bash
cd sdk/js-browser
npm install
npm run build
npm test
```

The tests use a `ScriptedCompleter` mock — no WebGPU, no model download.

## License

MIT (matches the rest of ai-agent).
