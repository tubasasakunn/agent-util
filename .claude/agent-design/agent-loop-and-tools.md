# エージェントループとツール設計

## コアループ: Generatorパターン

Claude Codeの最重要設計ポイント。`queryLoop()`は非同期ジェネレータ。

```
while(true) {
  response = callModel(state)

  if response.stopReason == "end_turn" {
    return completed
  }

  // ツール呼び出しがあれば実行して続ける
  toolResults = executeTools(response.toolUses)
  state = appendMessages(state, toolResults)
  // → 次のイテレーションへ
}
```

**Generatorの利点:**
- ストリーミングがyieldでそのまま流れる
- 中断（AbortController）がきれいに入る
- maxTurnsチェックがイテレーション境界で自然に入る
- ツール実行ループに複数の離脱ポイント(continue sites)を設けやすい

## 状態遷移: Continue/Terminal

ループの「続けるか止めるか」を型で管理する。

**Continue（続ける理由）:**
- `tool_use` — 通常のツール呼び出し
- `max_output_tokens_escalate` — トークン上限超え→上位モデルへ
- `reactive_compact_retry` — コンテキスト圧縮後リトライ
- `parse_error_retry` — パース失敗後リトライ（SLM向けに重要）

**Terminal（止める理由）:**
- `completed` — 正常完了
- `aborted` — ユーザー中断
- `prompt_too_long` — コンテキスト超過
- `max_turns_reached` — ターン上限
- `model_error` — 回復不能エラー

**設計意図:** 「なぜ続けるか」を型で管理することで、同じ回復戦略が二重に適用されるバグを防ぐ。

## ツール契約: Goでの実装イメージ

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() json.RawMessage  // JSON Schema

    // 振る舞い宣言
    IsReadOnly() bool
    IsConcurrencySafe() bool
    RequiresPermission() bool

    // 実行
    Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

type ToolResult struct {
    Content  string
    IsError  bool
    Metadata map[string]any
}
```

**統一契約の5項目:**
1. 入力スキーマ（JSON Schema）
2. 出力形式（ToolResult）
3. 副作用の有無（IsReadOnly）
4. 並列実行可能性（IsConcurrencySafe）
5. 必要な権限（RequiresPermission）

## 権限モデル: deny→ask→allow

```
ツール実行要求
  ↓
1. Denyリスト照合 → マッチしたら即拒否
2. Allowリスト照合 → マッチしたら即許可
3. ツール固有チェック → ツールが自前で判定
4. ユーザー承認 → 判断できない場合は人間に委ねる
```

グロブ対応: `Bash(git push*)`, `Edit(src/**)` のようなパターンで柔軟に設定。
**デフォルトはfail-closed（拒否）。** 明示的に許可されない限り実行しない。

## エラー処理: 指数バックオフとカスケード

```
API呼び出しエラー
  → 指数バックオフ（最大32秒、25%ジッタ）
  → Retry-Afterヘッダ優先

特殊ケース:
  - 429（レート制限）: バックオフして再試行
  - 529（過負荷）: foregroundのみリトライ、backgroundは即終了
  - 3連続失敗: 下位モデルへフォールバック
  - 413（コンテキスト超過）: コンテキスト縮約してリトライ
```

## サブエージェント設計

```
親エージェント
  ├── Fork（並列実行用）
  │   └── 親のコンテキストを複製して独立実行
  ├── Worktree（fs隔離）
  │   └── git worktreeで物理的にファイルシステムを分離
  └── Coordinator（マルチエージェント管理）
      └── メールボックスシステムでタスク分配と結果統合
```

**重要:** 親を中断してもサブエージェントは自動で死なない。明示的な終了制御が必要。

## transcript永続化

API呼び出しの**前に**JSONLでトランスクリプトを保存する。
クラッシュしても会話履歴が失われない。ゼロコストでクラッシュ耐性を獲得。

## Verification Agent

実装者がLLMである場合、「読んで正しそうに見える」ことは検証ではない。

```
実装完了
  ↓
Verification Agent（独立LLM）が:
  - ビルド実行
  - テスト実行
  - 差分検証
  ↓
VERDICT: PASS / FAIL / PARTIAL
```

**思想: LLM実装者を信用しない。外部の真実（テスト結果）で検証する。**
