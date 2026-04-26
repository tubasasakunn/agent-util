# 011: ストリーミング通知配線（stream.delta / context.status）の検証

## 目的

Phase 11 ではこれまで pkg/protocol と internal/rpc/notifier に定義だけ存在していた
`stream.delta` / `context.status` 通知を Engine から実際に発火できるよう配線した。
本検証では実機 SLLM（Gemma 4 E2B）で agent --rpc を立ち上げ、

- `streaming.enabled=true` を agent.configure 経由で有効化すると
  トークン単位の `stream.delta` 通知が JSON-RPC stdout に流れること
- `streaming.context_status=true` で `context.status` がターン進行と縮約に応じて発火すること
- streaming を未指定（デフォルト）の場合に通知が出ないこと（後方互換）
- `context.status` がコンテキスト縮約発生時にも追従すること

をエンドツーエンドで確認する。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| A | streaming.enabled で stream.delta が複数受信 | 「Hello world!」生成中に1件以上の `stream.delta` が届き、結合すると最終 response と一致 |
| B | streaming.context_status で context.status が複数発火 | 短いプロンプトでも `context.status` が >=2 件届く |
| C | streaming 未設定 → stream.delta なし | デフォルト（streaming セクション省略）で `stream.delta` 通知が一切出ないこと |
| D | 縮約時に context.status の ratio drop | 小さい `token_limit` で履歴が膨らみ縮約が走った後、`usage_ratio` が低下した status が観測される |

## 手段

- 対象: `internal/llm/stream.go`, `internal/engine/engine.go`, `internal/rpc/configure.go`,
  `internal/rpc/handlers.go`, `pkg/protocol/methods.go`
- 方法:
  1. `cmd/agent --rpc` を subprocess 起動
  2. JSON-RPC over stdio で `agent.configure(streaming=...)` → `agent.run` を発行
  3. 別 goroutine で stdout を読み続け、`stream.delta` / `stream.end` / `context.status`
     通知を逐次収集
  4. シナリオごとにスナップショットを比較し `results/sllm_streaming_results.json` に保存
- 条件:
  - 実機 SLLM: `gemma-4-E2B-it-Q4_K_M` (LM Studio @ localhost:8080)
  - mock テストは別系統で 5 件（`internal/llm/stream_test.go`）, 5 件（`internal/engine/streaming_test.go`）, 3 件（`internal/rpc/streaming_test.go`）通過

## 結果

### 総合評価

| # | テスト | 結果 | 判定 | 所要 |
|---|---|---|---|---|
| A | streaming.enabled で stream.delta | delta=3 件、response="Hello world!" と一致 | PASS | 2651 ms |
| B | streaming.context_status で context.status | status=2 件 (token_count=727 → 727) | PASS | 595 ms |
| C | streaming 未設定 → stream.delta なし | stream.delta=0、stream.end=1（既存仕様） | PASS | 516 ms |
| D | 縮約時に context.status の ratio drop | status=6 件、ratio 1.36 → 0.78 にドロップ観測 | PASS | 1641 ms |

**4/4 PASS**（実機SLLM、mock 13 件 とは別系統）

### 詳細

#### A. streaming.enabled で stream.delta（実機 SLLM 2.0 秒のチャット）

```
configure(streaming.enabled=true) → applied=[max_turns, system_prompt, streaming]
agent.run(prompt="Say 'Hello world!' and nothing else.")
  → stream.delta("Hello")
  → stream.delta(" world")
  → stream.delta("!")
  → stream.end({reason:"completed", turns:1})
result.response = "Hello world!"  // 結合と一致
```

LM Studio の SSE 応答（`data: {...}\n\n`）を `internal/llm/stream.go` が逐次パースし、
Engine が `WithStreamCallback` 経由で `Notifier.StreamDelta` を呼び、JSON-RPC 通知に変換できることを実機で確認。
チャンクは平均 1〜10 トークン単位で届き、`finish_reason=stop` で終了する。

#### B. streaming.context_status で context.status

短い対話（"say hi"）でも、turn 開始前 + ループ内ターン開始の 2 ヶ所から callback が発火し、
合計 2 件の `context.status` 通知が JSON-RPC に流れた。
両方とも `token_count=727 token_limit=8192 usage_ratio≈0.0887` で、最初のスナップショットを正しく送信できている。

#### C. streaming 未設定 → 通知なし（後方互換）

`agent.configure` を呼ばずにそのまま `agent.run` した場合、`stream.delta` 通知は 0 件、
`stream.end` のみが返る（`stream.end` は handlers.go が `agent.run` 完了時に常に出す既存挙動）。
streaming 機能を後付けしてもデフォルト挙動が壊れていないことを確認。

#### D. 縮約時に context.status の ratio drop

`token_limit=1024` + 長文プロンプト（Lorem ipsum × 30）で意図的にコンテキストを溢れさせ、
`compaction.target_ratio=0.5 budget_max_chars=200 keep_last=2` で縮約をトリガーしたシナリオ。
JSON-RPC に届いた `context.status` 通知の sample:

```
{token_count:1330, usage_ratio:1.30}  // 1回目run中
{token_count:1330, usage_ratio:1.30}
{token_count:1330, usage_ratio:1.30}
{token_count:1391, usage_ratio:1.36}  // 2回目run開始時（さらに増加）
{token_count:1391, usage_ratio:1.36}
{token_count: 795, usage_ratio:0.78}  // ← compaction後に ratio drop
```

`maybeCompact` 完了時に `emitContextStatus()` を呼ぶ実装により、
ラッパー側が「縮約が起きた瞬間」を usage_ratio 低下イベントとして検知できることを確認。

### 設計への示唆

- **StreamingCompleter を別インターフェースとして分離**したのは正解。
  既存の Completer 実装（mockCompleter, blockingCompleter, integrationCompleter, MCP クライアント等）を
  一切変更せずに、Client だけが streaming 経路を持てる。Engine 側は型アサーションで自動フォールバック。
- **Engine 内でのストリーム集約（`complete()`）**が後付け実装を簡潔にした。
  `chatStep` / `routerStep` は既存通り `*ChatResponse` を扱い、streaming は内部実装の差し替えにとどめた。
  router の JSON mode 出力もストリームで届くが、`ParseContent` は最終的な集約 string をパースするので互換性が保たれる。
- **ContextStatus はターン開始時 + 縮約完了時の 2 ヶ所**から発火する設計が、
  「進捗インジケータ」+「縮約イベント」の両方をラッパーに通知できる。
  通知粒度を細かくしたい場合は将来 `Add()` への Observer 経由でメッセージ追加ごとに発火する余地を残してある
  （ADR-005 の Observer パターンが下層にある）。
- **handlers.go の `agent.run` は既存の `stream.end` 通知を維持**したまま、新しい
  `stream.delta` / `context.status` を Engine コールバックで追加発火する。
  通知の3点セット（delta → end + status）でラッパー側 UI が「タイピング表示 + 進捗バー + コンテキスト警告」を
  独立したストリームとして扱える。
- **後方互換: streaming セクション省略時は callback も登録されない**ので、Phase 10 までの動作と完全一致。

### 関連 ADR

- ADR-001: JSON-RPC over stdio → 通知も同じ stdout チャネルを共有
- ADR-013: RemoteTool + PendingRequests → Server.Notify を再利用
- ADR-014（新規）: ストリーミング通知の配線（StreamingCompleter 別インターフェース + Engine コールバック）

## 再実行

```bash
cd .claude/skills/investigation/011_streaming_notification/
SLLM_ENDPOINT="http://localhost:8080/v1/chat/completions" \
SLLM_API_KEY="sk-gemma4" \
bash run_verify.sh
```
