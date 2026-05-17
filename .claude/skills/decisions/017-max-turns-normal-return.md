---
id: "017"
title: max_turns 到達時を Result の正常リターンに変更する
date: 2026-05-17
status: accepted
---

## コンテキスト

v0.2.1 以前、`engine.Run()` は `max_turns` 到達時に
`return nil, ErrMaxTurnsReached` で error を返していた。SDK 利用者からは
以下の不満が報告されている (A3):

> max_turns 到達はエラー。AgentError(code=-32603): max turns reached が
> throw される。**部分応答も握りつぶされる**。

具体的に問題なのは:
- ループ途中までに LLM が生成していた直近のアシスタント発話が捨てられる
- 利用側は `try/catch` で AgentError(code=-32603) を区別する必要があり、
  message 文字列マッチに依存する不安定なコード
- `delegate_task` で子エージェントが max_turns に達すると、子の進捗がすべて
  `"Subtask failed: max turns reached"` で握りつぶされ、親は何も活用できない

## 検討した選択肢

### A. 現状維持 (エラーで返す)
利用者が message でマッチして AgentError → AgentResult 変換する責任を負う。
SDK 側で吸収するならエラー型に部分応答フィールドを追加することになる。

### B. Result.Reason = "max_turns" で正常 return
Engine.Run() のループ末尾を `Result{Response: lastAssistantText, Reason: "max_turns",
Turns: maxTurns}` に変更する。エラーケースはあくまで「真の異常」だけ。

### C. 旧挙動オプトイン
`AgentConfig.maxTurnsAsError: true` でフラグ化。既定は B。

## 判断

**B を採用する**。

- ループ内で直近の `lr.Message.ContentString()` を `lastAssistantText` に保持
- 抜けた時に `Result{Response: lastAssistantText, Reason: "max_turns", Turns: maxTurns}`
  を return
- `ErrMaxTurnsReached` 定数自体は残置 (テストの mock LLM が「LLM レイヤ
  の中で」このエラーを返すケースで使われている; Engine.Run の戻り値からは消える)
- delegate 経路は `runDelegateChild` が `child.Run` の error を見ていたので、
  自動的に「正常 Result → `condenseDelegateResult` 経路」に乗るようになる。
  これは「子の部分応答を親の履歴にツール結果として保存する」より望ましい挙動

### Swift SDK の対応

- `AgentResult.Termination` enum (`.completed` / `.maxTurns` / `.userFixable` /
  `.maxConsecutiveFailures` / `.inputDenied` / `.other(raw:)`) を追加
- `AgentResult.isMaxTurns` / `isCompleted` で文字列比較なしに判別可能

## 理由

- **エラーは「異常」、Reason は「正常終了の種別」** という分離。max_turns は
  ハードリミットに引っかかっただけで、ハーネスとしては想定内の挙動
- **部分応答の有効活用**: max_turns 直前の LLM 出力には「ここまでわかった」
  内容が入っていることが多い。捨てる理由がない
- **delegate の連鎖が壊れない**: 子が部分応答を返せるので、親はその情報で
  「もう少し違う角度から」のリトライ判断ができる
- **既存テストの修正範囲**: `TestDelegateStep_ChildEngineError` が
  「Subtask failed」を期待しているのみ。これを「Subtask result」期待に
  改名・修正するだけで済む

## 影響

- **破壊的変更**: 旧 Python / JS SDK で `try ... except AgentError as e: if "max turns" in str(e):`
  のような書き方をしていた利用者は AgentResult.reason マッチに移行する必要がある
- **CHANGELOG での明示**: 0.3.0 の Breaking Changes として強く案内する
- **session_test 互換**: SessionRunner が completer のエラーとして
  `ErrMaxTurnsReached` を返す mock を使っているが、これは「LLM レイヤから
  例外が出てくる」シミュレーションで、Engine.Run の戻り値とは独立。変更不要
- **coordinator**: `coordinator_test.go` で `childFailErr: ErrMaxTurnsReached`
  が使われているが、これは並列子が失敗するシナリオの mock。Engine.Run の挙動
  変更とは独立で動作する
