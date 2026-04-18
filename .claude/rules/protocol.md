---
paths:
  - "pkg/protocol/**/*.go"
---

# pkg/protocol/ ルール

## 責務

JSON-RPCメッセージ型とイベント型の定義。
GoコアとラッパーのIF契約を定義する。外部パッケージからimportされる。

## 型定義の原則

- 全ての型にJSONタグを付ける
- ポインタ型は「値が存在しない」ことを表現する場合のみ使う
- `json.RawMessage` で柔軟なフィールドに対応する（引数のスキーマ等）
- 型名はドメイン用語と一致させる: `ChatRequest`, `ToolCall`, `StreamEvent`

```go
type Request struct {
    JSONRPC string          `json:"jsonrpc"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
    ID      *int            `json:"id,omitempty"` // nil = 通知
}

type Response struct {
    JSONRPC string          `json:"jsonrpc"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *RPCError       `json:"error,omitempty"`
    ID      *int            `json:"id"`
}
```

## メソッド命名

`namespace.action` 形式で統一する:

```
agent.run          — エージェント実行
agent.abort        — 実行中断
tool.register      — ツール登録（ラッパー→コア）
tool.execute       — ツール実行要求（コア→ラッパー）
stream.delta       — ストリーミング差分（通知）
stream.end         — ストリーム完了（通知）
context.status     — コンテキスト使用率（通知）
```

## ルール

- このパッケージに実装ロジックを置かない（型定義とバリデーションのみ）
- internal/ に依存しない（逆方向は可）
- 型のフィールド追加は後方互換を保つ（omitempty で対応）
- バージョニング: 破壊的変更時はメソッド名にバージョンを含める（`agent.run.v2`）
