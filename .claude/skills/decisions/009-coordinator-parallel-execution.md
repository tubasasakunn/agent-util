---
id: "009"
title: Coordinator並列実行にsync.WaitGroupと部分成功パターンを採用
date: 2026-04-19
status: accepted
---

## コンテキスト
複数の独立したサブタスクを並列に実行するCoordinatorパターンの実装において、並行実行の制御方法と失敗時の挙動を決定する必要がある。

## 検討した選択肢

### A: sync.WaitGroup + 部分成功
- メリット: 1タスクの失敗が他のタスクを中止しない。全結果（成功/失敗）を集約して親に返せる
- デメリット: 全タスク完了まで待つため、1タスクが極端に遅いと全体がブロックされる

### B: errgroup.Group（1エラーで全キャンセル）
- メリット: 失敗を検出したら早期に停止できる
- デメリット: 成功したタスクの結果も捨てられる。SLLMのエラー率を考えると、1つの失敗で全体を中止するのは厳しすぎる

### C: goroutine + channel（結果を逐次受信）
- メリット: 最も柔軟。タイムアウト制御も容易
- デメリット: 複雑度が高い。現段階では過剰

## 判断
選択肢Aを採用。sync.WaitGroupで全タスクの完了を待ち、成功/失敗の両方を結果に含める。

## 理由
- SLLMはエラー率が高い（investigation/004で確認）。1タスクの失敗で全体を中止すると実用性が低い
- context.Contextのキャンセルは全子Engineに自然伝播するため、ユーザー主導の中止は保証される
- 結果集約にバジェット制限（coordinateMaxChars / タスク数）を適用し、コンテキスト圧迫を防ぐ
- coordinate_tasksはバーチャルツールとして実装（ADR-006パターンの拡張）

## 影響
- coordinate_tasksがルータープロンプトに追加（~200トークン）
- coordinatorEnabled OptionでFork時に無効化（ネスト防止）
- coordinateMaxChars（デフォルト3000）で集約結果のサイズを制限
- 並列テストにはconcurrentMockCompleterを使用
