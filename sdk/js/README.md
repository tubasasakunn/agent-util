# ai-agent TypeScript SDK

Thin TypeScript / JavaScript client for the [ai-agent](../../) Go harness.
Speaks JSON-RPC 2.0 over stdio with a child `agent --rpc` process.

- **Zero runtime dependencies** — only Node.js built-ins (`node:child_process`,
  `node:readline`).
- ESM-only; ships `.js` + `.d.ts` from `dist/`.
- Mirrors `pkg/protocol/methods.go` and `docs/openrpc.json` exactly.
- AsyncIterable streaming for `for await` ergonomics.

## Install

```bash
# 1) Build the Go agent binary (from the repo root)
go build -o agent ./cmd/agent/

# 2) Build the SDK (from sdk/js/)
cd sdk/js
npm install
npm run build
```

To consume from another project locally:

```bash
# In a sibling project
npm install ../ai-agent/sdk/js
# or with pnpm
pnpm link ../ai-agent/sdk/js
```

Requires Node.js 20+. Bun and Deno should work because the SDK only uses
Node-compatible built-ins, but only Node.js is part of CI for now (see
[Bun / Deno](#bun--deno) below).

## Quickstart

### 1. Minimal run

```ts
import { Agent } from '@ai-agent/sdk';

const agent = new Agent({ binaryPath: './agent' });
await agent.start();
try {
  const result = await agent.run('こんにちは');
  console.log(result.response);
} finally {
  await agent.close();
}
```

`agent.run(prompt)` returns an `AgentResult` with `.response`, `.reason`,
`.turns` and `.usage` (`prompt_tokens`, `completion_tokens`, `total_tokens`).

If your runtime supports TC39 explicit resource management, `await using`
shortens the lifecycle:

```ts
await using agent = await Agent.open({ binaryPath: './agent' });
const r = await agent.run('hi');
console.log(r.response);
```

### 2. Register a tool

```ts
import { readFile } from 'node:fs/promises';
import { Agent, tool } from '@ai-agent/sdk';

const readFileTool = tool<{ path: string }>({
  name: 'read_file',
  description: 'Read a UTF-8 text file from the workspace.',
  parameters: {
    type: 'object',
    properties: { path: { type: 'string' } },
    required: ['path'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: async ({ path }) => readFile(path, 'utf8'),
});

const agent = new Agent({ binaryPath: './agent' });
await agent.start();
try {
  await agent.registerTools(readFileTool);
  const r = await agent.run('Read README.md and summarise it.');
  console.log(r.response);
} finally {
  await agent.close();
}
```

The handler receives the parsed `args` object (typed via the `tool<P>()`
generic). Return a `string`, a structured `{ content, is_error?, metadata? }`
object, or any value that will be coerced via `String(...)`.

### 3. Configure guards / permissions / streaming

```ts
import { Agent, inputGuard } from '@ai-agent/sdk';

const noSecrets = inputGuard('no_secrets', (input) => {
  if (input.toLowerCase().includes('password')) {
    return { decision: 'deny', reason: 'looks like a secret' };
  }
  return { decision: 'allow' };
});

const agent = new Agent({ binaryPath: './agent' });
await agent.start();
try {
  await agent.registerGuards(noSecrets);
  await agent.configure({
    max_turns: 8,
    permission: { enabled: true, allow: ['read_file'] },
    guards: { input: ['no_secrets'] },
    streaming: { enabled: true },
  });

  // for-await streaming
  for await (const ev of agent.runStream('Walk me through the README.')) {
    if (ev.kind === 'delta') process.stdout.write(ev.text);
    else if (ev.kind === 'end') console.log('\n---\n', ev.result.reason);
  }
} finally {
  await agent.close();
}
```

## API surface

```ts
class Agent {
  constructor(opts?: AgentOptions);
  static open(opts?: AgentOptions): Promise<Agent>;

  start(): Promise<void>;
  close(): Promise<void>;
  [Symbol.asyncDispose](): Promise<void>;

  configure(config: AgentConfig): Promise<string[]>;
  run(prompt: string, opts?: RunOptions): Promise<AgentResult>;
  runStream(prompt: string, opts?: RunOptions): AsyncIterable<StreamEvent>;
  abort(reason?: string): Promise<boolean>;

  registerTools(...tools: ToolDefinition[]): Promise<number>;
  registerGuards(...guards: GuardDefinition[]): Promise<number>;
  registerVerifiers(...verifiers: VerifierDefinition[]): Promise<number>;
  registerMCP(opts: MCPOptions): Promise<string[]>;

  readonly stderrOutput: string;
}

function tool<P>(opts: ToolOptions<P>): ToolDefinition<P>;
function inputGuard(name: string, fn: InputGuardFn): GuardDefinition;
function toolCallGuard(name: string, fn: ToolCallGuardFn): GuardDefinition;
function outputGuard(name: string, fn: OutputGuardFn): GuardDefinition;
function verifier(name: string, fn: VerifierFn): VerifierDefinition;
```

`AgentConfig` and the nested `*Config` interfaces use `snake_case` keys so
they pass straight through to the Go core (matching the Python SDK).
Undefined fields are stripped before serialisation.

### Errors

| Class           | JSON-RPC code       | When                                        |
| --------------- | ------------------- | ------------------------------------------- |
| `AgentBusy`     | `-32002`            | `agent.run` while another run is in flight  |
| `AgentAborted`  | `-32003`            | A run was cancelled via `agent.abort`       |
| `ToolError`     | `-32000` / `-32001` | Tool not found / tool execution failed      |
| `GuardDenied`   | n/a                 | An input guard returned `deny` / `tripwire` |
| `AgentError`    | other               | Base class for everything from the SDK      |

## Tests

```bash
cd sdk/js
npm test                          # unit tests, no agent binary required

# E2E against the real binary
go build -o ../../agent ../../cmd/agent/
AGENT_BINARY="$(pwd)/../../agent" npm test
```

## Bun / Deno

The SDK uses only `node:child_process` and `node:readline`, both of which are
implemented by Bun and Deno (with the `node:` specifier). Quick smoke tests:

```bash
# Bun
bun run dist/index.js

# Deno (requires --allow-* flags for spawn / stdio)
deno run --allow-read --allow-run dist/index.js
```

The included tests are written for `vitest` and require Node.js, but the
runtime artefacts (`dist/`) should work in any of the three.

## References

- [`docs/openrpc.json`](../../docs/openrpc.json) — full OpenRPC 1.2.6 spec.
- [`docs/schemas/`](../../docs/schemas/) — JSON Schemas for every type.
- [`pkg/protocol/methods.go`](../../pkg/protocol/methods.go) — Go source of truth.
- [`sdk/python/`](../python/) — sister Python SDK (same protocol).
- ADR-001 (JSON-RPC over stdio), ADR-013 (RemoteTool adapter pattern).
