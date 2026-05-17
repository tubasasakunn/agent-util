---
id: "018"
title: server.info ハンドシェイク RPC で SDK ⇔ バイナリ互換性を可視化する
date: 2026-05-17
status: accepted
---

## コンテキスト

v0.2.1 まで、Go バイナリは自身のバージョンや対応機能を一切公開していなかった。
ラッパー (SDK) が `agent --rpc` で起動するバイナリが古く、新規追加された
RPC (例: `llm.execute`) を実装していない場合、症状は以下のように分かりにくく出る:

> バイナリのバージョンと SDK のバージョン整合性にフォールバックがない。
> ./agent が llm.execute 未対応バイナリだと「localhost:8000 connection
> refused」というミスリードなエラーで気付くまで時間を食う (E3)

「OpenAI 互換 HTTP サーバが立っていない」「JSON-RPC 接続が切れた」「メソッドが
存在しない」など複数の原因が同じエラー文に集約され、調査が長引く。

## 検討した選択肢

### A. version 番号だけを起動時に stdout に印字
バイナリが `--version` で出すような従来パターン。SDK は標準出力をパースする
必要があり、JSON-RPC との混線リスクがある。

### B. `server.info` RPC を新設
JSON-RPC として `server.info` (params なし) を呼ぶと
`{library_version, protocol_version, methods[], features{}}` が返る。
SDK は `start()` の中で自動呼び出しし、`AgentConfig.versionCheck` ポリシー
(`strict` / `warn` / `skip`) に従って判定する。

### C. configure 応答に version を載せる
既存 `agent.configure` の result に version フィールドを足す。configure 前に
判定したいユースケース (機能の有無で configure 内容を切り替える) には不適。

## 判断

**B を採用する**。

- `protocol.MethodServerInfo = "server.info"`
- `ServerInfoResult { LibraryVersion, ProtocolVersion, Methods []string, Features map[string]bool }`
- 旧バイナリは `server.info` を実装していないので JSON-RPC `-32601 method not found`
  を返す → SDK 側で「`llm.execute` 等も使えない可能性が高い」と判別可能
- Swift SDK の `AgentConfig.versionCheck`:
  - `.strict` 不一致 / 旧バイナリで `AgentError` throw
  - `.warn` (デフォルト) stderr に警告だけ書いて続行
  - `.skip` ハンドシェイク自体スキップ
- 既知 features の例: `remote_tools` / `remote_guards` / `remote_verifiers`
  / `remote_judge` / `streaming` / `context_status` / `session_injection`
  / `llm_execute`

## 理由

- **`-32601` を「server.info 未実装の古いバイナリ」と一意に解釈できる**: 他の
  RPC は実装されているメソッドだけが respond するので、特定の `method not found`
  を「旧バイナリ」マーカーとして使える
- **`features` map によるソフト判定**: 将来 `llm.execute.stream` などを追加した
  場合に、SDK 側が `info.supports("llm_execute_stream")` で振り分けられる
- **`AgentConfig.versionCheck` で利用者が運用ポリシーを選べる**: 開発中は
  `.warn` で柔軟、本番は `.strict` で確実な失敗を選べる
- **OpenRPC スキマには `server.info` を載せない判断**: `session.*` 系も
  spec_test の `expectedMethods()` に登録しておらず、内部 RPC は柔軟に拡張する
  慣習に合わせる (将来正式化する際にまとめて load することも可)

## 影響

- **後方互換性**: 旧 SDK + 新バイナリは無問題 (SDK が `server.info` を呼ばないだけ)
- **新 SDK + 旧バイナリ**: `versionCheck = .warn` がデフォルトなので警告で続行
- **`aiAgentSDKLibraryVersion` 定数**: Swift SDK 側のソースコードに `0.3.0` を
  ハードコード。SwiftPM では Git tag でしかバージョン管理しないため、
  `pkg/protocol/version.go` と手動同期する
- **CLAUDE.md / VERSIONING.md のバージョン同期表に Swift の同期項目を追加**
