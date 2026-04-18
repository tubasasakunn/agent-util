---
paths:
  - "internal/engine/**/*.go"
---

# internal/engine/ ルール

## 責務

エージェントループの中核。以下を管理する:
- メインループ（while true: モデル呼び出し → 判断 → ツール実行 → 繰り返し）
- ルーターステップ（JSON modeでツール選択、ADR-002）
- Continue/Terminal の状態遷移
- ターン数制限、安全停止

## 状態遷移

ループの「続けるか止めるか」を discriminated union（型付きenum）で管理する。
同じ回復戦略が二重に適用されるバグを防ぐため、理由を型に埋め込む。

```go
type LoopResult struct {
    Kind   ResultKind
    Reason string
}

type ResultKind int
const (
    Continue ResultKind = iota  // ループ継続
    Terminal                     // ループ終了
)
```

Continue の理由: `tool_use`, `parse_error_retry`, `compact_retry`
Terminal の理由: `completed`, `aborted`, `max_turns`, `context_overflow`, `model_error`

## エージェントループの構造

```go
func (e *Engine) Run(ctx context.Context, input string) (*Result, error) {
    e.messages = append(e.messages, UserMessage(input))

    for turn := 0; turn < e.maxTurns; turn++ {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }

        resp, err := e.step(ctx)
        if err != nil {
            return nil, fmt.Errorf("turn %d: %w", turn, err)
        }

        switch resp.Kind {
        case Terminal:
            return resp.toResult(), nil
        case Continue:
            continue
        }
    }
    return nil, ErrMaxTurnsReached
}
```

## ルーターの設計（ADR-002）

- ルーターはJSON modeで動作し、toolsパラメータは使わない
- ツール一覧はシステムプロンプトにテキストとして埋め込む
- ルーターの出力スキーマ: `{"tool": string, "arguments": object, "reasoning": string}`
- ルーターが `"tool": "none"` を返したらツール実行をスキップ
- 将来SLLMの複数ツール対応が改善した場合に直接呼び出しに切り替え可能な設計にする

## ルール

- context.Context を全メソッドの第一引数に渡し、キャンセルに即座に反応する
- ループ内で panic しない。エラーは全て error として返す
- 各ターンの開始時にコンテキスト使用量をチェックし、閾値超過時は縮約を試みる
- ルーターとサブエージェントは同一の llm.Client を共有するが、呼び出しパラメータは独立
