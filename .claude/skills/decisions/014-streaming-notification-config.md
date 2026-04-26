---
id: "014"
title: ストリーミング通知の配線にStreamingCompleter別インターフェース＋Engineコールバックを採用
date: 2026-04-26
status: accepted
---

## コンテキスト

Phase 11 では `pkg/protocol` と `internal/rpc/notifier` に `stream.delta` / `context.status`
通知の型とヘルパーが既にあるが、Engine からは一度も呼ばれていない状態だった。
ラッパー言語側はトークン単位の応答もコンテキスト使用率の動的変化もリアルタイムで
受け取れず、JSON-RPC over stdio の双方向性が活かせていない。

ストリーミング配線にあたり以下が決まる必要があった:

- 既存の `Completer` インターフェース（mockCompleter、blockingCompleter、
  integrationCompleter、MCP クライアント等の複数実装あり）を壊さずに ChatCompletionStream を追加する方法
- Engine 内で chatStep / routerStep が streaming と非 streaming を選択する経路
- 「ストリーミング有効化」「context.status 通知」のラッパーからの設定方法
- 後方互換（Phase 10 までの動作と完全一致）の維持

## 検討した選択肢

### A. `Completer` インターフェースに ChatCompletionStream を追加

```go
type Completer interface {
    ChatCompletion(ctx, req) (*ChatResponse, error)
    ChatCompletionStream(ctx, req) (<-chan StreamEvent, error)
}
```

→ 既存の全モック・テスト実装（10 ヶ所以上）が一斉に壊れる。
   MCP クライアントなどストリーミング不要な実装も実装義務を負う。

### B. `StreamingCompleter` を別インターフェースとして分離（Completer を埋め込み）

```go
type StreamingCompleter interface {
    Completer
    ChatCompletionStream(ctx, req) (<-chan StreamEvent, error)
}
```

→ 既存実装は無変更。Engine は `comp.(StreamingCompleter)` の型アサーションで
   ストリーム対応有無を判定し、未対応なら通常呼び出しにフォールバック。

### C. Engine 内に streamingFlag を持たず常にストリーム集約

→ ストリーミングしない LLM バックエンドや、ストリーミング非対応の MCP 経路で
   不要なオーバーヘッド・複雑性が増える。

### コールバック設計の選択肢

#### a. StreamCallback / ContextStatusCallback を Engine の Functional Option

→ JSON-RPC レイヤと Engine が疎結合のまま、テストでも直接コールバックを観測できる。

#### b. Notifier を Engine が直接保持

→ Engine が rpc パッケージに依存することになり、`internal/engine` → `internal/rpc` の
   循環依存リスクと「コア層が通信層を知る」設計の歪みが生じる。

## 判断

**B（StreamingCompleter 別インターフェース）+ a（Engine Functional Option コールバック）** を採用。

具体構成:
- `internal/llm/stream.go`: `StreamingCompleter` インターフェース、`StreamEvent` 型、
  `Client.ChatCompletionStream` の SSE 実装を新規追加
- `internal/engine/option.go`: `WithStreaming(bool)`, `WithStreamCallback(func(delta, turn))`,
  `WithContextStatusCallback(func(ratio, count, limit))` を追加
- `internal/engine/engine.go`: `complete()` ヘルパーが streaming 設定 + 型アサーションで
  経路を選択。ストリームを drain して `*ChatResponse` に集約することで `chatStep` /
  `routerStep` のシグネチャを変えない
- `pkg/protocol/methods.go`: `AgentConfigureParams.Streaming` (`StreamingConfig{Enabled, ContextStatus}`)
- `internal/rpc/configure.go`: streaming.enabled が true のときのみ Notifier 経由のコールバックを
  Functional Option として注入

## 理由

- **既存 10+ 実装を一切変更せずに済む**（B の最大のメリット）
- **Engine は rpc パッケージに依存しない**（a を選ぶことで内部依存方向を維持）
- **`complete()` の集約パターン**により、router の JSON mode 出力もストリームで届くが
  最終的に集約 string を `ParseContent` できる（ADR-002 のルーター設計と整合）
- **streaming 未指定時は callback も未登録**なので、Phase 10 までの挙動と完全一致（後方互換）
- ContextStatus はターン開始時 + 縮約完了時の 2 ヶ所から発火する設計で、
  「進捗インジケータ」+「縮約イベント」の両方をラッパーが識別できる
- SSE パーサは `bufio.Scanner` ベースで `data: [DONE]` までを読み、`ctx` キャンセルで
  goroutine をクリーンに終了させる（ゴルーチンリーク防止、`-race` で確認済み）

## 影響

- 全 LLM バックエンドは `Completer` のみ実装すれば動作する。ストリーム対応は opt-in。
- Engine の `chatStep` / `toolStep` シグネチャは `(ctx, turn int)` に変わったが、
  ターン番号は streaming コールバックに渡すためだけで内部用途に閉じる。
- `agent.configure` の `streaming.enabled=true` で stream.delta、`streaming.context_status=true` で
  context.status 通知が ON。後者は前者と独立して有効化可能。
- `stream.end` は既存どおり handlers.go が `agent.run` 完了時に常に発火する（旧仕様維持）。
- Investigation 011 で実機 SLLM の SSE が想定通りパースされることを確認済み。
