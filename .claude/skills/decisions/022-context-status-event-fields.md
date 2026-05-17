---
id: "022"
title: context.status 通知に履歴イベント情報を載せて可観測性を上げる
date: 2026-05-17
status: accepted
---

## コンテキスト

利用者からの可観測性に関する 4 つの指摘 (C2, C3, G3 関連):

> コンテキストがターン毎に直線的に膨らむ (13% → 14% → 15%)。**何が積まれて
> いるか UI 側から分からない**。
>
> `CompactionConfig` の効果 (4 段カスケード) がブラックボックス。
> **何バイト落ちたか、どう要約したかが取れない**。

`context.status` 通知は `usage_ratio / token_count / token_limit` の 3 つだけ。
ゲージは描けるが「いま何が起きたか」が読めない。

## 検討した選択肢

### A. 既存 `context.status` にフィールド追加
`last_event` / `last_message_role` / `compaction_delta` を omitempty で追加。
schema 後方互換 (旧 SDK は新フィールドを無視するだけ)。

### B. 新規 `context.event` 通知メソッドを追加
`MethodContextEvent` を新設し、`context.status` は周期的なゲージ専用にする。
クリーンだが、SDK 側で 2 系統のハンドラを書く必要がある。

### C. emit を「event 経路」と「status 経路」で分ける
`emitContextStatus()` (周期) と `emitContextEvent(...)` (履歴イベント) で
コールバックを分離する。protocol を 2 メソッドにする一方で、Engine 側は
場所ごとに使い分けが必要。

## 判断

**A を採用する**。

- `protocol.ContextStatusParams` に omitempty で追加:
  - `LastEvent` — `"user_added"` / `"assistant_added"` / `"tool_added"` /
    `"compacted"` / `""` (周期更新)
  - `LastMessageRole` — 直近に追加されたメッセージの role
  - `CompactionDelta` — event=="compacted" の場合、削減トークン数
- `engine.ContextStatusCallback` を `(ratio, count, limit, event, role, delta)`
  に拡張 (signature 変更; 旧呼び出し側 2 件を更新)
- `emitContextStatus()` 既存メソッド = `emitContextStatusWith("", "", 0)`
- `emitContextStatusWith(event, role, delta)` を履歴更新箇所 (UserMessage 追加、
  Compact 完了) で呼ぶ
- Swift SDK 側: `StatusEventCallback` を `StatusCallback` と並列で提供。
  両方指定すれば両方呼ばれる

## 理由

- **JSON Schema 後方互換**: omitempty で追加なので、旧 SDK は新フィールドを
  単に無視する
- **Engine 側 emit 箇所が局所**: 追加されたのは Run() 冒頭の UserMessage 追加と
  maybeCompact 内の 2 箇所のみ。tool/assistant の追加は今後追加余地
- **SDK 側で UI に直結**: `event == "compacted"` のときに「縮約が走りました
  (-820 tokens)」という Toast を出す、`event == "user_added"` のときに
  ターン区切りを描画する、といった応用が可能
- **`tool_calls` (G1/G2) と相補的**: ツール呼び出し回数は AgentResult.toolCalls
  に集約 (run 終了後)、context イベントは context.status 通知 (run 中ライブ)
  というレイヤ分離

## 影響

- **engine.ContextStatusCallback の signature 変更**: 既存呼び出し側
  (`internal/rpc/configure.go`, `internal/engine/streaming_test.go`) の 2 箇所
  を更新。今後追加されるテストも同 signature に合わせる
- **`Notifier.ContextStatus` は委譲ラッパーとして残し**、新規
  `ContextStatusWithEvent(...)` を追加。**JSON-RPC wire format は同じ** メソッド名
  `context.status` だが、新フィールドが乗る
- **assistant_added / tool_added はまだ emit していない**: 当面 user_added と
  compacted のみ。将来 chatStep / executeAndRecord にも emit を仕込めば、
  各ターンの「LLM 応答が積まれた」「ツール結果が積まれた」が観測可能
- **OpenRPC schema 更新**: `docs/openrpc.json` の ContextStatusParams.properties
  に `last_event` / `last_message_role` / `compaction_delta` を追加。
  required からは外す
