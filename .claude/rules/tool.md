---
paths:
  - "pkg/tool/**/*.go"
---

# pkg/tool/ ルール

## 責務

Tool interface とスキーマ定義。
ラッパー側がツールを実装する際の契約を定義する。

## Tool interface

統一契約の5項目を満たすinterfaceを定義する。

```go
type Tool interface {
    // 識別
    Name() string
    Description() string
    Parameters() json.RawMessage // JSON Schema

    // 振る舞い宣言
    IsReadOnly() bool
    IsConcurrencySafe() bool

    // 実行
    Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

type Result struct {
    Content  string         `json:"content"`
    IsError  bool           `json:"is_error,omitempty"`
    Metadata map[string]any `json:"metadata,omitempty"`
}
```

## 設計原則

- **fail-closed**: `IsConcurrencySafe()` のデフォルトは false（安全側）
- **最小インターフェース**: ツール実装者が最低限満たすべきメソッドだけを含む
- **JSON Schemaによるパラメータ定義**: 引数のバリデーションはハーネス側で行う

## ツール定義のJSON表現

ルーターのシステムプロンプトに埋め込む用途と、JSON-RPCでの登録用途の両方に使う。

```go
type Definition struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"` // JSON Schema
    ReadOnly    bool            `json:"read_only,omitempty"`
}
```

## ルール

- Tool interface はこのパッケージで定義し、実装は別パッケージに置く
- internal/ に依存しない
- Definition は Tool interface から自動生成できるヘルパーを提供する
- パラメータの JSON Schema は `type`, `required`, `properties` を必ず含む
