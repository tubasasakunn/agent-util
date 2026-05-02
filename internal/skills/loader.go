// Package skills は Agent Skills の発見・管理を担う。
// Skill はファイル・インライン・リモート取得など実装手段を問わない抽象として定義する。
// ファイルシステムは手段の1つに過ぎない。
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill は Agent Skill のメタデータとコンテンツプロバイダ。
// Activate の実装はファイル読み込み・インライン定義・HTTP取得など何でもよい。
type Skill struct {
	Name        string
	Description string
	// Activate はスキルの全指示テキストを返す。呼び出しごとに最新内容を取得できる。
	Activate func() (string, error)
}

// New は任意の Activate 関数を持つ Skill を生成する。
func New(name, description string, activate func() (string, error)) Skill {
	return Skill{Name: name, Description: description, Activate: activate}
}

// NewInline はコンテンツをクロージャとして保持するインライン Skill を生成する。
// テスト・プログラム定義スキルに適する。
func NewInline(name, description, content string) Skill {
	return New(name, description, func() (string, error) { return content, nil })
}

// NewFileSkill は SKILL.md ファイルから本文を読み込む Skill を生成する。
// location は SKILL.md への絶対パス、dir はスキルのベースディレクトリ。
func NewFileSkill(name, description, location, dir string) Skill {
	return New(name, description, func() (string, error) {
		data, err := os.ReadFile(location)
		if err != nil {
			return "", fmt.Errorf("skills: read %s: %w", location, err)
		}
		_, _, body, err := parseFrontmatter(string(data))
		if err != nil {
			return "", fmt.Errorf("skills: parse %s: %w", location, err)
		}
		if dir != "" {
			body = fmt.Sprintf("%s\n\nSkill directory: %s", body, dir)
		}
		return body, nil
	})
}

// Catalog は発見済みスキルのコレクション。
type Catalog struct {
	skills []Skill
	byName map[string]Skill
}

// NewCatalog は Skill スライスから Catalog を生成する。
func NewCatalog(skills []Skill) *Catalog {
	byName := make(map[string]Skill, len(skills))
	for _, s := range skills {
		byName[s.Name] = s
	}
	return &Catalog{skills: skills, byName: byName}
}

// All は全スキルを返す。
func (c *Catalog) All() []Skill { return c.skills }

// Get は名前でスキルを引く。
func (c *Catalog) Get(name string) (Skill, bool) {
	s, ok := c.byName[name]
	return s, ok
}

// Len はスキル数を返す。
func (c *Catalog) Len() int { return len(c.skills) }

// Names は有効なスキル名の一覧を返す。
func (c *Catalog) Names() []string {
	names := make([]string, len(c.skills))
	for i, s := range c.skills {
		names[i] = s.Name
	}
	return names
}

// Loader はディレクトリを走査して SKILL.md ベースの Skill を発見する。
// ファイルシステム以外のスキルは NewCatalog に直接 Skill を渡すか、
// engine.WithSkills に加えて WithCustomSkills 等で追加する。
type Loader struct {
	dirs []string
}

// NewLoader は指定ディレクトリを走査する Loader を生成する。
// dirs は優先度順（先頭が高優先）で指定する。
func NewLoader(dirs ...string) *Loader {
	return &Loader{dirs: dirs}
}

// DefaultDirs は workDir を基準にした標準スキルディレクトリ一覧を返す。
// プロジェクトレベルがユーザーレベルより優先される。
func DefaultDirs(workDir string) []string {
	home, _ := os.UserHomeDir()
	var dirs []string

	if workDir != "" {
		dirs = append(dirs,
			filepath.Join(workDir, ".agents", "skills"),
			filepath.Join(workDir, ".claude", "skills"),
		)
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".agents", "skills"),
			filepath.Join(home, ".claude", "skills"),
		)
	}
	return dirs
}

// Load は設定ディレクトリを走査してスキルカタログを返す。
// 同名スキルは先に発見さ��たもの（高優先ディレクトリ）が勝つ。
func (l *Loader) Load() (*Catalog, error) {
	seen := make(map[string]struct{})
	var found []Skill

	for _, dir := range l.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("skills: read dir %s: %w", dir, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == ".git" || entry.Name() == "node_modules" {
				continue
			}
			skillMD := filepath.Join(dir, entry.Name(), "SKILL.md")
			data, err := os.ReadFile(skillMD)
			if err != nil {
				continue
			}

			name, desc, _, err := parseFrontmatter(string(data))
			if err != nil || name == "" || desc == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}

			skillDir := filepath.Join(dir, entry.Name())
			found = append(found, NewFileSkill(name, desc, skillMD, skillDir))
		}
	}

	return NewCatalog(found), nil
}

// parseFrontmatter は SKILL.md のコンテンツから name, description, body を抽出する。
func parseFrontmatter(content string) (name, description, body string, err error) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", content, fmt.Errorf("no frontmatter")
	}

	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		return "", "", content, fmt.Errorf("frontmatter not closed")
	}

	for _, line := range lines[1:closeIdx] {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = stripQuotes(val)

		switch key {
		case "name":
			name = val
		case "description":
			description = val
		}
	}

	body = strings.TrimSpace(strings.Join(lines[closeIdx+1:], "\n"))
	return name, description, body, nil
}

func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
