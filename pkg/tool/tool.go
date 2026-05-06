// Package tool defines the Tool interface and helpers for the ai-agent harness.
//
// Implement [Tool] to create a custom tool and register it with the engine:
//
//	type MyTool struct{}
//
//	func (t *MyTool) Name()        string          { return "my_tool" }
//	func (t *MyTool) Description() string          { return "Does something useful" }
//	func (t *MyTool) Parameters()  json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
//	func (t *MyTool) IsReadOnly()  bool            { return true }
//	func (t *MyTool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
//	    return tool.OK("done"), nil
//	}
package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// workDirKey はコンテキストからワーキングディレクトリを取得するためのキー。
type workDirKey struct{}

// ContextWithWorkDir はワーキングディレクトリを設定したコンテキストを返す。
// Engine が tool 実行前にコンテキストへ注入し、ツール側が WorkDirFromContext で取得する。
func ContextWithWorkDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, workDirKey{}, dir)
}

// WorkDirFromContext はコンテキストからワーキングディレクトリを取得する。
// 設定されていない場合は空文字を返す（ツールはデフォルト動作を使う）。
func WorkDirFromContext(ctx context.Context) string {
	dir, _ := ctx.Value(workDirKey{}).(string)
	return dir
}

// Tool はツールの統一契約。
// ツール実装者はこのインターフェースを満たすことで、エージェントハーネスに登録できる。
type Tool interface {
	// 識別
	Name() string
	Description() string
	Parameters() json.RawMessage // JSON Schema (type:"object")

	// 振る舞い宣言
	IsReadOnly() bool // true なら副作用なし（パーミッション判定に使用）

	// 実行
	Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

// Result はツール実行の結果。
// 成功時は Content に結果文字列を、失敗時は IsError=true + Content にエラー内容を設定する。
type Result struct {
	Content  string         `json:"content"`
	IsError  bool           `json:"is_error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// OK は成功結果を返すヘルパー。
func OK(content string) Result {
	return Result{Content: content}
}

// Errorf はフォーマット付きエラー結果を返すヘルパー。
// Execute の実装内で return tool.Errorf("failed: %v", err) のように使う。
func Errorf(format string, args ...any) Result {
	return Result{Content: fmt.Sprintf(format, args...), IsError: true}
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
