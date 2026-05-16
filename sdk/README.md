# ai-agent SDKs

`ai-agent` のエージェントハーネス（Go 製、`agent --rpc` バイナリ or WebLLM）を
各言語から薄くラップした SDK 集。**全 SDK が同じ Agent Object Model (AOM) を
体現**するため、どの言語でも同じ感覚で書ける。

```
┌────────────────────┐    JSON-RPC 2.0 / stdio    ┌─────────────────┐
│ あなたのアプリ     │ ──────────────────────────▶ │ agent --rpc     │
│ (Python/JS/Swift)  │ ◀────────────────────────── │ (Go ハーネス)   │
└────────────────────┘    notifications etc.       └─────────────────┘
                                                          │
                                                          ▼
                                                  ┌─────────────────┐
                                                  │ SLLM (OpenAI互換) │
                                                  └─────────────────┘

ブラウザだけは subprocess を使わず、in-page で同じ Agent ループを動かす。
```

## SDK 一覧

| 言語              | パッケージ           | パス                              | ランタイム                                    | AOM 高レベル API |
| ----------------- | -------------------- | --------------------------------- | --------------------------------------------- | :--------------: |
| **Python**        | `ai-agent` (local)   | [`./python/`](./python/)          | サブプロセス: `agent --rpc` over stdio        | ◯                |
| **TypeScript (Node)** | `@ai-agent/sdk`  | [`./js/`](./js/)                  | サブプロセス: `agent --rpc` over stdio        | △ (低レベルのみ) |
| **TypeScript (Browser)** | `@ai-agent/browser` | [`./js-browser/`](./js-browser/) | ブラウザ内 (LLM は WebLLM / WebGPU)           | △ (低レベルのみ) |
| **Swift**         | `AIAgent` (SwiftPM)  | [`./swift/`](./swift/)            | サブプロセス: `agent --rpc` over stdio        | ◯                |

## 言語の選び方

```
                  ┌─ Python ecosystem を使いたい  ───────────────▶ Python
                  ├─ iOS / macOS / SwiftUI から呼びたい ───────────▶ Swift
このエージェントを ─┤
どこで動かしたい？ ├─ Node.js / Bun / Deno でサーバ実装       ───▶ JS (Node)
                  └─ ブラウザ内で完結させたい (zero server)   ───▶ Browser
```

簡単な判断指標:

| やりたいこと                                         | 推奨 SDK           |
| --------------------------------------------------- | ------------------ |
| サーバサイドスクリプト, CI agent, CLI ツール        | Node / Python      |
| iOS / macOS アプリ                                   | Swift              |
| ブラウザで動く LLM デモ                              | Browser            |
| MCP 統合 / サブエージェント (`delegate_task`)         | Node / Python      |
| LangChain 等の Python エコシステムと併用             | Python             |
| `fork()` / `branch()` / `batch()` などの AOM 機能を使う | Python / Swift     |

## 共通モデル — Agent Object Model (AOM)

**「エージェントを、会話状態を持つファーストクラスオブジェクトとして扱う」**
という設計思想。Git のコミット・Unix のプロセス・OOP のオブジェクトと同じ発想
で、エージェントを生成・複製・合成・廃棄できる実体として設計している。

### 5 つの特徴

| 特徴                  | 表れ                                                                                  |
| --------------------- | ------------------------------------------------------------------------------------- |
| **1. 状態カプセル化** | `input()` を呼ぶだけで会話が積まれる。スキル/MCP/サブエージェントの内部呼び出しは漏れない |
| **2. コンポーザブル** | `fork()` `add()` `add_summary()` `branch()` でエージェント同士が会話文脈を受け渡せる    |
| **3. 設定の集約**     | バイナリパス・環境変数・system_prompt を `AgentConfig` 一つに束ねる                    |
| **4. バッテリー内蔵** | RAG 検索・要約・バッチ・チェックポイントが標準搭載。外部ライブラリ不要                  |
| **5. 段階的複雑性**   | `agent.input(prompt)` だけで動く。必要になったら `fork()` や `batch()` を足せばいい     |

### 低レベル API との住み分け

```
低レベル (client.py / client.ts / RawAgent.swift)
  └─ JSON-RPC の生の呼び出しを扱う。プロトコルに忠実。
     外部ラッパーや細かい制御が必要な実装者向け。

高レベル (easy.py / Agent.swift)
  └─ AOM を実装。エージェントをオブジェクトとして使う開発者向け。
     内部では低レベル API を呼んでいる。
```

JS (Node) と Browser は現状、低レベル API のみを提供している。

## 機能パリティ

| 機能                                            | Python | JS (Node) | Browser | Swift |
| ----------------------------------------------- | :----: | :-------: | :-----: | :---: |
| `agent.run` / streaming                         |   ◯    |    ◯      |   ◯     |   ◯   |
| ツール登録                                       |   ◯    |    ◯      |   ◯     |   ◯   |
| 組み込みガード / ベリファイア                    |   ◯    |    ◯      |   ◯     |   ◯   |
| カスタムガード / ベリファイア                     |   ◯    |    ◯      |   ◯     |   ◯   |
| パーミッションポリシー                            |   ◯    |    ◯      |   △ (no `ask`) | ◯  |
| ルーター (JSON mode + jsonfix)                  |   ◯    |    ◯      |   ◯     |   ◯   |
| コンテキスト圧縮 (4 段階カスケード)               |   ◯    |    ◯      |   △ (no LLM stage) | ◯ |
| `delegate_task` / `coordinate`                  |   ◯    |    ◯      |   ✕     |   ◯   |
| MCP 統合                                         |   ◯    |    ◯      |   ✕     |   ◯   |
| Worktree / SessionRunner                         |   ◯    |    ◯      |   ✕     |   ◯   |
| 監査ログ                                         |   ◯    |    ◯      |   ✕     |   ◯   |
| **AOM `fork()` / `branch()` / `batch()` / `search()`** | **◯** | ✕      | ✕     | **◯** |
| **AOM `context()` (会話要約)**                   | **◯** | ✕      | ✕     | **◯** |
| **AOM `export()` / `import_history()`**         | **◯** | ✕      | ✕     | **◯** |
| **AOM `register_judge()` / `improve_tool()`**   | **◯** | ✕      | ✕     | **◯** |

JS (Node) / Browser でも、JSON-RPC で `session.history` / `session.inject` /
`context.summarize` などの低レベル RPC を直接叩けば AOM 相当の操作は可能。
高レベルラッパーが Python / Swift にしか無いだけ。

## 共通: Go ハーネスのビルド

Python / JS (Node) / Swift は同じバイナリを使う：

```bash
go build -o agent ./cmd/agent/
```

バイナリは `agent --rpc` モードで起動すると stdin/stdout で JSON-RPC を話す。
プロトコル仕様は [`../docs/openrpc.json`](../docs/openrpc.json)、型定義は
[`../docs/schemas/`](../docs/schemas/) に置いている。

### 必要な環境変数 (SLLM 接続)

| 変数                | 必須 | 例                                                      |
| ------------------- | :--: | ------------------------------------------------------- |
| `SLLM_ENDPOINT`     | ◯    | `http://localhost:8080/v1/chat/completions`             |
| `SLLM_API_KEY`      |      | `sk-xxx`                                                 |
| `SLLM_MODEL`        |      | `gemma3:4b` / `gpt-4o-mini` 等                          |
| `SLLM_CONTEXT_SIZE` |      | `8192`                                                   |

OpenAI 互換エンドポイントなら何でも繋がる（ローカル llama.cpp、Ollama、
LMStudio、本物の OpenAI API 等）。

ブラウザ SDK は SLLM サーバ不要。WebLLM が IndexedDB にモデルを落として
WebGPU で実行する。

## アーキテクチャ概略

```
┌────────────── あなたのアプリ ─────────────┐
│                                            │
│  Agent (高レベル AOM, Python/Swift)        │
│   ├ input / inputVerbose / stream         │
│   ├ fork / branch / add / addSummary      │
│   ├ batch / search / context              │
│   ├ registerTools / registerGuards /      │
│   │  registerVerifiers / registerJudge /  │
│   │  registerMCP / registerSkills         │
│   └ export / importHistory / improveTool  │
│                                            │
│  ─ 内部で ─                               │
│  RawAgent (低レベル, 全 SDK)               │
│   ├ configure / run / abort               │
│   ├ register_tools / register_guards /    │
│   │  register_verifiers / register_mcp /  │
│   │  register_judge                       │
│   └ summarize                             │
│                                            │
│  ─ 通信 ─                                  │
│  JsonRpcClient (newline-delimited JSON)    │
│                                            │
└─────────────────────┬─────────────────────┘
                       │ stdin/stdout
                       ▼
              ┌────────────────┐
              │ agent --rpc    │  Go ハーネス
              │  (Engine /     │  - ルーター
              │   Coordinator) │  - ガード/Verifier 実行
              │                │  - Permission チェック
              │                │  - コンテキスト圧縮
              │                │  - Worktree / Audit
              └────────┬───────┘
                       │ HTTP
                       ▼
              ┌────────────────┐
              │ SLLM サーバ    │
              └────────────────┘
```

## JSON-RPC プロトコル

| 方向            | メソッド                                                                                                         |
| --------------- | ---------------------------------------------------------------------------------------------------------------- |
| wrapper → core  | `agent.configure` / `agent.run` / `agent.abort` / `context.summarize`                                            |
| wrapper → core  | `tool.register` / `guard.register` / `verifier.register` / `judge.register` / `mcp.register`                     |
| wrapper → core  | `session.history` / `session.inject` (AOM の fork/add/branch の基盤)                                              |
| core → wrapper  | `tool.execute` / `guard.execute` / `verifier.execute` / `judge.evaluate` (登録した callable を core が呼び返す)      |
| core → wrapper (通知) | `stream.delta` / `stream.end` / `context.status`                                                          |

完全仕様: [`../docs/openrpc.json`](../docs/openrpc.json)

## バージョン

現在: **0.1.0**

| 場所                                              | 役割                          |
| ------------------------------------------------- | ----------------------------- |
| `pkg/protocol/version.go`                         | 真実の源 (`LibraryVersion`)    |
| `sdk/python/pyproject.toml`                       | Python パッケージ              |
| `sdk/js/package.json` / `sdk/js-browser/package.json` | JS パッケージ                  |
| `sdk/swift/Package.swift`                         | Swift パッケージ (Git tag で管理) |

バージョニング方針: [`../docs/VERSIONING.md`](../docs/VERSIONING.md)

## 参考

- [`../docs/openrpc.json`](../docs/openrpc.json) — OpenRPC 1.2.6 完全仕様
- [`../docs/schemas/`](../docs/schemas/) — 各型の JSON Schema
- [`../pkg/protocol/methods.go`](../pkg/protocol/methods.go) — Go 側の真実の源
- [`../docs/CONTRIBUTING.md`](../docs/CONTRIBUTING.md) — 貢献ガイド
- ADR-001 (JSON-RPC over stdio) / ADR-013 (RemoteTool アダプタ) ほかは
  リポジトリルートで `/decision list` を実行
