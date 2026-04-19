---
id: "005"
title: コンテキスト縮約に4段階カスケードパターンを採用
date: 2026-04-19
status: accepted
---

## コンテキスト
8KコンテキストのSLLM（Gemma 4 E2B）では、read_file等のツール結果1回でコンテキストの大部分を消費する。Phase 4で閾値超過イベントの基盤は実装済みだが、実際の縮約処理がなかった。長い対話を維持するための多段階縮約戦略が必要。

## 検討した選択肢

### A: BudgetTrim をメッセージ追加時に適用
- メリット: 巨大なツール結果がそもそもコンテキストに入らない
- デメリット: Manager がポリシーを持つことになり、責務が曖昧になる。ルール「縮約は副作用を伴う操作なので、呼び出し側が明示的にトリガーする」に反する

### B: 縮約関数を func([]llm.Message)[]llm.Message で統一
- メリット: ルールファイルの推奨シグネチャに一致
- デメリット: entry型のトークンキャッシュにアクセスできず、カスケード中間の使用率判定に毎回全件再推定が必要

### C: 4段階カスケード（BudgetTrim → ObservationMask → Snip → Compact）を entry ベースで実装
- メリット: トークンキャッシュ活用、各段階後の早期リターン、ACON研究の裏付け（推論トレース > 生ツール出力）
- デメリット: ルールファイルのシグネチャと若干異なる（ただし公開境界は Manager.Compact()）

### D: Compact（LLM要約）を最初から SLLM で有効化
- メリット: 情報の保持率が高い
- デメリット: SLLMの要約品質が未検証。不正確な要約がコンテキストを汚染するリスク

## 判断
選択肢Cを採用。4段階カスケードを package-internal 関数として実装し、Manager.Compact() がカスケードを実行する。Compact は Summarizer コールバックで外部注入し、デフォルトは nil（スキップ）。

## 理由
- entry ベースにすることでトークンキャッシュを活用し、カスケード各段階後に即座に使用率を判定して早期リターンできる
- 公開境界は Manager.Compact() なので、内部表現（entry型）の漏洩はない
- ObservationMask はACON研究（26〜54%トークン削減で精度95%+維持）とJetBrains Junieパターンに基づく
- ToolCall-ToolResult ペア整合性は adjustKeepBoundary() で保護境界を自動拡張して維持
- Compact のSLLM有効化は要約品質の検証後に判断する（段階的アプローチ）

## 影響
- パラメータのデフォルト値: BudgetMaxChars=2000, KeepLast=6, TargetRatio=0.6
- Engine.step() の冒頭で閾値超過チェック → カスケード自動実行
- WithCompaction Option なしの場合は既存動作と完全互換
- Phase 5ではCompact（LLM要約）は基盤のみ。品質検証後に有効化を判断する
