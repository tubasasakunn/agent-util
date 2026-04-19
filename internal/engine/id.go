package engine

import (
	"crypto/rand"
	"fmt"
)

// generateCallID はツールコール用のユニークIDを生成する。
// OpenAI互換形式: "call_" + ランダム hex 文字列。
func generateCallID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return fmt.Sprintf("call_%x", b)
}
