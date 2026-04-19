package tool

import (
	"context"
	"encoding/json"
)

// Tool はツールの統一契約。
// ツール実装者はこのインターフェースを満たすことで、エージェントハーネスに登録できる。
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

// Result はツール実行の結果。
type Result struct {
	Content  string         `json:"content"`
	IsError  bool           `json:"is_error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Definition はツールのJSON表現。
// ルーターのシステムプロンプトへの埋め込みとJSON-RPC登録に使用する。
type Definition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	ReadOnly    bool            `json:"read_only,omitempty"`
}

// DefinitionOf は Tool から Definition を生成する。
func DefinitionOf(t Tool) Definition {
	return Definition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.Parameters(),
		ReadOnly:    t.IsReadOnly(),
	}
}
