# v0.1.0 — 初回リリース

`ai-agent` は SLLM (Small Language Model) を本番投入できる強度で動かすための、
言語非依存なエージェントハーネスです。Go コアに JSON-RPC over stdio で接続し、
Python / TypeScript / その他の言語からエージェントを構築できます。

リリース日: **2026-04-26**

---

## ハイライト

- **言語非依存**: JSON-RPC over stdio で Python / TS / Go から同じハーネスを使える
- **本番強度のセキュリティ**: `deny → allow → ask` の 3 段権限判定、
  Input / ToolCall / Output の 3 ステージ Guard、Tripwire、監査ログ
- **小さいモデルでも回る**: ルーター（JSON モード）+ 単一ツールパターン、
  4 段カスケード縮約、Plan-Execute-Verify サイクル
- **完全なオブザーバビリティ**: `stream.delta` / `context.status` /
  `stream.end` の通知、Usage / Reason の集計
- **拡張性**: ラッパー側ツール (`tool.execute`)、リモートガード/Verifier
  (`guard.execute` / `verifier.execute`)、MCP 接続

---

## 主要コンポーネント

### コア (Go)

JSON-RPC メソッド:

- `agent.run` / `agent.abort` / `agent.configure`
- `tool.register` / `tool.execute` / `mcp.register`
- `guard.register` / `guard.execute`
- `verifier.register` / `verifier.execute`

通知:

- `stream.delta` — トークン差分のストリーミング
- `stream.end` — ストリーム終端
- `context.status` — コンテキスト使用率

### ビルトイン

| 種別                  | 名前                                |
| --------------------- | ----------------------------------- |
| Input guards          | `prompt_injection`, `max_length`    |
| Tool call guards      | `dangerous_shell`                   |
| Output guards         | `secret_leak`                       |
| Verifiers             | `non_empty`, `json_valid`           |
| Compaction summarizer | `llm`                               |

### SDK

- **Python** (`sdk/python/ai-agent-sdk`): async-first、`@tool` /
  `@input_guard` / `@tool_call_guard` / `@output_guard` / `@verifier`
  デコレータ、AsyncIterable ストリーミング
- **TypeScript** (`sdk/js/@ai-agent/sdk`): ESM、AsyncIterable
  ストリーミング、`await using` 対応、Node.js 20+

### 仕様 / ドキュメント

- [`docs/openrpc.json`](openrpc.json) — OpenRPC 1.2.6 仕様、13 メソッド、36 スキーマ
- [`docs/api/`](api/) — 人間向け API リファレンス、22 ページ（overview / errors /
  builtins / methods / concepts）
- [`examples/`](../examples/) — Python / TypeScript で各 5 例
- ADR 15 件（`.claude/skills/decisions/`）、investigation 12 件
  （`.claude/skills/investigation/`）

---

## インストール

### コアバイナリ（リポジトリから）

```bash
git clone https://github.com/tubasasakunn/ai-agent.git
cd ai-agent
go build -o agent ./cmd/agent/
```

または、本リリースに添付された各プラットフォームの
`agent_v0.1.0_<os>_<arch>.tar.gz` (Linux / macOS) /
`.zip` (Windows) を解凍して `agent` バイナリをそのまま使う。

### Python SDK

```bash
pip install -e ./sdk/python
```

> PyPI への publish は次マイナー以降に予定。本リリース時点では
> リポジトリからの editable install を案内する。

### TypeScript SDK

```bash
cd sdk/js && npm install && npm run build
```

> npm への publish は次マイナー以降に予定。本リリース時点では
> ローカルビルド + パスインポートを案内する。

---

## クイックスタート

### Python

```python
import asyncio
from ai_agent import Client

async def main():
    async with Client.spawn(["./agent"]) as client:
        result = await client.agent_run(prompt="こんにちは")
        print(result.response)

asyncio.run(main())
```

### TypeScript

```ts
import { Client } from "@ai-agent/sdk";

const client = await Client.spawn(["./agent"]);
const result = await client.agentRun({ prompt: "こんにちは" });
console.log(result.response);
await client.close();
```

より実用的な例（ツール登録、ストリーミング、リモート Guard など）は
[`examples/`](../examples/) を参照。

---

## アーキテクチャの主要決定 (ADR)

このリリースに含まれる ADR（`.claude/skills/decisions/`）:

- ADR-001 JSON-RPC over stdio
- ADR-002 ルーター + 単一ツールパターン
- ADR-003 ルーター引数の直接使用
- ADR-004 コンテキスト entry 型ラッパー
- ADR-005 4 段カスケード縮約
- ADR-006 サブエージェント Engine 内バーチャルツール
- ADR-007 サブエージェント結果の文字数制限
- ADR-008 Worktree workDir を `context.Context` で伝達
- ADR-009 Coordinator 並列実行の部分成功
- ADR-010 Ralph Wiggum SessionRunner
- ADR-011 PromptBuilder セクションパターン
- ADR-012 PermissionChecker + GuardRegistry の 2 層分離
- ADR-013 RemoteTool + PendingRequests
- ADR-014 ストリーミング通知配線
- ADR-015 リモート Guard / Verifier

---

## 既知の制約

- `0.x` の間は MINOR バンプで破壊的変更を許容する（semver 慣例、
  [`docs/VERSIONING.md`](VERSIONING.md) 参照）
- JSON-RPC API の安定化保証は `1.0.0` 以降
- SDK の PyPI / npm への publish は未対応（次 MINOR で予定）
- Windows での動作未検証（CI でクロスビルドのみ、実機テストなし）
- ラッパー側 Guard / Verifier の RPC ラウンドトリップ遅延は
  Engine 実行レイテンシに直接影響する

---

## 詳細

- [CHANGELOG](../CHANGELOG.md) — 全変更の詳細
- [API リファレンス](api/README.md)
- [OpenRPC 仕様](openrpc.json)
- [JSON Schema](schemas/)
- [ADR 一覧](../.claude/skills/decisions/)
- [バージョニング方針](VERSIONING.md)
- [リリース手順](RELEASE.md)
- [貢献ガイド](CONTRIBUTING.md)
