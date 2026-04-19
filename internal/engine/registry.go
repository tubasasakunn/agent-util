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

// Definitions は全ツールの Definition を登録順で返す。
func (r *Registry) Definitions() []tool.Definition {
	defs := make([]tool.Definition, 0, len(r.order))
	for _, name := range r.order {
		defs = append(defs, tool.DefinitionOf(r.tools[name]))
	}
	return defs
}

// FormatForPrompt はルーターのシステムプロンプトに埋め込むツール一覧テキストを生成する。
func (r *Registry) FormatForPrompt() string {
	if len(r.order) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Available Tools\n\n")
	for _, name := range r.order {
		def := tool.DefinitionOf(r.tools[name])
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
