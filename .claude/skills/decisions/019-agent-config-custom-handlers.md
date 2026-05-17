---
id: "019"
title: AgentConfig.custom* で「ハンドラ定義」と「configure 経由の有効化」を統合する
date: 2026-05-17
status: accepted
---

## コンテキスト

v0.2.1 以前の Swift SDK では、ライフサイクル設計に構造的バグがあった (B1〜B4):

> `Agent.start()` の中で configure が呼ばれるため、AgentConfig.guards /
> verify.verifiers / judge に自前の名前を入れると「unknown guard」/「judge not
> registered」で必ず失敗する。
>
> 一方 `register*` は `Agent.start()` の **後** でしか呼べない。つまり自前 guard
> を AgentConfig 経由で有効化する手段がない。
>
> 回避策は `RawAgent` を直接使うことだが、その途端 `Agent` クラスの
> stream / fork / batch / add / branch / context などの便利 API が全部使えなくなる。

これは「定義」と「有効化」が異なるレイヤに分断されているのが根本原因。
`GuardsConfig(input: ["x"])` は **どの名前を有効化するか** の宣言で、
`registerGuards([... GuardSpec(name: "x") ...])` は **ハンドラ実体の登録**。
両者は AgentConfig だけで完結すべき。

## 検討した選択肢

### A. `start()` を 2 段階に分ける
`Agent.start()` を subprocess 起動だけにし、`configure` は初回 `input()` で
lazy に呼ぶ。`register*` を間に挟める。利点: 既存 API 互換。
欠点: `start()` 後すぐ configure 起因のエラーが取れない (初回 input まで遅延)、
利用側のメンタルモデルが「2 段階」になって複雑。

### B. AgentConfig に `customTools` / `customGuards` / `customVerifiers` / `customJudges` を足す
ハンドラ実装を AgentConfig に持たせ、`Agent.start()` 内で
「subprocess → register all → configure」の順に処理する。
`registerGuards(...)` を後から呼ぶ既存パスは引き続き利用可能 (動的スキル追加用)。

### C. `GuardsConfig` を `[GuardSpec]` ベースに変更
名前リストではなく GuardSpec オブジェクトそのものを保持。
名前と実装が同じ場所にあるが、`AgentConfig.guards` フィールド型を破壊的に変更する。

## 判断

**B を採用する**。

- `AgentConfig.customTools: [Tool]?` / `customGuards: [GuardSpec]?` /
  `customVerifiers: [Verifier]?` / `customJudges: [String: JudgeHandler]?`
- `Agent.start()` の処理順:
  1. `RawAgent(...)` で subprocess 起動 + wireHandlers
  2. `server.info` ハンドシェイク (ADR-018)
  3. `llm.execute` ハンドラ差し込み (ADR-016)
  4. **`customTools` → `customGuards` → `customVerifiers` → `customJudges` を順に register**
  5. `configure` を発行
- ステップ 5 の時点で 4 で登録された名前は既知になっているので、
  `AgentConfig.guards = GuardsConfig(input: ["my_guard"])` のような名前指定は
  必ず resolve できる
- 後から `agent.registerGuards(...)` を呼ぶ経路は維持 (動的スキルや fork 後の
  カスタマイズ用)

## 理由

- **「定義」と「有効化」が同じ AgentConfig 内で完結する**: 利用者は init 文 1 つで
  すべての設定を組める
- **`Agent` の高レベル API (`fork` / `branch` / `batch` 等) を引き続き使える**:
  RawAgent に降りる必要がない
- **既存 API 後方互換**: `customGuards` を渡さなければ旧来通り。利用者が
  `agent.registerGuards(...)` を後から呼ぶ経路も生きている
- **エラー検出タイミングが早まる**: 旧来は configure 失敗で「unknown guard」が出ていたのが、
  start() 内の register 失敗 (重複名等) と configure 失敗 (名前 unknown) の両方が
  start() 内で起きるようになり、利用者は 1 箇所で catch できる

## 影響

- **後方互換性**: 新フィールドは Optional。既存コードは変更不要
- **Python / JS SDK は別途実装**: 同等の `AgentConfig` 拡張が必要 (本リリースでは
  Swift SDK のみ; 他言語は次回追補)
- **AgentE2ETests の追加検証**: 「customGuards で deny → input が GuardDenied で
  reject される」のシナリオを E2E 化する余地あり
- **register* 経路の使い分け**: README に「`AgentConfig.custom*` は静的構成、
  `agent.register*(...)` は動的追加」と明示する
