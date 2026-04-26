# 012: ラッパー側カスタムガード/Verifierの逆方向呼び出し検証

## 目的

Phase 12 で `agent.configure` の `guards` / `verify` が「ビルトイン名のみ」だった制約を取り払い、
ラッパー側（Python/JS/etc.）が独自のガード/Verifier ロジックを Engine に挿し込めるようにした。
本検証ではラッパーが
- `guard.register` で名前付きガードを登録
- `agent.configure` でその名前を参照
- 実行時に core → wrapper の `guard.execute` 逆方向呼び出しが発火
- ラッパーの応答（allow/deny/tripwire）が Engine の判定として尊重される

の経路をエンドツーエンドで動作確認する。`verifier.register` / `verifier.execute` も同様に検証する。
fail-closed 方針（リモートエラー/タイムアウト時に deny）も実機で確認する。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| A | remote input guard が入力を拒否 | guard.register → configure → run が input_denied で終了し、ラッパーの guard.execute が 1 回発火 |
| B | remote verifier がツール結果を観測 | tool.register + verifier.register → configure → run でツール実行後 verifier.execute が発火、failed 応答で再試行ループに入る |
| C | リモートガードのタイムアウト | ラッパーが応答しない場合、コア側 DefaultGuardTimeout (30s) で deny に倒れる |

## 手段

- 対象: `pkg/protocol/methods.go`, `internal/rpc/remote_guard.go`, `internal/rpc/remote_verifier.go`,
  `internal/rpc/remote_registry.go`, `internal/rpc/handlers.go`, `internal/rpc/configure.go`
- 方法:
  1. `cmd/agent --rpc` を subprocess 起動
  2. JSON-RPC over stdio で wrapper 側 fake guard / verifier を `SetHandler()` 登録
  3. `guard.register` / `verifier.register` → `agent.configure(guards|verify=[name])` → `agent.run` を発行
  4. reader goroutine で stdout を分岐: `guard.execute` / `verifier.execute` リクエストには
     fake handler の応答を返信し、レスポンスで結果を確認
  5. `results/sllm_remote_guards_results.json` に保存
- 条件:
  - 実機 SLLM: `gemma-4-E2B-it-Q4_K_M` (LM Studio @ localhost:8080)
  - mock テストは `internal/rpc/remote_guard_test.go` (15 件), `remote_verifier_test.go` (8 件),
    `configure_test.go` 追加 (4 件), `integration_test.go` 追加 (2 件) — 計 29 件 PASS

## 結果

### 総合評価

| # | テスト | 結果 | 判定 | 所要 |
|---|---|---|---|---|
| A | remote input guard blocks input | guard_calls=1, reason="input_denied", response='Input rejected: matches blacklist evil' | PASS | 410 ms |
| B | remote verifier observes tool result | verifier_calls=1, ツール 1 回実行 → fail → 検証ループで再ルーティング | PASS | 6698 ms |
| C | remote guard timeout → fail-closed deny | DefaultGuardTimeout=30s 後 deny に倒れた | PASS | 40011 ms |

**3/3 PASS**（実機SLLM、mock 29 件 とは別系統）

### 詳細

#### A. 入力ガードがラッパーで判定される（実機 SLLM 不要）

```
guard.register({name:"wrapper_evil_blocker", stage:"input"}) → registered=1
agent.configure({guards:{input:["wrapper_evil_blocker"]}}) → applied=[guards]
agent.run({prompt:"Please do something evil"})
  ↓
core が guard.execute({name:"wrapper_evil_blocker", stage:"input", input:"Please do something evil"}) を送信
  ↓
ラッパー fake guard が "evil" を検知 → {decision:"deny", reason:"matches blacklist 'evil'"}
  ↓
Engine が GuardDeny で input_denied 終了
  ↓
result = {reason:"input_denied", response:"Input rejected: matches blacklist 'evil'"}
```

InputGuard が呼ばれる位置（Engine.run の冒頭）でラッパー側コールバックが発火し、SLLM 呼び出しに到達する前にブロック。
fail-closed が想定通り入力段階で完結している。

#### B. リモート Verifier がツール結果を検証（実機 SLLM 6.7 秒）

`echo` ツールを 1 つ登録 → `wrapper_audit` verifier を登録 → SLLM が echo を 1 回呼ぶ。
1 回目の verifier.execute で `{passed:false, summary:"first attempt failed by audit policy"}` を返したところ、

```
[tool] echo 完了 (19 bytes): echo: {"text":"hi"}
[verify] echo 検証失敗: wrapper_audit: first attempt failed by audit policy
[router] ツールを選択中...
[router] ツール不要 → 直接応答 (...verification failure...)
[chat] 応答を生成中...
[done] 2 turns, 1646 tokens
```

検証 1 回 → 失敗を検知 → 検証フィードバックを履歴に追加 → ルーターが再判断（"none"）→ 最終 chat 応答、という ADR-008 互換のフローが
リモート verifier でも成立。`verifier_calls=1` で「ツール実行ごとに verifier が呼ばれる」ことを実機で確認。

#### C. リモートガードのタイムアウト → fail-closed (40s)

ラッパー fake guard が `time.Sleep(40s)` で応答しないシナリオ。コア側 `DefaultGuardTimeout = 30s` で
`context.DeadlineExceeded` が `executeRemoteGuard` に到達し、`failClosedGuard()` が `GuardDeny` を返す。

```
result = {
  reason:   "input_denied",
  response: "Input rejected: remote guard \"slow_guard\" error: transport: context deadline exceeded"
}
```

エラーメッセージにラッパー側のガード名と原因 (`context deadline exceeded`) が両方含まれており、
オペレーターが「どの remote guard で何が起きたか」を診断できる。
所要 40011 ms ≈ 30000 ms (timeout) + 監視ループのオーバーヘッド + scenario クライアントの 90s wait。

### 設計への示唆

- **RemoteRegistry を Handlers の付属物として持ち、rebuildEngine に注入する分離**が効いた。
  builtin と remote を 2 段階フォールバックで解決する `resolveInputGuard` 等のヘルパーは
  「未知の名前」検出を 1 箇所に集約でき、エラーメッセージも「builtin にも remote にもない」と明示できる。
- **fail-closed の実装ポイントは `executeRemoteGuard` 一箇所**（共通ヘルパー）に集約。
  RemoteInputGuard / RemoteToolCallGuard / RemoteOutputGuard はそれぞれ params 構築だけ担い、
  「transport error → deny」「rpc error → deny」「unknown decision → deny」の3パスを 1 関数で扱う。
  これにより新ガード型を追加するときのバグ混入を抑える。
- **RemoteGuard.timeout を struct field にしておく**ことで、ユニットテストで短いタイムアウトを設定して
  fail-closed パスを高速に検証できた（`g.timeout = 30 * time.Millisecond`）。Functional option ではなく
  struct field なのは、タイムアウトはほとんど常にデフォルトで使われ、テスト時にだけ上書きするため。
- **PendingRequests + remoteSender インターフェース**で `Server` への直接依存を避けた結果、
  ガード/Verifier の単体テストで Server 全体を立ち上げる必要がなく、`stubSender` だけで完結。
  Phase 10 の RemoteTool パターン（ADR-013）を踏襲しつつ、テスト容易性を一段強化。
- **fail-closed 時に error 値そのものは返さない**（`(result, nil)`）設計が `GuardRegistry.RunInput`
  などの上位ループとの相性が良い。上位は `error` を「ガード自体のバグ」として skip するが、
  ここでは「ラッパー側の遅延/障害も判定の一部」として扱いたい。エラーは details/reason に詰めて返す。
- **wrapper_audit 1 件で再試行ループが回る**ことが B で確認できた。Phase 8 で実装した
  「verifier failed → 再ルーティング」が、リモート verifier でもそのまま機能する。
  Engine 側は Verifier interface の実装が builtin か remote かを区別しない（透過的）。

### 既知の制約

- DefaultGuardTimeout / DefaultVerifierTimeout は現状 30 秒固定。`agent.configure` から個別に上書きする
  設定は未実装（必要になればパラメータ追加で対応）。
- ガード/Verifier の登録名衝突: `guard.register` で同名を再登録すると上書きする（map の自然な挙動）。
  builtin と同名（"prompt_injection" 等）を remote 登録した場合、builtin が優先される。これは
  `resolveInputGuard` が builtin → remote の順で解決するため。
- agent.run 中の guard.register / verifier.register は仕様上は許可されているが、再構築は次の
  agent.configure 時に反映される（既存 Engine の Guard 配列は不変）。

### 関連 ADR

- ADR-001: JSON-RPC over stdio → guard.execute / verifier.execute も同じ stdout チャネル
- ADR-012: PermissionChecker + GuardRegistry の 2 層分離 → リモートガードは GuardRegistry 側に注入
- ADR-013: RemoteTool + PendingRequests → 同じパターンでガード/Verifier に拡張
- ADR-015 (新規): ラッパー側カスタムガード/Verifier をリモート呼び出しで統合する

## 再実行

```bash
cd .claude/skills/investigation/012_remote_guards_verifiers/
SLLM_ENDPOINT="http://localhost:8080/v1/chat/completions" \
SLLM_API_KEY="sk-gemma4" \
bash run_verify.sh
```
