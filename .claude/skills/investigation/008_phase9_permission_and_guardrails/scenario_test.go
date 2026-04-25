// Phase 9 統合検証シナリオ
//
// 実行方法:
//   go test ./internal/engine/ -run "TestRun_Permission|TestRun_InputGuard|TestRun_ToolCallGuard|TestRun_OutputGuard|TestRun_NoGuards|TestRun_GuardAndPermission" -v
//
// このファイルは検証記録用であり、実際のテストコードは internal/engine/engine_test.go に含まれる。
//
// 検証したシナリオ:
//
// 1. パーミッションパイプライン:
//   - TestRun_PermissionDenied_ToolBlocked: Denyルールによるツール拒否
//   - TestRun_PermissionAllowed_ReadOnly: ReadOnlyツールの自動承認
//   - TestRun_PermissionAsk_Approved: UserApprover承認フロー
//   - TestRun_PermissionAsk_Rejected: UserApprover拒否フロー
//   - TestRun_PermissionFailClosed_NoApprover: fail-closedデフォルト
//   - TestRun_PermissionDenied_ConsecutiveFailures: 連続失敗による安全停止
//
// 2. ガードレール3層:
//   - TestRun_InputGuard_Deny: 入力ガードレールのDeny
//   - TestRun_InputGuard_Tripwire: 入力ガードレールのTripwire
//   - TestRun_ToolCallGuard_Deny: ツール呼び出しガードレールのDeny
//   - TestRun_ToolCallGuard_Tripwire: ツール呼び出しガードレールのTripwire
//   - TestRun_OutputGuard_Deny: 出力ガードレールのDeny
//   - TestRun_OutputGuard_Tripwire: 出力ガードレールのTripwire
//
// 3. 共存テスト:
//   - TestRun_GuardAndPermission_Coexist: パーミッション+ガードレールの共存
//   - TestRun_NoGuards_BackwardCompat: 後方互換
//   - TestRun_NoPermissionChecker_BackwardCompat: パーミッション未設定の後方互換
//
// 4. 単体テスト:
//   - TestPermissionChecker_Check: パイプライン全段階のテーブル駆動テスト（13ケース）
//   - TestGuardRegistry_RunInput: 入力ガードレジストリ（7ケース）
//   - TestGuardRegistry_RunToolCall: ツール呼び出しガードレジストリ（4ケース）
//   - TestGuardRegistry_RunOutput: 出力ガードレジストリ（3ケース）
//   - TestAuditLogger_Log: 監査ログ出力（3ケース）
//   - TestClassifyError_TripwireError: トリップワイヤのエラー分類

package investigation
