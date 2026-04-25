# 006: Phase 7 コンテキスト構成の統合検証

## 目的

Phase 7（コンテキスト構成）の全サブフェーズ（7a-7d）の統合動作を検証する。
PromptBuilder によるセクションベースのプロンプト構築が、既存のエージェントループと正しく統合されていることを確認する。

### 検証項目

| # | テスト | 確認すること |
|---|---|---|
| 1 | セクション順序 | 優先度順にプロンプトが構成される |
| 2 | 動的セクション展開 | Dynamic 関数が呼ばれてコンテンツが展開される |
| 3 | リマインダー挿入 | 長い会話で末尾にリマインダーが挿入される |
| 4 | リマインダー非挿入 | 短い会話ではリマインダーなし |
| 5 | MEMORY インデックス | メモリインデックスが router プロンプトに含まれる |
| 6 | ツールスコーピング MaxTools | MaxTools でツール数が制限される |
| 7 | ツールスコーピング IncludeAlways | 必須ツールが MaxTools 内で保証される |
| 8 | 予約トークン整合性 | 動的/手動セクション含めて reservedTokens が正しい |
| 9 | E2E ツール実行 | PromptBuilder 経由でルーター→ツール実行フローが動く |

## 手段

- 対象: PromptBuilder, リマインダー, MemoryIndex, ToolScope
- 方法: mockCompleter によるシナリオテスト（9シナリオ）+ 実機 SLLM 検証
- 条件: Gemma 4 E2B, 8Kコンテキスト

## 結果

### 総合評価

| # | テスト | 結果 | 判定 |
|---|---|---|---|
| 1 | セクション順序 | system(0) < tools(30) < developer(1420) < instructions(1468) | PASS |
| 2 | 動的セクション展開 | Dynamic 関数が2回呼ばれ、コンテンツ展開確認 | PASS |
| 3 | リマインダー挿入 | 8メッセージ以上で [System Reminder] が挿入 | PASS |
| 4 | リマインダー非挿入 | 1メッセージ時はリマインダーなし | PASS |
| 5 | MEMORY インデックス | Knowledge Index ヘッダーとエントリが含まれる | PASS |
| 6 | ツールスコーピング MaxTools | MaxTools=2 で 3/3 中 2 ツールのみ含まれる | PASS |
| 7 | ツールスコーピング IncludeAlways | gamma が優先、残り枠に alpha | PASS |
| 8 | 予約トークン整合性 | reserved=798 tokens (9.7% of 8K) | PASS |
| 9 | E2E ツール実行 | echo ツール選択→実行→結果統合→最終応答 | PASS |

### 実機検証

**テストA: 基本応答（ツールなし）**
```
プロンプト: "What is 2 + 3? Answer with just the number."
結果: "5"
ルーター: tool=none (正確)
レイテンシ: 6.6s (ルーター) + 0.2s (応答)
トークン: 749 total
判定: PASS
```

**テストB: read_file ツール実行**
```
プロンプト: "Read the file CLAUDE.md and tell me the project name."
結果: "The project name is **ai-agent**."
ルーター: tool=read_file (正確)
レイテンシ: 5.6s (ルーター1) + 4.9s (ルーター2) + 2.5s (応答)
トークン: 2624 total
判定: PASS — PromptBuilder経由でもSLLMの応答品質に変化なし
```

### 設計への示唆

1. **Instructions の配置位置**: 当初 PriorityTools+1 (=11) に配置したが、developer セクション (20) より前に来てしまい "Lost in the Middle" パターンに反した。PriorityReminder-1 (=89) に修正し、プロンプト末尾（ユーザー入力に近い位置）に配置した。これによりJSON応答フォーマットの指示がSLLMの注意をより受けやすくなる

2. **予約トークンの効率**: 8Kコンテキストの9.7%（798 tokens）が予約。ツール2個+メモリ1件+リマインダーでこの水準は良好。Phase 10でツールが増加しても ToolScope で制御可能

3. **Dynamic関数の呼び出し回数**: テスト中にDynamic関数が複数回呼ばれる（PromptBuilder内のbuild + EstimateReservedTokens）。現在のツール数では問題ないが、高コストなDynamic関数の場合はキャッシュを検討する余地がある
