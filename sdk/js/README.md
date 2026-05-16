# ai-agent TypeScript SDK (Node)

[![npm](https://img.shields.io/badge/package-%40ai--agent%2Fsdk-cb3837)](#インストール)
[![node](https://img.shields.io/badge/node-20+-339933)](#要件)
[![protocol](https://img.shields.io/badge/JSON--RPC-2.0-green)](../../docs/openrpc.json)

`ai-agent` の Go ハーネスを TypeScript / JavaScript から薄くラップする SDK。
子プロセスとして `agent --rpc` を起動し、stdio 越しに JSON-RPC 2.0 で通信する。

- **依存ゼロ** (Node 標準の `node:child_process` / `node:readline` のみ)
- ESM のみ。`dist/` から `.js` + `.d.ts` を出力
- `pkg/protocol/methods.go` / `docs/openrpc.json` と完全一致
- `AsyncIterable` ベースのストリーミング (`for await`)

> **位置付け**: この SDK は**低レベル**寄りで、`Agent.run()` `Agent.runStream()`
> に集中している。Python / Swift にあるような `fork()` / `branch()` / `batch()` /
> `search()` などの AOM 高レベル API は未実装。AOM 相当の操作が必要なら
> `agent['_rpc'].call("session.history" or "session.inject", ...)` を直接叩く。

## 目次

1. [TL;DR](#tldr)
2. [要件](#要件)
3. [インストール](#インストール)
4. [クイックスタート](#クイックスタート)
5. [API リファレンス](#api-リファレンス)
6. [設定リファレンス](#設定リファレンス)
7. [ツール / ガード / ベリファイア](#ツール--ガード--ベリファイア)
8. [MCP 統合](#mcp-統合)
9. [ストリーミング](#ストリーミング)
10. [エラーハンドリング](#エラーハンドリング)
11. [Bun / Deno](#bun--deno)
12. [テスト](#テスト)
13. [トラブルシューティング](#トラブルシューティング)

## TL;DR

```ts
import { Agent } from '@ai-agent/sdk';

const agent = new Agent({
  binaryPath: './agent',
  env: {
    SLLM_ENDPOINT: 'http://localhost:8080/v1/chat/completions',
    SLLM_API_KEY: 'sk-xxx',
  },
});
await agent.start();
try {
  await agent.configure({ max_turns: 5, system_prompt: 'あなたは親切なアシスタントです。' });
  const r = await agent.run('こんにちは');
  console.log(r.response);
} finally {
  await agent.close();
}
```

## 要件

- Node.js 20+ / Bun / Deno (`node:` specifier 経由)
- ビルド済みの `agent` バイナリ
- SLLM サーバ (OpenAI 互換)

## インストール

```bash
# 1. Go バイナリをビルド (リポジトリルートで)
go build -o agent ./cmd/agent/

# 2. SDK をビルド
cd sdk/js
npm install
npm run build
```

他プロジェクトから利用する場合:

```bash
# 兄弟プロジェクトから
npm install ../ai-agent/sdk/js
# あるいは
pnpm link ../ai-agent/sdk/js
```

## クイックスタート

### 1. 最小実行

```ts
import { Agent } from '@ai-agent/sdk';

const agent = new Agent({ binaryPath: './agent' });
await agent.start();
try {
  const r = await agent.run('こんにちは');
  console.log(r.response);
} finally {
  await agent.close();
}
```

TC39 explicit-resource-management が使えるなら:

```ts
await using agent = await Agent.open({ binaryPath: './agent' });
const r = await agent.run('hi');
console.log(r.response);
```

### 2. ツールを登録

```ts
import { readFile } from 'node:fs/promises';
import { Agent, tool } from '@ai-agent/sdk';

const readFileTool = tool<{ path: string }>({
  name: 'read_file',
  description: 'UTF-8 のテキストファイルを読む',
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
  const r = await agent.run('README.md を要約して');
  console.log(r.response);
} finally {
  await agent.close();
}
```

handler は `args` を引数に取り、`string` / `ToolExecuteResult` / その他
(`String(...)` で文字列化) を返す。

### 3. ガード + ストリーミング

```ts
import { Agent, inputGuard } from '@ai-agent/sdk';

const noSecrets = inputGuard('no_secrets', (input) =>
  input.toLowerCase().includes('password')
    ? { decision: 'deny', reason: 'looks like a secret' }
    : { decision: 'allow' },
);

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

  for await (const ev of agent.runStream('README を案内して')) {
    if (ev.kind === 'delta') process.stdout.write(ev.text);
    else if (ev.kind === 'end') console.log('\n---', ev.result.reason);
  }
} finally {
  await agent.close();
}
```

## API リファレンス

### `Agent`

```ts
class Agent {
  constructor(opts?: AgentOptions);

  // ライフサイクル
  start(): Promise<void>;
  close(): Promise<void>;
  static open(opts?: AgentOptions): Promise<Agent>;
  [Symbol.asyncDispose](): Promise<void>;       // await using 用
  readonly stderrOutput: string;                 // 子プロセスの stderr

  // 設定 / 会話
  configure(config: AgentConfig): Promise<string[]>;
  run(prompt: string, opts?: RunOptions): Promise<AgentResult>;
  runStream(prompt: string, opts?: RunOptions): AsyncIterable<StreamEvent>;
  abort(reason?: string): Promise<boolean>;

  // 登録
  registerTools(...tools: ToolDefinition[]): Promise<number>;
  registerGuards(...guards: GuardDefinition[]): Promise<number>;
  registerVerifiers(...verifiers: VerifierDefinition[]): Promise<number>;
  registerMCP(opts: MCPOptions): Promise<string[]>;
}
```

```ts
interface AgentOptions {
  binaryPath?: string;                            // default: "agent" (PATH lookup)
  env?: Record<string, string>;                   // 子プロセスへ追加する環境変数
  cwd?: string;
  stderr?: 'inherit' | 'pipe' | 'ignore';         // default: "pipe"
}

interface RunOptions {
  maxTurns?: number;
  onDelta?: (text: string, turn: number) => void | Promise<void>;
  onStatus?: (usageRatio: number, tokenCount: number, tokenLimit: number) => void | Promise<void>;
  timeoutMs?: number;
}

interface MCPOptions {
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  transport?: 'stdio' | 'sse';
  url?: string;
}

type StreamEvent =
  | { kind: 'delta'; text: string; turn: number }
  | { kind: 'status'; usageRatio: number; tokenCount: number; tokenLimit: number }
  | { kind: 'end'; result: AgentResult };
```

### 戻り値型

```ts
interface AgentResult {
  response: string;
  reason: string;        // "completed" / "max_turns" / "aborted" / ...
  turns: number;
  usage: UsageInfo;
}

interface UsageInfo {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}
```

### ヘルパー関数

```ts
function tool<P>(opts: ToolOptions<P>): ToolDefinition<P>;
function inputGuard(name: string, fn: InputGuardFn): GuardDefinition;
function toolCallGuard(name: string, fn: ToolCallGuardFn): GuardDefinition;
function outputGuard(name: string, fn: OutputGuardFn): GuardDefinition;
function verifier(name: string, fn: VerifierFn): VerifierDefinition;
```

## 設定リファレンス

`AgentConfig` は **snake_case** キーで Go core にそのまま流れる。Python /
Swift と完全に同じ構造。`undefined` のフィールドは送信前に除去される。

```ts
interface AgentConfig {
  max_turns?: number;
  system_prompt?: string;
  token_limit?: number;
  work_dir?: string;

  delegate?: DelegateConfig;
  coordinator?: CoordinatorConfig;
  compaction?: CompactionConfig;
  permission?: PermissionConfig;
  guards?: GuardsConfig;
  verify?: VerifyConfig;
  tool_scope?: ToolScopeConfig;
  reminder?: ReminderConfig;
  streaming?: StreamingConfig;
}
```

| サブ設定             | 主なフィールド                                                   |
| -------------------- | ---------------------------------------------------------------- |
| `DelegateConfig`     | `enabled`, `max_chars`                                           |
| `CoordinatorConfig`  | `enabled`, `max_chars`                                           |
| `CompactionConfig`   | `enabled`, `budget_max_chars`, `keep_last`, `target_ratio`, `summarizer` |
| `PermissionConfig`   | `enabled`, `deny: string[]`, `allow: string[]`                   |
| `GuardsConfig`       | `input: string[]`, `tool_call: string[]`, `output: string[]`     |
| `VerifyConfig`       | `verifiers: string[]`, `max_step_retries`, `max_consecutive_failures` |
| `ToolScopeConfig`    | `max_tools`, `include_always: string[]`                          |
| `ReminderConfig`     | `threshold`, `content`                                           |
| `StreamingConfig`    | `enabled`, `context_status`                                      |

完全な型は [`src/config.ts`](./src/config.ts) を参照。

## ツール / ガード / ベリファイア

### `tool()`

```ts
const myTool = tool<{ x: number; y: number }>({
  name: 'add',
  description: '2 つの整数を加算',
  parameters: {
    type: 'object',
    properties: { x: { type: 'integer' }, y: { type: 'integer' } },
    required: ['x', 'y'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: ({ x, y }) => String(x + y),
});
```

戻り値は `string` / `{ content, is_error?, metadata? }` / 任意値 (string 化)。

### ガード

`{ decision, reason? }` を返す関数を登録する。`decision` は
`"allow" | "deny" | "tripwire"`。

```ts
import { inputGuard, toolCallGuard, outputGuard } from '@ai-agent/sdk';

const inp = inputGuard('no_secrets', (input) =>
  input.includes('password') ? { decision: 'deny', reason: '...' } : { decision: 'allow' });

const tc  = toolCallGuard('fs_root_only', (toolName, args) => /* ... */);

const out = outputGuard('pii_redactor', (output) => /* ... */);
```

登録: `agent.registerGuards(inp, tc, out)`、有効化: `configure({ guards: { input: ['no_secrets'], ... } })`。

### ベリファイア

```ts
import { verifier } from '@ai-agent/sdk';

const nonEmpty = verifier('non_empty', (toolName, args, result) => ({
  passed: result.trim().length > 0,
  summary: result.trim().length > 0 ? 'ok' : 'empty result',
}));

await agent.registerVerifiers(nonEmpty);
await agent.configure({ verify: { verifiers: ['non_empty'] } });
```

## MCP 統合

```ts
// stdio
const tools = await agent.registerMCP({
  transport: 'stdio',
  command: 'uvx',
  args: ['mcp-server-fetch'],
  env: { FOO: 'bar' },
});

// SSE
await agent.registerMCP({ transport: 'sse', url: 'https://...' });
```

返り値は MCP サーバが公開するツール名の配列。

## ストリーミング

### コールバック方式 (`run`)

```ts
await agent.run('長めの説明をして', {
  onDelta: (text, turn) => process.stdout.write(text),
  onStatus: (ratio) => console.log(`[ctx ${(ratio * 100).toFixed(0)}%]`),
});
```

### イベント方式 (`runStream`)

```ts
for await (const ev of agent.runStream('...')) {
  switch (ev.kind) {
    case 'delta':  process.stdout.write(ev.text); break;
    case 'status': console.log('ctx', ev.usageRatio); break;
    case 'end':    console.log('done', ev.result.reason); break;
  }
}
```

事前に `streaming: { enabled: true }` を `configure` で設定すること。

## エラーハンドリング

```ts
import { AgentError, AgentBusy, AgentAborted, ToolError, GuardDenied } from '@ai-agent/sdk';

try {
  await agent.run('...');
} catch (e) {
  if (e instanceof GuardDenied) { /* ... */ }
  else if (e instanceof AgentBusy) { /* ... */ }
  else if (e instanceof AgentAborted) { /* ... */ }
  else if (e instanceof ToolError) { /* ... */ }
  else if (e instanceof AgentError) { /* ... */ }
}
```

| クラス         | JSON-RPC code      | 発生条件                                       |
| -------------- | ------------------ | ---------------------------------------------- |
| `AgentBusy`    | `-32002`           | 既に別の `agent.run` が実行中                  |
| `AgentAborted` | `-32003`           | `agent.abort` でキャンセル                      |
| `ToolError`    | `-32000` / `-32001`| ツールが見つからない / 実行失敗                |
| `GuardDenied`  | `-32005`           | ガードが `deny` / `tripwire` を返した          |
| `AgentError`   | その他              | SDK 基底クラス                                  |

## Bun / Deno

`node:child_process` と `node:readline` しか使わないので、両者でも動くはず:

```bash
# Bun
bun run dist/index.js

# Deno (spawn/stdio に --allow-* が必要)
deno run --allow-read --allow-run dist/index.js
```

テストは `vitest` を使うため Node が必要。`dist/` の成果物自体は 3 ランタイム
共通で動く。

## テスト

```bash
cd sdk/js
npm test                            # ユニット (バイナリ不要)

# E2E (実バイナリ + 実 LLM)
go build -o ../../agent ../../cmd/agent/
AGENT_BINARY="$(pwd)/../../agent" npm test
```

## トラブルシューティング

| 症状                                                          | 対処                                                       |
| ------------------------------------------------------------- | ---------------------------------------------------------- |
| `ENOENT: spawn ... ENOENT`                                    | `binaryPath` が正しいか、バイナリ実行権限を確認            |
| 子プロセスが起動しない                                          | `stderr: 'inherit'` を指定して `agent --rpc` のエラーを見る |
| `for await` がブロックされる                                    | `streaming: { enabled: true }` を `configure` で設定        |
| `cannot find module '@ai-agent/sdk'`                          | `npm run build` でビルドし、`dist/index.js` の存在を確認    |
| `agent.stderrOutput` で core の標準エラー出力を取得できる        | デバッグに活用                                              |

## 参考

- [`../../docs/openrpc.json`](../../docs/openrpc.json) — OpenRPC 1.2.6 完全仕様
- [`../../docs/schemas/`](../../docs/schemas/) — 各型の JSON Schema
- [`../../pkg/protocol/methods.go`](../../pkg/protocol/methods.go) — Go 側の真実の源
- [`../README.md`](../README.md) — SDK 全体のハブ
- [`../python/`](../python/) — 兄弟 Python SDK (同じプロトコル、AOM 完全実装)
- [`../swift/`](../swift/) — 兄弟 Swift SDK (AOM 完全実装)
- ADR-001 (JSON-RPC over stdio), ADR-013 (RemoteTool アダプタ)
