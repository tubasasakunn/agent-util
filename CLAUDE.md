# ai-agent

SLLM（小さいLLM）を動かすエージェントハーネスをGoで構築するプロジェクト。

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

詳細は `/decision list` で確認。

## API仕様

ラッパー実装者向けの公開仕様は `docs/` 以下に置く。

- `docs/openrpc.json` — OpenRPC 1.2.6 仕様。全 JSON-RPC メソッド・パラメータ・結果・エラーを定義
- `docs/schemas/*.json` — 各型の独立 JSON Schema（Draft 2020-12）。quicktype / datamodel-code-generator 等で型生成に使用
- `docs/README.md` — 使い方とクライアント生成例
- `pkg/protocol/spec_test.go` — Go 型と OpenRPC 仕様の一致を検証

`pkg/protocol/methods.go` が真実の源で、`docs/openrpc.json` は手書きで同期する。バージョニング方針は `.claude/rules/protocol.md` を参照。

## 開発ルール

- 各Phaseの実装完了後は `/investigate` でシナリオベースの統合検証を実施し、結果を記録すること
