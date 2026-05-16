---
id: "016"
title: LLM 呼び出しをラッパー委譲するための llm.execute 逆 RPC を採用する
date: 2026-05-16
status: accepted
---

## コンテキスト

v0.2.1 まで、コアの LLM 呼び出しは `internal/llm/Client` が OpenAI 互換の
HTTP POST に固定されていた (`SLLM_ENDPOINT` 環境変数で指定)。SDK
(Python / JS / Swift) はあくまで Go バイナリを JSON-RPC で叩くだけで、LLM 自体への
入出力には触れない。このため以下の要望に応えられない:

- Anthropic / Bedrock / Vertex AI など OpenAI 非互換 API を使いたい
- ローカル ollama / llama-server / WebLLM など独自バイナリプロトコルを使いたい
- テスト時に LLM を mock したい (HTTP サーバを立てるのは過剰)
- API キー管理・レート制御・キャッシュ・観測をラッパー側に集中させたい
- ラッパー側でリクエスト/レスポンスを書き換えたい (テンプレート挿入、PII マスキング等)

「ラッパー側でどんな形式でも叩けるようにしたい」というユーザ要求を満たす必要がある。

## 検討した選択肢

### A. Go 側にドライバプラグインを増やす
`--llm-driver openai|anthropic|ollama` のように Go バイナリにマルチドライバを実装する。
SDK は変更不要だが、新フォーマット追加のたびに Go 再ビルドが必要。
ラッパー側固有のロジック (社内 API、独自認証) は載せられない。

### B. RemoteCompleter (`llm.execute` 逆 RPC) + LLMConfig.mode 切替
ADR-013 の RemoteTool / ADR-015 の RemoteGuard と同じく、`llm.Completer` interface を
実装するプロキシ `RemoteCompleter` を `internal/rpc/` に置く。`agent.configure` の
`llm.mode="remote"` 指定時に Engine の completer をこのプロキシに差し替え、すべての
ChatCompletion を `llm.execute` (コア → ラッパー) として送信する。

### C. WASM プラグイン
言語制約は外せるが ADR-015 と同じ理由 (規模過大・デバッグ困難) で却下。

## 判断

**B. RemoteCompleter + LLMConfig.mode 切替パターンを採用する**。

具体構成:
- プロトコル: `MethodLLMExecute = "llm.execute"` を新設 (コア → ラッパー)
- `LLMExecuteParams = { request: RawMessage }` / `LLMExecuteResult = { response: RawMessage }`
  で OpenAI 互換 `ChatRequest` / `ChatResponse` を透過 (`json.RawMessage` でフィールド増加に追従)
- `LLMConfig = { mode: "http" | "remote", timeout_seconds: int }` を `AgentConfigureParams` に追加
- `internal/rpc/remote_completer.go` で `llm.Completer` を実装する透過プロキシ
- `rebuildEngine` で `p.LLM.Mode == "remote"` のとき `prev.Completer()` を `RemoteCompleter` で置換
- デフォルトは `mode=""` (= 既存 HTTP クライアント維持) で完全後方互換
- DefaultLLMTimeout = 120s (ツール 30s より長め: 外部 API レイテンシ込み)
- SDK 側: Python `set_llm_handler` / JS `setLLMHandler` / Swift `setLLMHandler` で
  逆 RPC ハンドラを登録。高レベル API は `AgentConfig.llm_handler` 1 行で完結し、
  指定時は自動で `LLMConfig(mode="remote")` を適用 (明示指定が優先)

## 理由

- **既存 interface を変更しない**: `llm.Completer` は ADR-001 以来の基幹契約。
  RemoteCompleter はそれを実装するだけで Engine 側コードゼロ変更
- **RemoteTool / RemoteGuard パターンの再利用**: PendingRequests / Server.SendRequest /
  RawMessage 透過パターンがそのまま使える
- **AOM 哲学との整合**: 「SDK 側だけで完結する操作は `easy.py` にメソッドを追加する」
  という CLAUDE.md の判断軸どおり。Go コアは driver 知識を持たず、ラッパーが
  自由に LLM を選べる
- **後方互換**: `llm.mode` 未指定なら従来通り HTTP クライアントを使用。既存ラッパーは無修正で動作
- **streaming は当面ノンサポート**: `StreamingCompleter` interface は実装しない。
  ストリーミングを使いたい時は `llm.mode="http"` のままにする (将来必要になったら
  `llm.execute.stream` 通知で拡張可能、後方互換)
- **テスト容易性**: ハンドラを差し込むだけで HTTP モック不要。実 SLLM 無しの CI で
  agent.run の挙動全体を検証できる

## 影響

- **後方互換性**: `LLMConfig` は新規 omitempty フィールド。既存の `agent.configure`
  呼び出しは無変更で従来挙動
- **switch の不可逆性**: 現状 `mode="remote"` に切り替えた後 `mode="http"` に戻しても、
  元の HTTP クライアントは再生成されないため、`Completer` のままになる
  (実害は少ないが、必要なら `cmd/agent/main.go` で初期 client を保持して
  `rebuildEngine` に渡す方針で対応可能)
- **タイムアウト**: 個別タイムアウトは `LLMConfig.timeout_seconds` で上書き可能。
  0 のときは 120s
- **ストリーミング**: `streaming.enabled=true` と `llm.mode="remote"` を同時指定した場合、
  `RemoteCompleter` が `StreamingCompleter` を満たさないため、Engine 側の fallback で
  非ストリーミング呼び出しに落ちる (動作するが delta 通知は飛ばない)
- **SDK API 拡張**: Python `set_llm_handler` / JS `setLLMHandler` / Swift `setLLMHandler`
  という新 API。高レベル AgentConfig には `llm_handler` (Python/Swift) / `llm`
  (JS は handler 引数なし、setLLMHandler 個別呼び出し) でアクセス
- **js-browser SDK は対象外**: 元から `Completer` interface でプラガブル
  (Go バイナリを使わないスタンドアロン構成)
- **エラー診断性**: ラッパー側でハンドラが例外を投げた場合、JSON-RPC エラーとして
  ChatCompletion が失敗する。Go コア側は `remote llm: wrapper returned error: <message>`
  形式で wrap する
