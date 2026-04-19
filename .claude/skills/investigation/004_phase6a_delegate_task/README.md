# 004: Phase 6a delegate_task 基盤検証

## 目的

Phase 6aで実装したサブエージェント委譲（delegate_task）が、実際のエージェント利用シナリオで正しく動作するか確認する。ユニットテスト（internal/engine/delegate_test.go）とは異なり、エンドツーエンドのシナリオで検証する。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| 1 | 単一delegate完全フロー | ルーター→delegate_task選択→子Engine実行→結果凝縮→親に返却→最終応答生成の完全フロー |
| 2 | delegate結果凝縮 | 子Engineが長い応答(3000文字)を返した場合、最大文字数に凝縮されて親に格納される |
| 3 | delegateキャンセル伝播 | 親のcontext.Cancelが子Engineに正しく伝播し、子が即座に停止する |
| 4 | delegate失敗時の回復 | 子Engineがエラーを返した場合、親はエラー内容をツール結果として受け取り、続行する |
| 5 | delegate後のコンテキスト使用率 | delegate結果格納後のコンテキスト使用率が閾値内に収まる（8Kシミュレーション） |
| 6 | ネストdelegateの防止 | 子Engineのルータープロンプトにdelegate_taskが含まれない |

## 手段

- 方法: Go テストプログラム（mockCompleter使用、SLLM不要）
- 対象: `internal/engine.Engine` (Fork, delegateStep, condenseDelegateResult)
- 実行: `go test -v ./.claude/skills/investigation/004_phase6a_delegate_task/`

## 結果

### 総合評価

| # | テスト | 結果 | 判定 |
|---|---|---|---|
| 1 | 単一delegate完全フロー | 親→子Engine（ツール使用含む）→結果返却→最終応答、2ターン・135トークンで正常完了 | **PASS** |
| 2 | delegate結果凝縮 | 3000文字→582文字に凝縮（delegateMaxChars=500 + メタヘッダー）、truncatedマーカー付き | **PASS** |
| 3 | delegateキャンセル伝播 | 子EngineのLLM呼び出し中にcancel()→context.Canceledがバブルアップ、呼び出し2回で停止 | **PASS** |
| 4 | delegate失敗時の回復 | 子Engine maxTurns到達→"Subtask failed: max turns reached"が親に返却→親が別手段でリカバリ、3ターンで完了 | **PASS** |
| 5 | delegate後のコンテキスト使用率 | 8Kコンテキストで通常ツール使用+delegate×2回、4ターン・180トークン、サブタスク結果2件が正しく格納 | **PASS** |
| 6 | ネストdelegatの防止 | 子ルータープロンプト（1073文字）にdelegate_task不在を確認、親プロンプトには存在を確認 | **PASS** |

### 詳細

#### シナリオ 1: 単一 delegate 完全フロー
- 親ルーター→delegate_task選択→子Engine生成（Fork、delegate無効）
- 子Engine内でread_fileツール使用→結果を分析→chatStepで応答返却
- 凝縮結果が親の履歴に格納（[Subtask result (2 turns, 70 tokens)]形式）
- 親が凝縮結果を元に最終応答を生成
- LLM呼び出し合計6回（親ルーター1 + 子ルーター2 + 子chat1 + 親ルーター1 + 親chat1）

#### シナリオ 2: delegate 結果凝縮
- delegateMaxChars=500に設定、子Engineが3000文字の応答を返す
- 凝縮結果: 582文字（500文字本文 + truncatedマーカー + メタヘッダー）
- `[... truncated, original: 3000 chars ...]` マーカーが正しく付加
- 親の履歴でのコンテキスト消費を最小限に抑制

#### シナリオ 3: delegate キャンセル伝播
- 子EngineのChatCompletion呼び出し時にctx.Cancel()を実行
- context.Canceledが子Engine→delegateStep→親Engineの順でバブルアップ
- 呼び出し2回目（子ルーター）でキャンセル検出、即座に停止

#### シナリオ 4: delegate 失敗時の回復
- 子Engineが10ターン全てでツール実行を繰り返しmaxTurns到達
- ErrMaxTurnsReachedが"Subtask failed: max turns reached"としてツール結果に変換
- 親はこのエラーを見て代替手段（直接read_file）に切り替え
- 3ターンで正常終了（delegate失敗を含む）

#### シナリオ 5: delegate 後のコンテキスト使用率
- 8192トークンコンテキストで通常ツール使用1回 + delegate×2回の現実的シナリオ
- 最終リクエストのメッセージ数: 8（system + user + tool_call + tool_result + delegate_call + delegate_result × 2 + ...）
- サブタスク結果2件が正しく格納され、各結果に[Subtask result]マーカー付き
- 4ターンで完了、トークン使用量180（8Kの2.2%、十分な余裕）

#### シナリオ 6: ネスト delegate の防止
- 親ルータープロンプト: delegate_task定義を含む
- 子ルータープロンプト（1073文字）: delegate_task定義を含まない
- Fork()時のdelegateEnabled=false設定が正しく機能
- 無限再帰の可能性を構造的に排除

### 実機検証（Gemma 4 E2B）

mockCompleter による自動テストに加え、実際のSLLM（Gemma 4 E2B, localhost:8080）で手動検証を実施。

#### テスト A: 明示的にdelegate_taskを指示
- プロンプト: `delegate_taskを使って、go.modファイルを読んでGoバージョンを調べてください`
- 結果: **PASS**
  - 親ルーターが delegate_task を選択（580tok, 16.4s）
  - 子Engineが read_file でgo.modを読み取り→「go 1.25.1」と回答（2ターン, 3190tok）
  - 親が凝縮結果を受け取り最終応答を生成（合計2+2=4ターン）
  - レイテンシ: 全体約60秒（子Engine含む）

#### テスト B: delegate_taskでファイル行数カウント
- プロンプト: `delegate_taskを使って、internal/engine/delegate.goファイルの行数を数えてほしい`
- 結果: **PASS**
  - 親→delegate_task→子がread_file→76行と回答→親が最終応答
  - 子Engine内でSLLMが正しくファイル内容を解析し行数をカウント

#### テスト C: delegateなし（SLLMの自律判断）
- プロンプト: `CLAUDE.mdの内容を分析して、このプロジェクトの概要を3行で要約して`
- 結果: SLLMはdelegate_taskを使わず直接read_fileを選択
- 評価: **正しい判断**。単一ファイル読み取り+要約はdelegateが不要

#### テスト D: 複雑な複数ファイルタスク
- プロンプト: `internal/engine/engine.goとinternal/engine/delegate.goの2つのファイルを読んで説明して`
- 結果: SLLMがtoolフィールドに配列を返しJSONパースエラー
- 評価: **delegate_taskの問題ではなくSLLMのJSON出力品質の問題**（既知の制限）

#### 実機検証の所見
1. **delegate_taskの実機動作を確認**: SLLMがdelegate_taskを正しく選択し、子Engineが独立コンテキストで実行・完了する
2. **レイテンシの現実**: delegate使用時は子Engine分のLLM呼び出しが追加されるため、全体で30-60秒程度かかる
3. **SLLMの判断品質**: 明示的に指示すればdelegate_taskを使うが、自発的にdelegateを選ぶケースは未観測。より複雑なタスクでの検証が必要
4. **トークン消費**: 子Engine単体で1000-3000トークン消費。8Kコンテキストの制約下では、delegateの回数に注意が必要

### 設計への示唆

1. **バーチャルツールパターンの有効性確認**: ADR-006の設計判断が正しく機能。toolStep()のif文1つの分岐で、既存フローを壊さずにサブエージェント委譲を実現
2. **結果凝縮の妥当性**: ADR-007の文字数制限方式が実用的。delegateMaxChars=500でも十分な情報を保持し、8Kコンテキストの消費は最小限
3. **エラー回復の堅牢性**: 子Engineの失敗がツール結果として親に返され、親が代替手段を取れる設計が有効。これはPhase 8（エラー回復）の基盤としても活用可能
4. **キャンセル伝播の即時性**: Go標準のcontext.Contextによるキャンセル伝播が子Engineまで正しく到達。goroutineリークの心配なし
5. **ネスト防止の構造的安全性**: Fork()で子Engineを生成する際にdelegateEnabled=falseが自動適用され、設定ミスによる無限再帰を防止
