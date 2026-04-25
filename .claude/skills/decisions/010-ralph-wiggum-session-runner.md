---
id: "010"
title: Ralph WiggumループにファイルシステムベースのSessionRunnerを採用
date: 2026-04-19
status: accepted
---

## コンテキスト
8Kコンテキストでは、複数のタスクを1つのセッションで処理しきれない場合がある。コンテキストウィンドウを超えた継続性を提供する仕組みが必要。

## 検討した選択肢

### A: SessionRunner（JSON進捗ファイル + 新Engine/セッション）
- メリット: 各セッションが8Kをフル活用。ファイルシステムが「メモリ」として機能。クラッシュ耐性（進捗ファイルから再開可能）
- デメリット: セッション間でコンテキストが完全にリセットされる

### B: コンテキストリセット（既存Engineのメッセージ履歴をクリア）
- メリット: Engine再生成のオーバーヘッドなし
- デメリット: 状態の引き継ぎ方法が曖昧。クラッシュ耐性なし

### C: 要約チェーン（前セッションの要約を次セッションに注入）
- メリット: コンテキストの連続性が高い
- デメリット: SLLMの要約品質に依存（ADR-005, 007で懸念を確認済み）

## 判断
選択肢Aを採用。internal/engine/にSessionRunnerを配置し、JSON形式の進捗ファイルで状態を管理する。

## 理由
- Ralph Wiggumパターンの核心は「ファイルシステムがコンテキストウィンドウを超えた継続性を提供する」こと
- JSON進捗ファイルはSessionRunnerが読み書きし、SLLMのシステムプロンプトに整形して注入する。SLLMが直接ファイルを読む必要はない
- アトミック書き込み（tmp + rename）でクラッシュ時のデータ破損を防止
- pending → failed のタスクは自動的に再試行される（最大maxSessions回）

## 影響
- SessionRunner: NewSessionRunner(completer, SessionConfig) で生成
- RunLoop(ctx, tasks): 2フェーズループ（初期化 → 繰り返し実行）
- 進捗ファイル形式: ProgressFile{Tasks, StartedAt, UpdatedAt}
- 各イテレーションで新Engine生成（8Kコンテキストをフル活用）
- buildSessionPromptで現在のタスクと全体進捗をシステムプロンプトに注入
