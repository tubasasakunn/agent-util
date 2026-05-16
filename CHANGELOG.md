# Changelog

このプロジェクトの全ての注目すべき変更はこのファイルに記録される。
形式は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に準拠し、
バージョニングは [Semantic Versioning](https://semver.org/lang/ja/) を採用する。

詳細なバージョニング方針は [docs/VERSIONING.md](docs/VERSIONING.md) を参照。

## [Unreleased]

(まだリリースされていない変更があればここに追加)

## [0.2.1] - 2026-05-16

Swift SDK の **配信修正リリース**。v0.2.0 を SwiftPM 経由で外部利用すると
`Package.swift` がリポジトリルートに無いため依存解決に失敗していた。
ロジック変更なし、Swift 利用者向けの hotfix のみ。

### Fixed

- **Swift SDK の SwiftPM 依存解決を修正** — Swift Package Manager は
  リポジトリのルートにある `Package.swift` しか認識しないため、ルートに
  マニフェストを置き、ソースパスを `sdk/swift/Sources/AIAgent` / テストを
  `sdk/swift/Tests/AIAgentTests` に向けるよう変更。これにより
  `.package(url: "https://github.com/tubasasakunn/agent-util.git", from: "0.2.1")`
  で正しく利用可能になった。
- 重複していた `sdk/swift/Package.swift` を削除。ローカルで `swift build` /
  `swift test` を実行する際は **リポジトリのルート**から実行する。

### Docs

- `sdk/swift/README.md` を 0.2.1 のインストール手順に追従。
- `sdk/README.md` でルート Package.swift の存在を明記。

## [0.2.0] - 2026-05-16

エージェント連携を強化する 4 つの大きなテーマを含む 2 回目のリリース。
**Swift SDK 新規追加**、**Browser SDK 新規追加**、**AOM (Agent Object Model)
の確立**、**Deep Research / Agent Skills の実装**。前回からの差分は 44 コミット。

### Added

#### Swift SDK 新規追加 (47b2c78)

- `sdk/swift/` — Python/JS と同等の AOM を実装した SwiftPM ライブラリ。
  macOS 13+、Swift 5.9+。Foundation のみ依存。
- `Agent` (高レベル AOM) — `input` / `stream` / `fork` / `branch` / `add` /
  `addSummary` / `batch` / `search` / `context` / `export` / `importHistory` /
  `improveTool` / `registerJudge` を含む完全実装。
- `RawAgent` (低レベル) — JSON-RPC を直接叩く実装者向け API。
- `JSONValue` — 動的 JSON を表す Sendable な列挙型 (Codable + リテラル準拠)。
- `MessageIndex` — 外部依存なしの TF-IDF RAG (日本語 CJK 対応)。
- E2E テスト 6/6 PASS — Gemma 4 E2B に対して configure / abort / input /
  inputVerbose / fork (会話履歴の継承を確認) / ツール登録まで動作確認済み。

#### Browser SDK 新規追加 (b3b4897, 1acd38a, 2cc1196, 0337e28, a528c43)

- `sdk/js-browser/` — ピュア TypeScript の **ブラウザ内エージェント**。
  ルーター → ツール → ガード → ベリファイア → 出力のループが in-process で動く。
- `WebLLMCompleter` — WebGPU + IndexedDB でローカル LLM を実行 (Gemma /
  Llama 3.2 / Qwen 2.5 等)。
- `Completer` インターフェース — 任意の LLM バックエンドを差し替え可能。
  `ScriptedCompleter` (テスト用) を同梱。
- 組み込みガード/ベリファイア — `prompt_injection` / `max_length` /
  `dangerous_shell` / `secret_leak` / `non_empty` / `json_valid`。
- Playwright での実機動作確認 + ブラウザデモを examples/ に追加。

#### AOM (Agent Object Model) 確立 (e68938b, f7c38e8, 409a7a6)

- 設計哲学を CLAUDE.md に言語化 — 「エージェントを、会話状態を持つ
  ファーストクラスオブジェクトとして扱う」。
- 高レベル API `easy.Agent` / `easy.AgentConfig` を Python SDK に追加。
  Swift SDK の API と等価。
- Go コア側に AOM を支える新 RPC を追加:
  - `session.history` — 会話履歴エクスポート (fork/add/export の基盤)
  - `session.inject` — 会話履歴注入 (`prepend` / `append` / `replace`)
  - `context.summarize` — LLM による会話要約 (`context()` / `add_summary()` の基盤)

#### Deep Research モード (9977a2e, 02fcc20, 4fda92f)

- 複数ソース集約 + 構造化レポート生成。Node.js examples で実機完走を確認。
- 反復試験で安定化済み。

#### Agent Skills 実装 (5762460, f704f2c, 18a9ec1, 3c0370c)

- ディレクトリベースのスキル定義と動的ロード。
- Node.js REPL / REST API server / Browser UI (ai-agent Studio) の例を追加。

#### ai-agent Studio (0efeacf)

- スキル / MCP / ツールの管理を行うブラウザ UI を新規追加。

#### Python SDK 強化

- `easy.Agent` の AOM 完全実装 (409a7a6, 201bbc7) — Guard / Verifier / Judge
  登録、`input()` シグネチャ改善。
- `register_*` の戻り値を `list[str]`(名前リスト) に統一 (26d8f59)。
- `Literal` / `Enum` を JSON Schema に正確にマップ (15fe655)。
- `TripwireTriggered` サブクラスと `batch()` タイムアウト追加 (c5bd291)。
- `AgentConfig` バリデーション / `__repr__` 追加 / `close()` 冪等化 (13a762c)。
- `GoalJudgeCallable` 型強化 / `Tool.__repr__` 追加 / env docstring 改善 (aa5bf23)。
- `py.typed` 追加 / `input_verbose()` 追加 / `AgentResult` トップレベル
  エクスポート (82c9f03)。

#### プロトコル / コア

- LoopType enum / GoalJudge / RouterCompleter を追加し、エージェント設定を拡張
  (460c75c)。
- ポインタヘルパー / LoopType 定数 / Python LoopConfig 型安全化 (d739c8b)。

### Changed

- `Tool` interface から `IsConcurrencySafe` を削除し、`OK`/`Errorf` ヘルパーを
  追加 (5241a6d, e4b5d4c)。
- `Agent` / `AgentConfig` (Python) を高レベル API に統一 (409a7a6)。
- エンジン全体のバグ修正・設計改善・重複コード削減 (92116f3)。
- 設計改善 10 件 — 計算重複排除 / Fork バグ修正 / DRY 化 / ポーリング改善 /
  型統一 / goroutine 上限 / dead code 削除 (46e945f, 5221e7f)。

### Fixed

- `fork()` の RAG インデックス分離・`stream()` フォールバック修正 (8ac3143)。
- `async judge` の `await` 修正・`errors.__all__` 追加 (73c308c)。
- `register_tools` テスト修正・`__init__.py` の `AgentResult` 重複を除去 (1da97c7)。
- `asyncio.TimeoutError` を `AgentError` に変換 (1058531)。
- `GuardDenied` を実際に raise するエラーコードを追加 (Go/Python 同期、154dd26)。
- "not found" エラーに登録済み名一覧を付与 (d4f2c4f)。
- エラーメッセージを改善し原因と解決策を明示 (a2e4256)。
- `SLLM_ENDPOINT` のデフォルト値をフルパスに修正 (778fdf6)。
- `TestCoordinateStep_ResultBudget` の並列スケジューリング依存を除去 (11a1420)。
- gofmt 違反を修正 (`pkg/protocol/methods.go` の整列) (e9ff73f)。
- WebLLM の 3 つの非互換を解消 (2cc1196)。
- WebLLM の動的 import を Vite が解決できるよう修正 (1acd38a)。

### Docs

- 4 SDK の README を統一構造で全面書き直し (cd9f7f0) — `sdk/README.md` を
  ハブ化、Python / JS (Node) / JS (Browser) / Swift を同じ章立てに揃え、
  AOM 実装状況の機能パリティ表を明示。
- CLAUDE.md に SDK 設計哲学「エージェントオブジェクトモデル (AOM)」を
  言語化 (e68938b)。
- `docs/VERSIONING.md` を更新 — Browser / Swift SDK のバージョン管理方針を
  追記。

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
