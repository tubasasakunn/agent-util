# ai-agent

SLLM（小さいLLM）を動かすエージェントハーネスをGoで構築するプロジェクト。

## バージョン

- Version: `0.2.1`
- 真実の源: `pkg/protocol/version.go` (`protocol.LibraryVersion` 定数。JSON-RPC 仕様の `Version="2.0"` とは別物）
- 変更履歴: [CHANGELOG.md](CHANGELOG.md)
- バージョニング方針: [docs/VERSIONING.md](docs/VERSIONING.md)

`pkg/protocol/version.go` / `sdk/python/pyproject.toml` / `sdk/js/package.json` /
`sdk/js-browser/package.json` / `README.md` バッジ / 本セクションは常に同一値で同期する。
Swift SDK (`sdk/swift`) は SwiftPM の慣習に従い Git tag (例: `v0.2.0`) で
バージョン管理する。

## プロジェクト構成

```
cmd/agent/       — バイナリのエントリポイント
internal/engine/ — エージェントループ、ルーター、状態遷移
internal/llm/    — OpenAI互換クライアント、パース補正、リトライ
internal/context/ — コンテキスト管理、メッセージ履歴、縮約
internal/rpc/    — JSON-RPCサーバー（stdin/stdout）
pkg/protocol/    — JSON-RPCメッセージ型、イベント定義
pkg/tool/        — Tool interface、スキーマ定義
```

## スキル

- `/decision` — 技術判断の記録・参照（.claude/skills/decisions/）
- `/investigate` — 実験検証の記録（.claude/skills/investigation/）
- `/agent-design` — エージェント設計の知識ベース（.claude/skills/agent-design/）

## 主要な設計判断

- ADR-001: 通信方式にJSON-RPC over stdioを採用
- ADR-002: SLLMのツール呼び出しにルーター（JSON mode）+ 単一ツール パターンを採用
- ADR-003: ルーター引数の直接使用（サブエージェントステップの省略）
- ADR-004: コンテキスト管理にentry型ラッパーパターンを採用
- ADR-005: コンテキスト縮約に4段階カスケードパターンを採用
- ADR-006: サブエージェント統合にEngine内バーチャルツールパターンを採用
- ADR-007: サブエージェント結果の凝縮に文字数制限方式を採用
- ADR-008: Worktree実行モデルでworkDirをcontext.Context経由で伝達
- ADR-009: Coordinator並列実行にsync.WaitGroupと部分成功パターンを採用
- ADR-010: Ralph WiggumループにファイルシステムベースのSessionRunnerを採用
- ADR-011: PromptBuilderによるセクションベースのプロンプト構成を採用
- ADR-012: 権限とガードレールにPermissionChecker+GuardRegistryの2層分離パターンを採用
- ADR-013: JSON-RPCサーバーにRemoteToolアダプタ+PendingRequestsパターンを採用
- ADR-014: ストリーミング通知の配線にStreamingCompleter別インターフェース＋Engineコールバックを採用
- ADR-015: ラッパー側カスタムガード/Verifierをリモート呼び出しで統合する
- ADR-016: LLM 呼び出しをラッパー委譲するための `llm.execute` 逆 RPC を採用する

詳細は `/decision list` で確認。

## API仕様

ラッパー実装者向けの公開仕様は `docs/` 以下に置く。

- `docs/openrpc.json` — OpenRPC 1.2.6 仕様。全 JSON-RPC メソッド・パラメータ・結果・エラーを定義
- `docs/schemas/*.json` — 各型の独立 JSON Schema（Draft 2020-12）。quicktype / datamodel-code-generator 等で型生成に使用
- `docs/README.md` — 使い方とクライアント生成例
- `pkg/protocol/spec_test.go` — Go 型と OpenRPC 仕様の一致を検証

`pkg/protocol/methods.go` が真実の源で、`docs/openrpc.json` は手書きで同期する。バージョニング方針は `.claude/rules/protocol.md` を参照。

## SDK 設計哲学 — エージェントオブジェクトモデル (AOM)

`sdk/python/ai_agent/easy.py` が体現している使用感を言語化したもの。
JS SDK を拡張するときも、この設計哲学を基準にする。

### 一言で言うと

> **「エージェントを、会話状態を持つファーストクラスオブジェクトとして扱う」**

Git のコミット・Unix のプロセス・OOP のオブジェクトと同じ発想で、
エージェントを「生成・複製・合成・廃棄できる実体」として設計する。

### 5 つの特徴

| 特徴 | 具体的な表れ |
|---|---|
| **1. 状態カプセル化** | `input()` を呼ぶだけで会話が積まれる。スキル・MCP・サブエージェントの内部呼び出しはコンテキストに漏れない |
| **2. コンポーザブル** | `fork()` `add()` `add_summary()` `branch()` でエージェント同士が会話文脈を受け渡せる |
| **3. 設定の集約** | バイナリパス・環境変数・system_prompt を `AgentConfig` 1 つに束ねる。呼び出し側は設定オブジェクトを渡すだけ |
| **4. バッテリー内蔵** | RAG 検索・要約・バッチ・チェックポイントが標準搭載。外部ライブラリ不要 |
| **5. 段階的複雑性** | `agent.input(prompt)` だけで動く。必要になったら `fork()` や `batch()` を足せばいい |

### 低レベル API との住み分け

```
低レベル (sdk/python/ai_agent/client.py)
  └── JSON-RPC の生の呼び出しを扱う。プロトコルに忠実。
      外部ラッパーや細かい制御が必要な実装者向け。

高レベル (sdk/python/ai_agent/easy.py)
  └── AOM を実装。エージェントをオブジェクトとして使う開発者向け。
      内部では低レベル API を呼んでいる。
```

### 新機能を追加するときの判断軸

- **エージェントの「状態」に関わるか？** → `session.*` RPC を経由して Go に持たせる
- **複数エージェントの「合成」に関わるか？** → `easy.py` の `Agent` クラスに追加する
- **LLM を「道具」として使う操作か？** → `context.summarize` のように専用 RPC にする
- **SDK 側だけで完結するか？** → Go を触らず `easy.py` にメソッドを追加する

### 実装済みの新 RPC（AOM を支える Go 側の土台）

```
session.history   — 会話履歴エクスポート  (fork/add/export の基盤)
session.inject    — 会話履歴注入          (prepend / append / replace)
context.summarize — LLM による会話要約    (context() / add_summary() の基盤)
```

## 開発ルール

- 各Phaseの実装完了後は `/investigate` でシナリオベースの統合検証を実施し、結果を記録すること

## リリース

- リリース手順: [docs/RELEASE.md](docs/RELEASE.md)
- リリース直前のチェックリスト: [docs/RELEASE_CHECKLIST.md](docs/RELEASE_CHECKLIST.md)
- v0.1.0 リリースノート: [docs/RELEASE_NOTES_v0.1.0.md](docs/RELEASE_NOTES_v0.1.0.md)
- 貢献ガイド: [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md)
- バージョン同期は `pkg/protocol/version.go` / `sdk/python/pyproject.toml` /
  `sdk/js/package.json` / `README.md` バッジ / 本ファイル `## バージョン` /
  `CHANGELOG.md` 見出しの 6 箇所すべてを同一値に揃える
