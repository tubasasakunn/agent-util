---
id: "015"
title: ラッパー側カスタムガード/Verifierをリモート呼び出しで統合する
date: 2026-04-26
status: accepted
---

## コンテキスト

Phase 9-11 まで `agent.configure` の `guards` / `verify` はビルトイン名のリスト
（`prompt_injection`, `secret_leak`, `non_empty` 等）からの選択しか許さなかった。
ラッパー側（Python/JS/etc.）が独自のガードロジック（社内データ分類器、ML 分類モデル、
社内 API への問い合わせ、ポリシーエンジン等）を Engine に挿し込みたい場合、ビルトインを
増やすか、ラッパー側で agent.run の前に手動チェックする以外に手段がなかった。
特に LLM-as-judge 型の verifier や、ML 分類器の入力ガードは「ラッパーの言語/エコシステムで
書きたい」要求が強い。

## 検討した選択肢

### A. ビルトインを増やし続ける
シンプルだが Go コアにロジックが集中し、リリース毎に再ビルドが必要。
ラッパーごとの個別要件には対応不可。

### B. WASM プラグイン
ラッパー言語制約からは解放されるが、WASM ランタイム同梱・サンドボックス管理が
プロジェクト規模に対して過大。デバッグも難しい。

### C. RemoteGuard / RemoteVerifier アダプタ + RemoteRegistry
ADR-013 の RemoteTool パターンを踏襲し、`engine.InputGuard` / `ToolCallGuard` /
`OutputGuard` / `Verifier` interface を実装するプロキシ構造体を `internal/rpc/` に置く。
`guard.register` / `verifier.register` でラッパーから名前付き定義を受け取って
`RemoteRegistry` に保持し、`agent.configure` で名前参照されたら builtin → remote の順で
解決する。実行時は core → wrapper の `guard.execute` / `verifier.execute` 逆方向呼び出しで
判定をラッパーに委譲する。

## 判断

**RemoteGuard / RemoteVerifier アダプタ + RemoteRegistry パターンを採用する**。

具体構成:
- プロトコル: `MethodGuardRegister` / `MethodGuardExecute` / `MethodVerifierRegister` /
  `MethodVerifierExecute` を新設
- `RemoteInputGuard` / `RemoteToolCallGuard` / `RemoteOutputGuard` / `RemoteVerifier`:
  既存 interface を実装する透過プロキシ
- `RemoteRegistry`: 名前→Guard/Verifier の対応表、Handlers が保持
- 名前解決順: builtin → remote → error（fail-fast at configure）
- fail-closed: リモートエラー / タイムアウト / 不正な応答は全て GuardDeny / Passed=false に倒す
- DefaultGuardTimeout = DefaultVerifierTimeout = 30s

## 理由

- **既存 interface を変更しない**: `engine.InputGuard` 等は Phase 9 で確立した契約。
  RemoteXxx はその interface を実装するだけなので Engine 側コードゼロ変更
- **RemoteTool パターンの再利用** (ADR-013): PendingRequests / Server.SendRequest が
  そのまま使える。fail-closed 方針も RemoteTool の `IsConcurrencySafe()=false` と整合
- **2 段階フォールバック**: builtin と remote が共存可能。同名は builtin 優先（決定論性）
- **fail-closed の集約**: `executeRemoteGuard` ヘルパー 1 関数に集約することで、
  3 種のガード型でフォールバックパスのバグを生まない
- **テスト容易性**: `remoteSender` interface で Server を抽象化。stubSender だけで
  ガード/Verifier の単体テストが完結（Server 起動不要）

## 影響

- **後方互換性**: 既存の `agent.configure` 呼び出しは無変更で動作。
  remote 機能は `guard.register` / `verifier.register` を呼ばない限り起動しない
- **エラー診断性**: fail-closed reason に「remote guard "xxx" error: <原因>」を含める
  ため、ラッパー側のどのガードでなぜ deny されたかをトレースできる
- **再構築タイミング**: ガード/Verifier は agent.configure 時点の Engine に焼き込まれる。
  agent.run 中の guard.register は次の configure まで効果を持たない
- **タイムアウトは固定**: 個別 timeout の上書きは未実装。必要になれば
  `GuardDefinition.TimeoutMs` 等のフィールド追加で対応可能（後方互換）
- **登録の自然な上書き**: 同名再登録は map の上書き挙動でサイレント置換。これは
  Hot reload を意図しており、ラッパー側で意識的に制御することを期待
- **`RemoteRegistry` は Handlers の付属物**: Engine から見えないことで、Engine の
  ピュアな domain interface（InputGuard 等）と RPC 都合のレジストリが分離される
