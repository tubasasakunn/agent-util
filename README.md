# ai-agent

SLLM（小さい LLM）を動かすエージェントハーネスを Go で構築するプロジェクト。

[![CI](https://github.com/tubasasakunn/ai-agent/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/tubasasakunn/ai-agent/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25.1-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Status](https://img.shields.io/badge/Status-alpha-orange)](docs/VERSIONING.md)
[![Latest](https://img.shields.io/badge/Latest-v0.1.0-blue)](CHANGELOG.md)

ルーター + 単一ツールパターン、4 段カスケードのコンテキスト縮約、
サブエージェント委譲、PEV サイクル + Verifier、3 段 Guard、JSON-RPC over stdio
によるラッパー統合といったエージェントハーネスの主要要素を 1 バイナリに集約する。
ラッパーからのカスタムツール / Guard / Verifier 登録に対応し、Python と
TypeScript の公式 SDK を提供する。

## Quickstart

### Go バイナリのビルド

```bash
go build -o agent ./cmd/agent
./agent  # stdin / stdout で JSON-RPC をしゃべるサーバーが起動
```

### Python SDK

```bash
pip install -e sdk/python
```

```python
import asyncio
from ai_agent import Client

async def main():
    async with Client.spawn(["./agent"]) as client:
        result = await client.agent_run(prompt="こんにちは")
        print(result.response)

asyncio.run(main())
```

### TypeScript SDK

```bash
cd sdk/js && npm install && npm run build
```

```ts
import { Client } from "@ai-agent/sdk";

const client = await Client.spawn(["./agent"]);
const result = await client.agentRun({ prompt: "こんにちは" });
console.log(result.response);
await client.close();
```

より実用的な例（ツール登録、ストリーミング、リモート Guard など）は
[`examples/`](examples/) を参照。

## ドキュメント

| 種別                 | 場所                                                                |
| -------------------- | ------------------------------------------------------------------- |
| プロジェクト概要     | [CLAUDE.md](CLAUDE.md)                                              |
| API リファレンス     | [docs/api/](docs/api/) (overview / methods / concepts / errors)     |
| OpenRPC 仕様         | [docs/openrpc.json](docs/openrpc.json)                              |
| JSON Schema          | [docs/schemas/](docs/schemas/)                                      |
| 利用例               | [examples/](examples/) (Python / JS 各 5 例)                        |
| 変更履歴             | [CHANGELOG.md](CHANGELOG.md)                                        |
| バージョニング方針   | [docs/VERSIONING.md](docs/VERSIONING.md)                            |
| 設計判断 (ADR)       | [.claude/skills/decisions/](.claude/skills/decisions/)              |
| 検証ログ             | [.claude/skills/investigation/](.claude/skills/investigation/)      |

## プロジェクト構成

```
cmd/agent/        — バイナリのエントリポイント
internal/engine/  — エージェントループ、ルーター、状態遷移、Guard、PEV
internal/llm/     — OpenAI 互換クライアント、パース補正、リトライ
internal/context/ — コンテキスト管理、メッセージ履歴、4 段カスケード縮約
internal/rpc/     — JSON-RPC サーバー (stdin/stdout)
internal/mcp/     — MCP クライアント統合
internal/tools/   — ビルトインツール
pkg/protocol/     — JSON-RPC メッセージ型、イベント定義、Version 定数
pkg/tool/         — Tool interface、スキーマ定義
sdk/python/       — Python SDK
sdk/js/           — TypeScript SDK
docs/             — 公開 API ドキュメント / OpenRPC / JSON Schema
examples/         — Python / TS の利用例
```

## ステータス

`v0.1.0` (alpha)。`0.x` の間は minor バンプで破壊的変更を許容する
（semver 慣例）。`1.0.0` 以降で JSON-RPC API の安定化を約束する。

詳細は [docs/VERSIONING.md](docs/VERSIONING.md) を参照。

## ライセンス

[MIT](LICENSE)
