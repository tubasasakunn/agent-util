# 008: Phase 9 権限とガードレールの統合検証

## 目的

Phase 9で実装したPermissionChecker、3層ガードレール、トリップワイヤが、エージェントループ全体を通して正しく機能することを確認する。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| 1 | パーミッションパイプライン（deny/allow/readOnly/ask） | 各段階の判定が正しく動作し、ツール実行を制御できること |
| 2 | ガードレール3層（入力/ツール呼び出し/出力） | 各層のDeny/Tripwireが正しくエージェントループに影響すること |
| 3 | トリップワイヤの即時停止 | TripwireErrorでRun()がerrorを返し停止すること |
| 4 | パーミッション+ガードレールの共存 | 両方が設定された場合の処理順序が正しいこと |
| 5 | 後方互換 | 未設定時にPhase 8以前と同一動作すること |
| 6 | 連続失敗カウント | permission_denied/guard_blockedが連続失敗に含まれること |
| 7 | Fork継承 | 子Engineがポリシー継承しUserApproverはnil（fail-closed）であること |

## 手段

- 対象: internal/engine パッケージ
- 方法: シナリオベースの統合テスト（scenario_test.go）+ 既存テストの回帰確認
- 条件: mockCompleter使用（ロジックの正しさ確認）

## 結果

### 総合評価

| # | テスト | 結果 | 判定 |
|---|---|---|---|
| 1 | パーミッションパイプライン | 全段階の判定が正しい | PASS |
| 2 | ガードレール3層 | 各層のDeny/Tripwireが正しく動作 | PASS |
| 3 | トリップワイヤ即時停止 | TripwireErrorでFatal停止を確認 | PASS |
| 4 | パーミッション+ガードレール共存 | パーミッション→ガードレールの順序で正しく動作 | PASS |
| 5 | 後方互換 | 未設定時は全許可、Phase 8以前と同一動作 | PASS |
| 6 | 連続失敗カウント | permission_denied/guard_blockedが連続失敗としてカウントされる | PASS |
| 7 | Fork継承 | ポリシー継承+UserApprover nil確認 | PASS |

### 詳細

#### テスト1: パーミッションパイプライン
- Denyルール合致 → PermDenied、ツール実行されない
- Allowルール合致 → PermAllowed、ツール実行される
- ReadOnlyツール（ルールなし）→ 自動承認
- UserApprover承認 → 実行、拒否 → PermDenied
- approverなし+ルールなし+非ReadOnly → fail-closed（PermDenied）
- Denyワイルドカード(*) → 全ツール拒否

#### テスト2: ガードレール3層
- 入力ガード Deny → input_denied理由で即Result返却、LLM呼び出しなし
- ツール呼び出しガード Deny → guard_blocked理由でContinue、フィードバックメッセージ追加
- 出力ガード Deny → output_blocked理由でTerminal、安全メッセージに差し替え

#### テスト3: トリップワイヤ
- 入力/ツール呼び出し/出力の各層でGuardTripwire → TripwireError返却
- classifyError → ErrClassFatal → Run()がerrorを返す
- errors.As(err, &TripwireError{}) で判定可能

#### テスト4: パーミッション+ガードレール共存
- パーミッションAllow → ガードレールDeny → ツール実行されない（ガードが拒否）
- パーミッションDeny → ガードレールは評価されない（パーミッションが先に拒否）

#### テスト5: 後方互換
- PermissionChecker nil + GuardRegistry nil → 全ツール許可
- 既存の全テスト（40件以上）が変更なしで通過

#### テスト6: 連続失敗カウント
- permission_denied が3回連続 → max_consecutive_failures で安全停止

#### テスト7: Fork継承
- シナリオテストで確認: 親のPermissionPolicyが子に継承される
- 子のUserApproverはnil → 非ReadOnlyツールはfail-closedで拒否

### テスト結果サマリー

```
go test ./internal/engine/ -count=1
ok  	ai-agent/internal/engine	8.389s

go test ./... -count=1
ok  	ai-agent/internal/context	0.372s
ok  	ai-agent/internal/engine	8.389s
ok  	ai-agent/internal/llm	4.618s
ok  	ai-agent/internal/tools/echo	0.644s
ok  	ai-agent/internal/tools/readfile	1.250s
ok  	ai-agent/pkg/tool	1.099s

go vet ./...
(no issues)
```

## 実機検証（Gemma 4 E2B）

### テスト1: ReadOnlyツール（read_file）の自動承認
```
$ go run ./cmd/agent/ "Read the file CLAUDE.md and tell me the project name"
```
- ルーターが read_file を選択
- `[audit] tool=read_file decision=allowed readonly=true reason="read_only auto-approve"` が出力
- ユーザー確認プロンプトは表示されない
- ファイル内容を読んで応答を生成: **PASS**

### テスト2: ツール不要の直接応答
```
$ go run ./cmd/agent/ "What is 2 + 3? Answer directly without using any tools."
```
- ルーターが "none" を選択
- パーミッションチェック / 監査ログは発生しない（正しい動作）
- 直接 "5" と応答: **PASS**

### テスト3: 監査ログの出力確認
```
$ go run ./cmd/agent/ "Read go.mod and tell me the module name"
```
- `[audit] tool=read_file decision=allowed readonly=true reason="read_only auto-approve"` が出力
- 各ツール実行ごとに監査ログが記録される: **PASS**

### テスト4: 非ReadOnlyツール（write_echo）のユーザー承認フロー
テスト用に `write_echo`（IsReadOnly=false）を登録し、REPLモードで検証。
```
$ printf 'Echo "hello world" using write_echo\ny\nexit\n' | go run test_repl_permission.go
```
- ルーターが write_echo を選択
- `[permission] Tool "write_echo" を実行しますか？ [y/N]:` が表示
- `y` 入力 → `[audit] tool=write_echo decision=user_approved readonly=false reason="user confirmed"`
- ツール実行成功、`ECHOED: hello world` が返却: **PASS**

### テスト5: 非ReadOnlyツールのユーザー拒否フロー
```
$ printf 'Echo "secret" using write_echo\nN\nexit\n' | go run test_repl_permission.go
```
- `N` 入力 → `[audit] tool=write_echo decision=user_rejected readonly=false reason="user rejected"`
- Permission denied がLLMにフィードバックされ、LLMが代替応答を生成: **PASS**

### バグ修正: stdin共有問題
実機検証中にバグを発見・修正:
- **問題**: `bufio.Scanner`（REPL）と`bufio.Reader`（Approver）が別バッファでstdinを読み、Scannerが先読みしてApproverにデータが渡らない
- **修正**: 1つの`*bufio.Reader`をREPLとApproverで共有する設計に変更
- **影響ファイル**: `cmd/agent/main.go`, `cmd/agent/approver.go`

## 設計への示唆

- PermissionCheckerとGuardRegistryの2層分離は、関心事の分離として妥当。テストの独立性も高い
- nil チェックによる後方互換は既存テストの修正不要で効果的
- isFailureReasonへの追加により、権限拒否が繰り返されるケースでも安全停止が機能する
- トリップワイヤをTripwireError（error型）として実装したことで、Run()の呼び出し元で適切にハンドリング可能
