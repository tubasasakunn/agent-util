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

- `/decision` — 技術判断の記録・参照（.claude/decisions/）
- `/investigate` — 実験検証の記録（.claude/investigation/）
- `/agent-design` — エージェント設計の知識ベース（.claude/agent-design/）

## 主要な設計判断

- ADR-001: 通信方式にJSON-RPC over stdioを採用
- ADR-002: SLLMのツール呼び出しにルーター（JSON mode）+ 単一ツール パターンを採用

詳細は `/decision list` で確認。
