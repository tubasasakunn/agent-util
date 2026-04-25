# 005: Phase 6b-d Worktree/Coordinator/Session 統合検証

## 目的

Phase 6b（Worktree実行モデル）、Phase 6c（Coordinator並列実行）、Phase 6d（Ralph Wiggumセッションランナー）の統合検証。ユニットテスト（internal/engine/*_test.go）とは異なり、実際のエージェント利用シナリオでの動作を検証する。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| 1 | Worktree ファイル分離 | worktreeモードで子Engineが別ディレクトリで動作し、完了後にクリーンアップされる |
| 2 | Worktree フォールバック | 非gitディレクトリでworktree作成失敗→forkモードにフォールバック |
| 3 | Coordinator 並列実行 | 3タスクの並列実行と結果集約 |
| 4 | Coordinator 部分失敗 | 1タスク成功/1タスク失敗で両方の結果が返る |
| 5 | Coordinator 結果バジェット | coordinateMaxCharsで各タスク結果が按分制限される |
| 6 | SessionRunner 全完了 | 3タスクが順次完了し、進捗ファイルに記録される |
| 7 | SessionRunner リトライ | 失敗タスクが自動再試行で最終的に成功する |
| 8 | Coordinator ネスト防止 | 子Engineにcoordinate_tasks/delegate_taskが含まれない |

## 手段

- 方法: Go テストプログラム（mockCompleter使用、SLLM不要）
- 対象: `internal/engine` の Worktree, Coordinator, SessionRunner
- 実行: `go test -v ./.claude/skills/investigation/005_phase6bcd_worktree_coordinator_session/`

## 結果

### 総合評価

| # | テスト | 結果 | 判定 |
|---|---|---|---|
| 1 | Worktree ファイル分離 | 子workDir=/var/.../agent-worktree-*（親repoと異なる）、完了後にディレクトリ消滅確認 | **PASS** |
| 2 | Worktree フォールバック | `fatal: not a git repository`→fork fallback→正常完了（2 turns, 45 tokens） | **PASS** |
| 3 | Coordinator 並列実行 | 3タスク並列→全結果集約→"All 3 analyses completed"（2 turns, 50 tokens） | **PASS** |
| 4 | Coordinator 部分失敗 | okタスク成功 + failタスクFAILED→両方の結果が集約されて親に返却 | **PASS** |
| 5 | Coordinator 結果バジェット | coordinateMaxChars=1500、2タスク×2000文字→各750文字に切り詰め | **PASS** |
| 6 | SessionRunner 全完了 | 3タスク順次完了→進捗ファイル688bytes、全タスクdone、タイムスタンプ設定済 | **PASS** |
| 7 | SessionRunner リトライ | タスク1: 初回失敗→タスク2成功→タスク1再試行成功（task1 attempts=4） | **PASS** |
| 8 | Coordinator ネスト防止 | 子システムプロンプト1038文字にcoordinate_tasks/delegate_task不在 | **PASS** |

### 詳細

#### シナリオ 1: Worktree ファイル分離
- 親workDir: テスト用gitリポジトリ（`setupTestRepo`で初期コミット付き作成）
- 子workDir: `/var/folders/.../agent-worktree-*`（一時ディレクトリ、git worktree add --detachで作成）
- capture toolがcontext.Contextから子のworkDirを取得→親repoと異なることを確認
- delegate完了後、worktreeディレクトリが自動削除されている（`os.Stat → IsNotExist`）

#### シナリオ 2: Worktree フォールバック
- 非gitディレクトリ（t.TempDir()）をworkDirに指定
- ログ出力: `[worktree] 作成失敗、fork にフォールバック: git worktree add: fatal: not a git repository`
- フォールバック後、通常のfork delegateとして正常完了

#### シナリオ 3: Coordinator 並列実行
- routingMockで親/子のレスポンスを分離（system promptに"focused assistant"を含むかで判定）
- 3タスク（a: count words, b: count lines, c: check format）が並列実行
- 全結果が `[Coordinated results: 3 tasks]` として集約

#### シナリオ 6: SessionRunner 全完了
- 進捗ファイル（JSON）の内容:
  - tasks: 3件、全てstatus="done"
  - 各タスクにsummary（Engineの応答テキスト）が記録
  - started_at, updated_atにISO8601タイムスタンプ

#### シナリオ 7: SessionRunner リトライ
- task1（"tricky task"）: 最初のEngine実行でエラー→status="failed"→後でpending優先でtask2実行→task2成功→task1再試行で成功
- task1のattempts=4（失敗時router+失敗、成功時router+chat）

## 実機検証

### 対象
- SLLM: Gemma 4 E2B（ローカル、8Kコンテキスト）
- エンドポイント: http://localhost:8000/v1/chat/completions

### 検証方法
`go run ./cmd/agent/` で以下のプロンプトを手動入力:

**テストA: delegate_task worktreeモード**
```
ファイルの内容を確認して。worktreeモードで委譲して
```
→ SLLMがmode="worktree"のdelegate_taskを選択できるか

**テストB: coordinate_tasks**
```
README.mdの行数と文字数を同時に調べて
```
→ SLLMがcoordinate_tasksを選択し、複数タスクのJSON配列を生成できるか

### 結果

| # | テスト | プロンプト | 結果 | 判定 |
|---|---|---|---|---|
| A | 基本ツール使用 | `Read the file CLAUDE.md` | read_file→応答生成、2 turns, 3338 tokens、57秒 | **PASS** |
| B | delegate_task | `Delegate a subtask to read CLAUDE.md and summarize it briefly` | delegate_task正しく選択→子Engine(read_file→要約)→凝縮結果→最終応答 | **PASS** |
| C | coordinate_tasks | `Use coordinate_tasks to do two things in parallel: count ADRs / list directory structure` | JSON配列を正確に生成、2タスク並列実行、結果集約→最終応答 | **PASS** |

#### テストB詳細: delegate_task
```
[delegate] サブタスクを委譲: Read the file CLAUDE.md and provide a brief summary.
[delegate] 理由: The user explicitly asked to delegate a task...
→ 子Engine: read_file → CLAUDE.md読込 → 要約生成 (2 turns, 2258 tokens)
[delegate] サブタスク完了 (2 turns, 2258 tokens)
→ 親: 凝縮結果を受け取り最終応答 (2 turns, 2386 tokens)
コンテキスト使用率: 9% (709/8192 tokens)
合計レイテンシ: ~100秒
```

#### テストC詳細: coordinate_tasks
SLLMが生成したJSON:
```json
{"tool":"coordinate_tasks","arguments":{"tasks":[
  {"id":"a","task":"read CLAUDE.md and count how many ADR entries there are"},
  {"id":"b","task":"read CLAUDE.md and list the directory structure"}
]},"reasoning":"..."}
```
- 2タスクが**実際に並列実行**（ログで2つの`[router] ツールを選択中...`が同時出力）
- 子a: ADR数=10を正確にカウント (2 turns, 2086 tokens)
- 子b: ディレクトリ構造を正確にリスト (2 turns, 1978 tokens)
- 親: 集約結果から最終応答を生成 (2 turns, 2401 tokens)
- コンテキスト使用率: 6% (474/8192 tokens)
- 合計レイテンシ: ~150秒（並列部分で約25秒の同時実行）

### 実機検証の所見
- **delegate_taskの明示的指示**: SLLMは「delegate」という単語をプロンプトに含めると正確にdelegate_taskを選択する
- **coordinate_tasksのJSON配列生成**: 懸念していたJSON配列生成は問題なし。SLLMはtasks配列を正確に構成できた
- **並列実行の効果**: 2つの子Engineが同時にLLM呼び出しを行い、合計レイテンシが直列実行より短い
- **worktreeモードは未テスト**: delegate_taskのmode="worktree"をSLLMが自発的に選択するかは確認していない（明示的指示が必要と思われる）

## 設計への示唆

1. **Worktree フォールバックは安全弁として有効**: 非gitディレクトリでも正常動作が保証される
2. **Coordinator の routingMock パターン**: 並列テストでは応答順序が非決定的なため、親/子を判定してルーティングするmockが必須
3. **SessionRunner の再試行**: pending → failed の優先度順で探索するため、新タスクが先に処理される。これは意図的な設計（失敗タスクは後回しにして進行を優先）
4. **coordinate_tasks のJSON配列生成はSLLM(Gemma 4 E2B)で問題なし**: 事前の懸念に反し、JSON配列を正確に生成できた。ただしプロンプトで明示的に指示した場合のみ検証済み
5. **並列実行はコンテキスト効率に寄与**: 各子Engineが独立コンテキストで動作するため、親のコンテキスト使用率は6%に抑えられた（集約結果のみ格納）
