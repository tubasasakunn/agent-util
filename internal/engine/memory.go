package engine

import (
	"fmt"
	"strings"
)

// MemoryEntry はメモリインデックスの1エントリ。
// コンテキストには軽量なポインタ情報のみ載せ、実体は read_file で取得する。
type MemoryEntry struct {
	Key     string // 一意キー（"adr-001", "project-structure" 等）
	Summary string // 1行要約
	Path    string // 実体ファイルへのパス
}

// MemoryIndex は軽量ポインタのコレクション。
// プロンプトに常時載せるインデックスとして機能する。
type MemoryIndex struct {
	entries []MemoryEntry
}

// NewMemoryIndex は MemoryEntry のスライスから MemoryIndex を生成する。
func NewMemoryIndex(entries []MemoryEntry) *MemoryIndex {
	return &MemoryIndex{entries: entries}
}

// FormatForPrompt はインデックスをプロンプト用テキストに変換する。
// エントリが空の場合は空文字を返す。
func (mi *MemoryIndex) FormatForPrompt() string {
	if len(mi.entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Knowledge Index\n\n")
	for _, e := range mi.entries {
		sb.WriteString(fmt.Sprintf("- [%s] %s — %s\n", e.Key, e.Summary, e.Path))
	}
	sb.WriteString("\nTo read details, use read_file with the path.\n")
	return sb.String()
}

// Len はエントリ数を返す。
func (mi *MemoryIndex) Len() int {
	return len(mi.entries)
}
