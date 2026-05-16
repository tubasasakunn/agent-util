# @ai-agent/browser

[![npm](https://img.shields.io/badge/package-%40ai--agent%2Fbrowser-cb3837)](#インストール)
[![runtime](https://img.shields.io/badge/runtime-WebGPU-orange)](#要件)
[![protocol](https://img.shields.io/badge/JSON--RPC-N%2FA-lightgrey)](#アーキテクチャ)

**ブラウザ内で完結するピュア TypeScript SDK。** ルーター → ツール → ガード →
ベリファイア → 出力という Go コアと同じエージェントループを、ブラウザの中で
動かす。LLM は任意の `Completer` 実装を差し替え可能で、デフォルトでは
[WebLLM](https://github.com/mlc-ai/web-llm) が WebGPU 上でローカル LLM を実行
する。**サーバ無し。API キー無し。サブプロセス無し。**

> 他の SDK との大きな違い: subprocess を使わず in-process で動くため、
> Node SDK 同様の Agent ループだけが移植されており、AOM の `fork()` / `batch()` /
> MCP / サブエージェントなどは未対応。

```ts
import { Agent, tool } from '@ai-agent/browser';
import { WebLLMCompleter } from '@ai-agent/browser/llm';

const llm = new WebLLMCompleter({ model: 'gemma-2-2b-it-q4f16_1-MLC' });
await llm.load((p) => console.log(p.text, p.progress));

const agent = new Agent({ llm });
await agent.configure({ max_turns: 8, guards: { input: ['prompt_injection'] } });

const r = await agent.run('Summarise https://example.com');
console.log(r.response);
```

## 目次

1. [TL;DR](#tldr)
2. [要件](#要件)
3. [インストール](#インストール)
4. [クイックスタート](#クイックスタート)
5. [API リファレンス](#api-リファレンス)
6. [Completer (LLM バックエンド)](#completer-llm-バックエンド)
7. [WebLLM モデル選択](#webllm-モデル選択)
8. [設定リファレンス](#設定リファレンス)
9. [ツール](#ツール)
10. [組み込みガード / ベリファイア](#組み込みガード--ベリファイア)
11. [ストリーミング](#ストリーミング)
12. [Node SDK との差分](#node-sdk-との差分)
13. [アーキテクチャ](#アーキテクチャ)
14. [テスト](#テスト)

## TL;DR

```ts
import { Agent } from '@ai-agent/browser';
import { WebLLMCompleter } from '@ai-agent/browser/llm';

const llm = new WebLLMCompleter({ model: 'qwen2.5-0.5b-instruct-q4f16_1-MLC' });
await llm.load();

const agent = new Agent({ llm });
console.log((await agent.run('Say hi in 3 words.')).response);
```

## 要件

| 項目         | 仕様                                                                                  |
| ------------ | ------------------------------------------------------------------------------------- |
| **WebGPU**   | Chrome / Edge 113+, Safari 17.4+ (macOS), もしくは `chrome://flags#enable-unsafe-webgpu` |
| **IndexedDB**| 全モダンブラウザ (モデル重みのキャッシュ用)                                            |
| **メモリ**   | 1-3 GB 空き RAM (モデルサイズ次第)                                                     |
| **帯域**     | 100 MB-1.5 GB **初回ダウンロードのみ** (以降は IndexedDB キャッシュから瞬時)            |

小さいモデル (Qwen 2.5 0.5B / Llama 3.2 1B) は内蔵 GPU やモバイルでも動く。
Gemma 2 2B はラップトップでの標準的なデフォルト。

## インストール

```bash
npm install @ai-agent/browser @mlc-ai/web-llm
```

`@mlc-ai/web-llm` は peer dependency。WebLLM を使わない (mock / OpenAI proxy 等
別の `Completer` を実装する) 場合は不要。

## クイックスタート

### 1. 最小実行

```ts
import { Agent } from '@ai-agent/browser';
import { WebLLMCompleter } from '@ai-agent/browser/llm';

const llm = new WebLLMCompleter({ model: 'qwen2.5-0.5b-instruct-q4f16_1-MLC' });
await llm.load();
const agent = new Agent({ llm });
const r = await agent.run('hi');
console.log(r.response);
```

### 2. ツール付き

```ts
import { Agent, tool } from '@ai-agent/browser';

const calc = tool<{ a: number; b: number }>({
  name: 'add',
  description: '2 つの整数を加算',
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
console.log((await agent.run('What is 17 + 25?')).response);
```

### 3. ガード + ストリーミング

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

## API リファレンス

### `Agent`

```ts
class Agent {
  constructor(opts: AgentOptions);

  readonly completer: Completer;
  configure(config: AgentConfig): Promise<string[]>;
  setTemperature(t?: number): void;

  registerTools(...tools: ToolDefinition[]): number;
  unregisterTool(name: string): boolean;
  registerGuards(...guards: GuardDefinition[]): number;
  registerVerifiers(...verifiers: VerifierDefinition[]): number;
  tools(): ToolDefinition[];

  run(prompt: string, opts?: RunOptions): Promise<AgentResult>;
  runStream(prompt: string, opts?: RunOptions): AsyncIterable<StreamEvent>;
}

interface AgentOptions {
  llm: Completer;
  initialConfig?: AgentConfig;
}

interface RunOptions {
  maxTurns?: number;
  onDelta?: (text: string, turn: number) => void | Promise<void>;
  onEvent?: (ev: LoopEvent) => void | Promise<void>;
}

type StreamEvent =
  | { kind: 'delta'; text: string; turn: number }
  | { kind: 'event'; event: LoopEvent }
  | { kind: 'end'; result: AgentResult };
```

### 戻り値

```ts
interface AgentResult {
  response: string;
  reason: string;
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

## Completer (LLM バックエンド)

LLM 呼び出しは `Completer` インターフェース経由で抽象化されている:

```ts
interface Completer {
  complete(req: ChatRequest): Promise<ChatResponse>;
  stream?(req: ChatRequest): AsyncIterable<StreamEvent>;
}
```

組み込み実装:

| クラス                            | 用途                                                  |
| --------------------------------- | ----------------------------------------------------- |
| `WebLLMCompleter` (`@ai-agent/browser/llm`) | WebGPU + IndexedDB でローカル LLM (推奨)         |
| `ScriptedCompleter` (テスト用)    | 決め打ちレスポンスを返すモック                          |

任意の `Completer` を自前実装すれば、OpenAI 直叩き / プロキシサーバ経由 /
拡張機能の Native Messaging 経由など何でも繋がる。

> Node / Python / Swift SDK には Go コアの LLM 呼び出しをラッパー側に委譲する
> `llm.execute` 逆 RPC 機構 (ADR-016) があるが、Browser SDK は最初からこの
> `Completer` interface で同じ自由度を実現している (Go コアを使わない
> スタンドアロン構成のため)。`llm.mode="remote"` 相当の設定は不要。

## WebLLM モデル選択

```ts
new WebLLMCompleter({
  model: 'gemma-2-2b-it-q4f16_1-MLC',
  temperature: 0.7,
  engineConfig: { logLevel: 'INFO' },     // @mlc-ai/web-llm の CreateMLCEngine 引数
});
```

WebLLM 0.2.x で動作確認。推奨スターターモデル:

| Model id (WebLLM)                                | サイズ | 用途                                          |
| ------------------------------------------------ | ------ | --------------------------------------------- |
| `qwen2.5-0.5b-instruct-q4f16_1-MLC`              | ~400MB | ローエンドラップトップ / スマホ / 最速コールドスタート |
| `llama-3.2-1b-instruct-q4f16_1-MLC`              | ~700MB | バランス重視                                   |
| `gemma-2-2b-it-q4f16_1-MLC`                      | ~1.5GB | ルーティング + ツール呼び出しに最適なデフォルト  |
| `Llama-3.2-3B-Instruct-q4f16_1-MLC`              | ~2GB   | より精度の高い指示追従                         |

モデルは IndexedDB にキャッシュされ、2 回目以降は瞬時にロードされる。

## 設定リファレンス

`AgentConfig` は Node SDK と同じ形 (snake_case)。ただし以下のみ実装:

| サブ設定             | サポート                                                       |
| -------------------- | -------------------------------------------------------------- |
| `max_turns` / `system_prompt` / `token_limit` | ◯                                        |
| `compaction`         | △ (LLM ステージ無し、snip のみ)                                |
| `permission`         | △ (`ask` 未実装 → 自動拒否)                                    |
| `guards`             | ◯                                                              |
| `verify`             | ◯                                                              |
| `tool_scope`         | ◯                                                              |
| `reminder`           | ◯                                                              |
| `streaming`          | ◯                                                              |
| `delegate` / `coordinator` | ✕                                                          |
| `loop` / `router` / `judge` | ✕                                                         |

完全な型は [`src/config.ts`](./src/config.ts) を参照。

## ツール

```ts
const myTool = tool<{ x: number }>({
  name: 'square',
  description: '整数を 2 乗',
  parameters: {
    type: 'object',
    properties: { x: { type: 'integer' } },
    required: ['x'],
    additionalProperties: false,
  },
  readOnly: true,
  handler: ({ x }) => String(x * x),
});

agent.registerTools(myTool);
```

`handler` は同期/非同期どちらでも OK。戻り値は `string` /
`ToolExecuteResult` / 任意値 (`String()` 化)。

## 組み込みガード / ベリファイア

`agent.configure({ guards: { ... }, verify: { verifiers: [...] } })` で名前指定する:

| ステージ   | 名前                  | 動作                                                          |
| ---------- | --------------------- | ------------------------------------------------------------- |
| input      | `prompt_injection`    | `ignore previous`, `you are now`, `system:` 等のパターンをブロック |
| input      | `max_length`          | 50,000 文字超の入力を拒否                                      |
| tool_call  | `dangerous_shell`     | `rm -rf /`, fork bomb, `mkfs`, `dd of=/dev/...` 等をブロック   |
| output     | `secret_leak`         | `sk-...`, `ghp_...`, `AKIA...`, RSA 秘密鍵を拒否               |
| verifier   | `non_empty`           | ツール結果が空 / 空白のみなら fail                              |
| verifier   | `json_valid`          | 結果が `{`/`[` で始まるなら有効な JSON であることを要求          |

カスタムガード / ベリファイア:

```ts
import { inputGuard, verifier } from '@ai-agent/browser';

agent.registerGuards(inputGuard('no_pii', (input) => /* ... */));
agent.registerVerifiers(verifier('my_check', (tn, args, res) => ({ passed: true, summary: 'ok' })));

await agent.configure({
  guards: { input: ['no_pii'] },
  verify: { verifiers: ['my_check'] },
});
```

## ストリーミング

### コールバック方式

```ts
await agent.run('長めの説明をして', {
  onDelta: (text) => document.body.append(text),
  onEvent: (ev) => console.log('event', ev),
});
```

### イベント方式 (`runStream`)

```ts
for await (const ev of agent.runStream('...')) {
  if (ev.kind === 'delta') document.body.append(ev.text);
  else if (ev.kind === 'event') {
    // ev.event は LoopEvent: router / tool_call / tool_result / verifier / guard / etc.
  } else if (ev.kind === 'end') {
    console.log('done', ev.result.reason);
  }
}
```

`streaming: { enabled: true }` を `configure` で設定すること。

## Node SDK との差分

| 機能                           | Node SDK | Browser SDK |
| ------------------------------ | :------: | :---------: |
| `Agent.run` / `runStream`      | ◯        | ◯           |
| `Agent.configure`              | ◯        | ◯ (subset)  |
| `tool()`, `inputGuard()` etc.  | ◯        | ◯           |
| 組み込みガード / ベリファイア   | ◯        | ◯           |
| `delegate_task` (サブエージェント) | ◯        | ✕         |
| `coordinate_tasks`             | ◯        | ✕           |
| MCP 統合                        | ◯        | ✕           |
| Worktree / SessionRunner       | ◯        | ✕           |
| LLM サマライザ (4 段階の最終段) | ◯        | △ (snip のみ) |
| Permission `ask` step          | ◯        | ✕ (fail-closed) |
| 監査ログ                        | ◯        | ✕           |

ブラウザで未対応の機能は、サブプロセス (MCP / worktree) や対話 UI
(permission `ask`) など「ブラウザに自然な対応形が無いもの」が中心。
それらが必要なら Node SDK を別サーバで動かしてプロキシ通信させるのが定石。

## アーキテクチャ

```
Agent  ─┬─ Completer (Completer interface)
        │     ├ WebLLMCompleter  (WebGPU + IndexedDB)
        │     └ ScriptedCompleter (テスト用)
        │
        ├─ AgentLoop
        │   ├ History         (token 概算 / snip)
        │   ├ Router step     (JSON mode + jsonfix リカバリ)
        │   ├ Tool registry + permission check + tool_call guards
        │   ├ Tool execute    (ハンドラがブラウザタブで動く)
        │   ├ Verifiers       (non_empty / json_valid / カスタム)
        │   └ Chat step       (最終応答 + output guards + streaming)
        │
        └─ 組み込みガード / ベリファイアファクトリ (名前で指定)
```

ルーター / ツールループ / ガード / コンテキスト管理は Go コアと同じロジックを
TypeScript で実装したもの。プロトコル定義 (`docs/openrpc.json`) を共有しない
代わりに、内部 API シグネチャを Node SDK と揃えている。

## テスト

```bash
cd sdk/js-browser
npm install
npm run build
npm test
```

テストは `ScriptedCompleter` モックを使うので、WebGPU もモデルダウンロードも
不要。

## ライセンス

MIT (ai-agent 本体と同じ)。

## 参考

- [`../README.md`](../README.md) — SDK 全体のハブ
- [`../js/`](../js/) — 兄弟 Node SDK
- [WebLLM 公式](https://github.com/mlc-ai/web-llm) — ベースの LLM ランタイム
- [WebGPU API](https://developer.mozilla.org/en-US/docs/Web/API/WebGPU_API)
