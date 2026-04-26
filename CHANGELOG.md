# Changelog

このプロジェクトの全ての注目すべき変更はこのファイルに記録される。
形式は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に準拠し、
バージョニングは [Semantic Versioning](https://semver.org/lang/ja/) を採用する。

詳細なバージョニング方針は [docs/VERSIONING.md](docs/VERSIONING.md) を参照。

## [Unreleased]

(まだリリースされていない変更があればここに追加)

## [0.1.0] - 2026-04-26

初回リリース。SLLM 向けエージェントハーネスの中核機能、JSON-RPC API、SDK、
ドキュメント一式が揃った最初のスナップショット。

### Added

#### コア（Phase 1〜10）

- Phase 1: LLM クライアント — OpenAI 互換 HTTP クライアント、JSON 応答の補正、
  指数バックオフリトライ。`internal/llm` (b0d9925, 6958344)
- Phase 2: エージェントループ — Continue / Terminal の状態遷移、
  最大ターン数制御。`internal/engine` (5560b3d)
- Phase 3: ツール実行 — ルーター（JSON モード）+ 単一ツール呼び出しパターン。
  ADR-002 / ADR-003。`internal/engine`, `pkg/tool` (dfb4624)
- Phase 4: コンテキスト管理基盤 — Manager、トークン推定、Observer、
  entry 型ラッパーパターン。ADR-004。`internal/context` (e6d8314)
- Phase 5: コンテキスト縮約カスケード — BudgetTrim → ObservationMask
  → Snip → Compact の 4 段カスケード。LLM サマライザを内蔵。
  ADR-005。`internal/context` (a0b88d5)
- Phase 6a: サブエージェント委譲 — Engine 内バーチャルツール `delegate_task`、
  結果の文字数制限による凝縮。ADR-006 / ADR-007。`internal/engine` (007a2f3)
- Phase 6b-d: Worktree / Coordinator / SessionRunner — `context.Context` 経由の
  workDir 伝達、`sync.WaitGroup` ベースの並列実行と部分成功、
  ファイルシステムベースの Ralph Wiggum ループ。
  ADR-008 / ADR-009 / ADR-010。`internal/engine` (4bbf1a8)
- Phase 7: コンテキスト構成 — PromptBuilder、MemoryIndex、ToolScope。
  セクションベースのプロンプト構成。ADR-011。`internal/context` (67a3ffe)
- Phase 8: 検証ループとエラー回復 — Plan-Execute-Verify (PEV) サイクル、
  エラー 4 分類（transient / format / semantic / fatal）、Verifier 層、
  ビルトイン `non_empty` / `json_valid`。`internal/engine` (099e84f)
- Phase 9: 権限とガード — `deny → allow → ask` の 3 段権限判定、
  Input / ToolCall / Output の 3 ステージ Guard、Tripwire、監査ログ。
  ADR-012。ビルトイン `prompt_injection` / `max_length` / `dangerous_shell`
  / `secret_leak`。`internal/engine` (33b3fa7)
- Phase 10: JSON-RPC over stdio + RemoteTool + MCP 統合 — RemoteTool
  アダプタと PendingRequests パターンでラッパーから登録した
  ツールを Engine が実行可能に。MCP サーバー統合。ADR-001 / ADR-013。
  `internal/rpc`, `internal/mcp`, `pkg/protocol` (dab635d)

#### ラッパー連携拡張（Phase 11〜13）

- Phase 11: `agent.configure` — Permission / Guard 設定、Verify 設定、
  Compaction、Streaming、Reminder、Delegate / Coordinator、ToolScope を
  RPC 経由で動的に調整可能に (f77eb73)
- Phase 12: ストリーミング通知配線 — `stream.delta` / `stream.end` /
  `context.status` の通知を Engine から発火。StreamingCompleter
  別インターフェース + Engine コールバック。ADR-014 (3f9e539)
- Phase 13: ラッパー側カスタム Guard / Verifier — `guard.execute` /
  `verifier.execute` のリモート呼び出しでラッパー実装の Guard と
  Verifier を統合。ADR-015 (3823d51)

#### 公開仕様・SDK・ドキュメント

- OpenRPC 1.2.6 仕様公開 — `docs/openrpc.json` と 36 個別 JSON Schema
  (`docs/schemas/*.json`、Draft 2020-12)。`pkg/protocol/spec_test.go`
  で Go 型と仕様の一致を検証 (24b9bd0)
- Python SDK — `sdk/python/ai_agent`。async-first、`@tool` /
  `@input_guard` / `@tool_call_guard` / `@output_guard` / `@verifier`
  デコレータ、ストリーミング AsyncIterable (e844139)
- TypeScript SDK — `sdk/js/@ai-agent/sdk`。ESM、AsyncIterable
  ストリーミング、Node.js 20+ (f415bea)
- examples/ ディレクトリ — Python と JS で各 5 例 (4b15927)
  - 01 minimal_chat
  - 02 file_reader_tool / http_fetch_tool
  - 03 guards_and_permission
  - 04 streaming
  - 05 custom_remote_guard
- API リファレンスドキュメント — `docs/api/`、22 ページ。overview /
  errors / builtins / methods / concepts (fb19bd2)

#### バージョン管理

- `pkg/protocol/version.go` — ライブラリのセマンティックバージョン定数
  `LibraryVersion = "0.1.0"`。SDK ホップ間の一致を保証する真実の源
  （既存の `Version = "2.0"` は JSON-RPC 仕様バージョンを指すため別途残す）
- `CHANGELOG.md` — 本ファイル
- `docs/VERSIONING.md` — semver 採用方針と破壊的変更時の運用ルール
- `README.md` — プロジェクト概要 / Quickstart / リンク集
- `LICENSE` — MIT

### Built-ins

- Input guards: `prompt_injection`, `max_length`
- Tool call guards: `dangerous_shell`
- Output guards: `secret_leak`
- Verifiers: `non_empty`, `json_valid`
- Compaction summarizers: `llm`

### JSON-RPC API

- リクエスト/レスポンス: `agent.run` / `agent.abort` / `agent.configure` /
  `tool.register` / `tool.execute` / `mcp.register` /
  `guard.register` / `guard.execute` /
  `verifier.register` / `verifier.execute`
- 通知: `stream.delta` / `stream.end` / `context.status`

### Architecture Decisions

このリリースに含まれる ADR (`.claude/skills/decisions/`):

- ADR-001 JSON-RPC over stdio
- ADR-002 ルーター + 単一ツールパターン
- ADR-003 ルーター引数の直接使用
- ADR-004 コンテキスト entry 型ラッパー
- ADR-005 4 段カスケード縮約
- ADR-006 サブエージェント Engine 内バーチャルツール
- ADR-007 サブエージェント結果の文字数制限
- ADR-008 Worktree workDir を context.Context で伝達
- ADR-009 Coordinator 並列実行の部分成功
- ADR-010 Ralph Wiggum SessionRunner
- ADR-011 PromptBuilder セクションパターン
- ADR-012 PermissionChecker + GuardRegistry の 2 層分離
- ADR-013 RemoteTool + PendingRequests
- ADR-014 ストリーミング通知配線
- ADR-015 リモート Guard / Verifier

### 既知の制約

- 0.x の間は minor バンプで破壊的変更を許容する（semver 慣例）
- JSON-RPC API の安定化保証は 1.0.0 以降
- ラッパー側 Guard / Verifier の RPC ラウンドトリップ遅延は
  Engine の実行レイテンシに直接影響する

[Unreleased]: https://github.com/tubasasakunn/ai-agent/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/tubasasakunn/ai-agent/releases/tag/v0.1.0
