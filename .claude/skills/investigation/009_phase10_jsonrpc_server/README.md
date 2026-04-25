# 009: Phase 10 JSON-RPCサーバーの統合検証

## 目的

Phase 10で実装したJSON-RPC over stdioサーバーが正しく動作することを検証する。
双方向通信（ラッパー→コア、コア→ラッパー）の完全なフローを確認する。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| 1 | プロトコル型テスト | JSON-RPC 2.0メッセージの正しいシリアライズ/デシリアライズ |
| 2 | サーバー基盤テスト | リクエスト/レスポンスの送受信、エラーハンドリング |
| 3 | RemoteToolテスト | ラッパー側ツールのプロキシ動作、タイムアウト |
| 4 | ハンドラテスト | tool.register/agent.run/agent.abortの各メソッド |
| 5 | E2E統合テスト | ツール登録→実行→結果の完全フロー |
| 6 | CLI統合テスト | --rpcフラグでのサーバーモード起動・ビルド |
| 7 | パイプ手動テスト | bashでJSON-RPCメッセージを直接送信して動作確認 |

## 手段

- 対象: internal/rpc, pkg/protocol, cmd/agent
- 方法: go test（自動テスト30件） + パイプ経由の手動テスト
- 条件: go 1.25.1、外部依存なし

## 結果

### 総合評価

| # | テスト | 結果 | 判定 |
|---|---|---|---|
| 1 | プロトコル型テスト | 7/7 PASS | PASS |
| 2 | サーバー基盤テスト | 9/9 PASS | PASS |
| 3 | RemoteToolテスト | 4/4 PASS | PASS |
| 4 | ハンドラテスト | 7/7 PASS | PASS |
| 5 | E2E統合テスト | 5/5 PASS | PASS |
| 6 | CLI統合テスト | ビルド成功、全パッケージPASS | PASS |
| 7 | パイプ手動テスト | 不正メソッド→エラー、ツール登録→成功 | PASS |

### 詳細

#### 1. プロトコル型テスト (pkg/protocol/)
- Request/Response のシリアライズ/デシリアライズ ラウンドトリップ
- IsNotification() の判定（ID nil/非nil）
- NewResponse, NewErrorResponse, NewNotification ファクトリ
- AgentRunParams, ToolExecuteParams, ToolRegisterParams の JSON ラウンドトリップ

#### 2. サーバー基盤テスト (internal/rpc/)
- ハンドラ登録→リクエスト処理→レスポンス返却
- 未定義メソッド → MethodNotFound エラー
- 不正JSON → ParseError
- 無効なJSONRPCバージョン → InvalidRequest
- 通知（ID なし）にはレスポンスを返さない
- Notify() で通知メッセージを送信
- SendRequest() → Resolve() のラウンドトリップ
- 並行書き込みでデータ破損しない（10 goroutine同時書き込み）
- EOF/空行の正常処理

#### 3. RemoteTool テスト
- tool.Tool インターフェースの完全実装（コンパイル時検証）
- Execute() → tool.execute リクエスト送信 → レスポンス変換
- エラーレスポンス → tool.Result{IsError: true}
- タイムアウト → context.DeadlineExceeded

#### 4. ハンドラテスト
- tool.register: 正常登録（2ツール）、重複登録エラー
- agent.run: 正常実行→完了結果、排他制御（二重実行→AgentBusy）
- agent.abort: 非実行時→Aborted=false
- 不正パラメータ → InvalidParams エラー

#### 5. E2E統合テスト (io.Pipe()接続)
- SimpleRun: agent.run→完了レスポンス
- ToolRegisterAndRun: tool.register→agent.run→tool.execute→結果（完全双方向通信）
- StreamNotifications: stream.end通知の受信確認
- InvalidMethod: 未定義メソッドのエラーハンドリング
- AgentAbort: 実行中のagent.runをabortで中断

#### 6. CLI統合テスト
- `go build ./cmd/agent/` でバイナリ生成成功
- `go test ./...` で全8パッケージPASS（既存Phase 1-9のテストも含む）

#### 7. パイプ手動テスト
```bash
# 不正メソッド → エラー
$ echo '{"jsonrpc":"2.0","method":"nonexistent","id":1}' | ./agent --rpc
{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found: nonexistent"},"id":1}

# ツール登録 → 成功
$ echo '{"jsonrpc":"2.0","method":"tool.register","params":{"tools":[...]},"id":1}' | ./agent --rpc
{"jsonrpc":"2.0","result":{"registered":1},"id":1}
```

### 実機SLLM検証（Gemma 4 E2B）

**検証スクリプト**: `sllm_rpc_verify.go`

**シナリオ**: MCP的なツール（read_file, list_directory）をJSON-RPCで登録し、SLLMにファイルシステム操作を依頼

**プロンプト**: "List the files in cmd/agent/ and then read main.go. Tell me what the main function does."

**結果**: PASS

**フロー詳細**:
1. `tool.register` で read_file, list_directory の2ツールを登録 → `{"registered":2}`
2. `agent.run` でタスク依頼
3. SLLM（ルーター）が `coordinate_tasks` を選択 → 2つのサブタスクに分割
4. サブエージェント1: `read_file` を選択 → `tool.execute` がラッパーに送信 → main.go の内容を返却
5. サブエージェント2: `list_directory` を選択 → `tool.execute` がラッパーに送信 → ファイル一覧を返却
6. 親エージェントが結果を統合、`"none"` で最終応答へ
7. 最終応答: main関数の役割を2-3文で正確に要約

**観測ポイント**:
- `tool.execute` 呼び出し: 2回（read_file, list_directory）
- 終了理由: `completed`
- 消費ターン数: 2（ルーター→ツール→ルーター→応答）
- レイテンシ: ルーター判断 ~5-10秒、最終応答生成 ~3秒
- SLLMは `coordinate_tasks`（バーチャルツール）を適切に選択し、並列実行を活用

**SLLMの最終応答**:
> The main function in main.go serves as the entry point for the AI agent application. It handles the initialization and configuration of the agent, including parsing command-line flags and environment variables to set up the LLM client. Based on the configuration, it then decides whether to run the agent in an RPC mode or a tool execution mode.

**RPCモードでのビルトインツール除外**: 検証中に `read_file` のビルトイン重複登録エラーを発見。RPCモードではラッパーがすべてのツールを提供する設計のため、ビルトインツールを登録しないよう `cmd/agent/main.go` を修正した。

### 設計への示唆

- **双方向通信の設計は安定**: RemoteTool + PendingRequests パターンにより、tool.Toolインターフェースを変更せずにラッパー側ツールを透過的に統合できた
- **サブエージェント統合も動作**: SLLMがcoordinate_tasksを選択し、RemoteTool経由のツールを並列サブエージェントで実行できた（Phase 6のバーチャルツール+Phase 10のRemoteToolが連携）
- **RPCモードとCLIモードの分離**: RPCモードではビルトインツール・パーミッション・ユーザー承認を無効化し、ラッパーに委譲する設計が正しい
- **排他制御が重要**: Engineはステートフルなため、agent.runの同時実行制御は必須。sync.Mutexで実現
- **ストリーミング通知**: StepCallback による通知は後方互換で追加可能。将来的に stream.delta でターンごとの応答差分を送信する拡張が容易
- **全Phase回帰テスト**: Phase 10の追加でPhase 1-9の既存テスト（engine, llm, context等）に影響なし。Engine.RegisterTool()の追加は完全に後方互換
