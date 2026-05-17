package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"ai-agent/pkg/tool"
)

// Registry は登録されたツールを管理する。
type Registry struct {
	tools map[string]tool.Tool
	order []string // 登録順を保持（プロンプト生成の再現性のため）
}

// NewRegistry は空のレジストリを生成する。
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]tool.Tool),
	}
}

// Register はツールを登録する。同名のツールが既に存在する場合はエラーを返す。
func (r *Registry) Register(t tool.Tool) error {
	name := t.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("register tool: duplicate name %q", name)
	}
	r.tools[name] = t
	r.order = append(r.order, name)
	return nil
}

// Get は名前でツールを取得する。
func (r *Registry) Get(name string) (tool.Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Len は登録されたツール数を返す。
func (r *Registry) Len() int {
	return len(r.tools)
}

// Tools は登録された全ツールを登録順で返す。
func (r *Registry) Tools() []tool.Tool {
	tools := make([]tool.Tool, 0, len(r.order))
	for _, name := range r.order {
		tools = append(tools, r.tools[name])
	}
	return tools
}

// Definitions は全ツールの Definition を登録順で返す。
func (r *Registry) Definitions() []tool.Definition {
	defs := make([]tool.Definition, 0, len(r.order))
	for _, name := range r.order {
		defs = append(defs, tool.DefinitionOf(r.tools[name]))
	}
	return defs
}

// ToolScope はツールスコーピングの設定。
type ToolScope struct {
	// MaxTools はルーターに提示するツールの最大数。0 は無制限。
	MaxTools int
	// IncludeAlways は常に含めるツール名のセット。MaxTools 制限内で優先される。
	IncludeAlways map[string]bool
	// ToolBudget は同一ツールの呼び出し回数上限 (A1/A4)。
	// 例: {"shell": 1} なら shell は 1 回呼んだら以後ルーターに提示されない。
	// nil または 0 のキーは「上限なし」。
	ToolBudget map[string]int
}

// FormatForPrompt はルーターのシステムプロンプトに埋め込むツール一覧テキストを生成する。
func (r *Registry) FormatForPrompt() string {
	return r.formatDefs(r.Definitions())
}

// ScopedFormatForPrompt はスコープに基づいてフィルタしたツール一覧テキストを生成する。
func (r *Registry) ScopedFormatForPrompt(scope ToolScope) string {
	return r.ScopedFormatForPromptWithCalls(scope, nil)
}

// ScopedFormatForPromptWithCalls は ScopedFormatForPrompt に加えて、現在までの
// ツール呼び出し回数 (currentCalls) を考慮して toolBudget 超過のツールを除外する (A1/A4)。
// currentCalls が nil または scope.ToolBudget が nil なら従来通り。
func (r *Registry) ScopedFormatForPromptWithCalls(
	scope ToolScope,
	currentCalls map[string]int,
) string {
	defs := r.Definitions()

	// A1/A4: toolBudget 超過のツールを除外
	if len(scope.ToolBudget) > 0 && currentCalls != nil {
		filtered := defs[:0]
		for _, d := range defs {
			if limit, ok := scope.ToolBudget[d.Name]; ok && limit > 0 {
				if currentCalls[d.Name] >= limit {
					continue // 予算オーバー: ルーターには見せない
				}
			}
			filtered = append(filtered, d)
		}
		defs = filtered
	}

	if scope.MaxTools <= 0 || scope.MaxTools >= len(defs) {
		return r.formatDefs(defs)
	}

	// IncludeAlways を先に確保
	var always, rest []tool.Definition
	for _, d := range defs {
		if scope.IncludeAlways[d.Name] {
			always = append(always, d)
		} else {
			rest = append(rest, d)
		}
	}

	// 残りの枠を登録順で埋める
	remaining := scope.MaxTools - len(always)
	if remaining < 0 {
		remaining = 0
	}
	if remaining > len(rest) {
		remaining = len(rest)
	}

	result := make([]tool.Definition, 0, scope.MaxTools)
	result = append(result, always...)
	result = append(result, rest[:remaining]...)
	return r.formatDefs(result)
}

// formatDefs はツール定義リストをプロンプト用テキストに変換する。
func (r *Registry) formatDefs(defs []tool.Definition) string {
	if len(defs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Available Tools\n\n")
	for _, def := range defs {
		sb.WriteString(fmt.Sprintf("### %s\n", def.Name))
		sb.WriteString(fmt.Sprintf("%s\n", def.Description))
		sb.WriteString("Parameters:\n```json\n")
		indented, err := json.MarshalIndent(json.RawMessage(def.Parameters), "", "  ")
		if err != nil {
			sb.Write(def.Parameters)
		} else {
			sb.Write(indented)
		}
		sb.WriteString("\n```\n\n")
	}
	return sb.String()
}
