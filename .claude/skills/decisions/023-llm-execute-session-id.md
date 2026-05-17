---
id: "023"
title: llm.execute に session_id と call_index を載せてラッパー側 KV cache を可能にする
date: 2026-05-17
status: accepted
---

## コンテキスト

ADR-016 で導入した `llm.execute` 逆 RPC は、ラッパーに OpenAI 互換
ChatRequest を透過させる構造。これにより Anthropic Messages や ollama を
ラッパー側で叩けるが、利用者から以下の問題報告 (E2):

> ChatRequest が毎回全 messages 配列を含む → 自前で ChatSession を組み立て
> 直すか、history を replay する必要 → **KV cache が効かず遅い**。

具体例:
- Anthropic の `cache_control` を使いたいが、`session_id` 相当の識別子がないので
  「同じ会話の続き」を判別できない
- ollama の context (前回の internal state) を再利用したいが、毎回新規セッション
  扱い
- 自前で `hashChatRequest(req)` してキャッシュキーにするのは fragile

## 検討した選択肢

### A. delta 送信 (messages の差分だけ送る)
最初の `llm.execute` でフル messages、以降は新規追加メッセージのみ。
**プロトコル設計が複雑** (どこから差分か、ラッパー側で結合復元する必要、
失敗時の resync 設計が必要)。0.x で取り組むには重い。

### B. session_id + call_index を ChatRequest と並列に送る
ChatRequest は今まで通り全 messages を含むが、別フィールドで:
- `session_id` — agent.run スコープのユニーク識別子
- `call_index` — 同一 run 内で何回目の llm.execute か (0 始まり)
ラッパーはこれをキーに自前で「prefix が一致するか」を判定して、Anthropic の
cache_control や ollama の context を再利用する。

### C. ラッパー側でハッシュ計算
SDK 利用者が `messages` を hash してキャッシュキーにする。
**全員が同じ実装を再発明する** ことになるので避けたい。

## 判断

**B を採用する**。

- `protocol.LLMExecuteParams`:
  - `Request   json.RawMessage `json:"request"`` (既存)
  - `SessionID string          `json:"session_id,omitempty"`` (新)
  - `CallIndex int             `json:"call_index,omitempty"`` (新)
- `internal/llm/session.go` に `WithSessionID(ctx, id)` /
  `SessionIDFromContext(ctx)` を新設 (engine と rpc の循環依存を避けるため
  llm パッケージに置く)
- `engine.Run()` 冒頭で `crypto/rand` から 8 byte hex のセッション ID を発番し、
  `llm.WithSessionID(ctx, ...)` で ctx に載せる
- `RemoteCompleter` インスタンスごとに `callIndex` カウンタを保持、
  `ChatCompletion` ごとにインクリメントして `LLMExecuteParams.CallIndex` に詰める
- Swift SDK 側:
  - 新 typealias `LLMHandlerWithSession (request, sessionID, callIndex)`
  - `setLLMHandlerWithSession` を追加、他 handler とは排他
  - 優先順位: WithSession > Streaming > 通常

## 理由

- **delta 送信より破壊性が低い**: 既存 SDK はフィールドを無視するだけで動作
- **ラッパーに最大限の自由度**: cache をどう実装するかはラッパー側が選べる
  (Anthropic の cache_control、ollama の context、自前 LRU、何でも)
- **agent.run スコープの session_id**: 「会話の続き」を識別するのに ちょうど良い粒度。
  AOM の `fork()` で派生した子は別 session として扱われる (それで正しい)
- **`call_index` の有用性**: 同じ session の中で「最初の呼び出し」と「2 回目以降」を
  判別したいケース (cache_control 付与のタイミング) で必須
- **`crypto/rand` 8 byte hex**: 16 文字、衝突確率 2^-64。エンジンの run スコープなら
  十分

## 影響

- **後方互換性**: 新フィールドは omitempty。旧 SDK / 旧ラッパーは無視するだけ
- **`Run(ctx, ...)` の ctx 上書き**: engine 内部で `ctx = llm.WithSessionID(ctx, ...)`
  と再代入している。外部から渡された ctx の cancellation / timeout は維持される
- **fork で session_id が変わる**: 子の `Agent` は別 `RawAgent` instance を持ち、
  その中の Engine が新規 session を発番する。これは意図通り
- **OpenRPC schema 更新**: `docs/openrpc.json` の `LLMExecuteParams.properties` に
  `session_id` / `call_index` を追加 (required からは外す)
- **delta 送信は将来再検討**: 「同じ session_id の連続呼び出しは messages の差分
  だけ送る」最適化は将来 ADR-024 等で検討
- **Python / JS SDK の追従**: 同等の `LLMHandlerWithSession` 追加は次回フォロー
