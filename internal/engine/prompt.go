package engine

import (
	"sort"
	"strings"

	agentctx "ai-agent/internal/context"
)

// SectionScope はセクションが含まれるプロンプトの種類を決定する。
type SectionScope int

const (
	// ScopeAll は chat/router 両方のプロンプトに含まれる。
	ScopeAll SectionScope = iota
	// ScopeRouter は router プロンプトにのみ含まれる。
	ScopeRouter
	// ScopeManual はプロンプト構築に自動で含まれない。Resolve() で明示的に取得する。
	ScopeManual
)

// 優先度定数。値が小さいほど高優先（先頭配置）。
const (
	PrioritySystem    = 0
	PriorityTools     = 10
	PriorityDeveloper = 20
	PriorityUser      = 30
	PriorityReminder  = 90
)

// Section はプロンプトの構成要素。
type Section struct {
	Key      string        // 一意識別子
	Priority int           // 小さいほど高優先
	Scope    SectionScope  // 含��れるプロンプトの種類
	Content  string        // 静的コンテンツ（Dynamic が nil の場合に使用）
	Dynamic  func() string // 動的コンテンツ生成関数（非nil なら Content より優先）
	Required bool          // true = 常に保持
}

// resolve はセクションのコンテンツを返す。Dynamic が設定されていればそちらを優先する。
func (s Section) resolve() string {
	if s.Dynamic != nil {
		return s.Dynamic()
	}
	return s.Content
}

// PromptBuilder は優先度付きセクションからシス���ムプロンプトを組み立てる。
type PromptBuilder struct {
	sections     map[string]Section
	dirty        bool
	cachedTokens int
}

// NewPromptBuilder は空の PromptBuilder を生成する。
func NewPromptBuilder() *PromptBuilder {
	return &PromptBuilder{
		sections: make(map[string]Section),
		dirty:    true,
	}
}

// Add はセクションを登録する。同じ Key が既に存在する場合は上書きする。
func (pb *PromptBuilder) Add(s Section) {
	pb.sections[s.Key] = s
	pb.dirty = true
}

// Remove はセクションを削除する。
func (pb *PromptBuilder) Remove(key string) {
	if _, ok := pb.sections[key]; ok {
		delete(pb.sections, key)
		pb.dirty = true
	}
}

// Has は指定したキーのセクションが存在するかを返す。
func (pb *PromptBuilder) Has(key string) bool {
	_, ok := pb.sections[key]
	return ok
}

// Resolve は指定したキーのセクションのコンテンツを返す。
// セクションが存在しない場合は空文字と false を返す。
func (pb *PromptBuilder) Resolve(key string) (string, bool) {
	s, ok := pb.sections[key]
	if !ok {
		return "", false
	}
	return s.resolve(), true
}

// sortedSections は指定モードに含まれるセクションを優先度順に返す。
// ScopeAll: ScopeAll のセクションのみ。
// ScopeRouter: ScopeAll + ScopeRouter の全セクション。
func (pb *PromptBuilder) sortedSections(mode SectionScope) []Section {
	var filtered []Section
	for _, s := range pb.sections {
		if s.Scope == ScopeManual {
			continue // ScopeManual はプロンプト構築に含めない
		}
		switch mode {
		case ScopeAll:
			if s.Scope == ScopeAll {
				filtered = append(filtered, s)
			}
		case ScopeRouter:
			filtered = append(filtered, s)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Priority != filtered[j].Priority {
			return filtered[i].Priority < filtered[j].Priority
		}
		return filtered[i].Key < filtered[j].Key
	})
	return filtered
}

// build はセクションを優先度順に結合してプロンプトテキストを返す。
func (pb *PromptBuilder) build(mode SectionScope) string {
	sections := pb.sortedSections(mode)
	var parts []string
	for _, s := range sections {
		content := s.resolve()
		if content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n")
}

// BuildSystemPrompt は chat 用のシステムプロンプトを返す（ScopeAll セクションのみ）。
func (pb *PromptBuilder) BuildSystemPrompt() string {
	return pb.build(ScopeAll)
}

// BuildRouterSystemPrompt ��� router 用のシステムプ��ンプトを返す（全セクション）。
func (pb *PromptBuilder) BuildRouterSystemPrompt() string {
	return pb.build(ScopeRouter)
}

// EstimateReservedTokens は router プロンプト（最大ケース）の推定トーク��数を返す。
// dirty フラグが false の場合はキャッシュを返す。
func (pb *PromptBuilder) EstimateReservedTokens() int {
	if !pb.dirty {
		return pb.cachedTokens
	}
	tokens := agentctx.EstimateTextTokens(pb.BuildRouterSystemPrompt())
	// ScopeManual セクション（リマインダー等）もトークン予約に含める
	for _, s := range pb.sections {
		if s.Scope == ScopeManual {
			content := s.resolve()
			if content != "" {
				tokens += agentctx.EstimateTextTokens(content)
			}
		}
	}
	pb.cachedTokens = tokens
	pb.dirty = false
	return pb.cachedTokens
}

// IsDirty はセクションが変更されたかを返す。
func (pb *PromptBuilder) IsDirty() bool {
	return pb.dirty
}
