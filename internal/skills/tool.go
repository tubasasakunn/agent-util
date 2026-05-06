package skills

import (
	"context"
	"encoding/json"
	"fmt"

	"ai-agent/pkg/tool"
)

// skillTool は Skill を tool.Tool として公開する。
// AI目線ではスキル・MCPツール・通常ツールの区別はない。すべて同一のツールとして扱われる。
type skillTool struct {
	skill Skill
}

// AsTool は Skill を tool.Tool に変換する。
// これにより AI はスキルを他のツールと同等に呼び出せる。
func AsTool(s Skill) tool.Tool {
	return &skillTool{skill: s}
}

func (t *skillTool) Name() string            { return t.skill.Name }
func (t *skillTool) Description() string     { return t.skill.Description }
func (t *skillTool) IsReadOnly() bool { return true }

// Parameters はスキルツールが引数を取らないことを示す。
// スキルはツール名そのもので識別され、追加パラメータは不要。
func (t *skillTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *skillTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	content, err := t.skill.Activate()
	if err != nil {
		return tool.Errorf("failed to activate skill: %v", err), nil
	}
	// スキル内容取得後はルーターが "none" へ遷移するよう明示する。
	body := fmt.Sprintf("<skill_content name=%q>\n%s\n\nInstructions loaded. Follow these to respond. Select tool=\"none\" next.\n</skill_content>",
		t.skill.Name, content)
	return tool.OK(body), nil
}

// CatalogAsTools は Catalog のすべてのスキルを []tool.Tool に変換する。
func CatalogAsTools(c *Catalog) []tool.Tool {
	tools := make([]tool.Tool, len(c.skills))
	for i, s := range c.skills {
		tools[i] = AsTool(s)
	}
	return tools
}
