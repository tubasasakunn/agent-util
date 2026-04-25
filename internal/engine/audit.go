package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// AuditEntry は監査ログの1エントリ。
// パーミッションチェックとガードレールの判定結果を記録する。
type AuditEntry struct {
	Timestamp  time.Time
	ToolName   string
	Args       json.RawMessage
	Decision   string // "allowed", "denied", "user_approved", "user_rejected", "tripwire"
	Reason     string
	IsReadOnly bool
}

// AuditLogger は監査エントリを記録する。
// io.Writer に [audit] プレフィックスで構造化テキストを出力する。
type AuditLogger struct {
	w io.Writer
}

// NewAuditLogger は AuditLogger を生成する。
// w が nil の場合はログを出力しない。
func NewAuditLogger(w io.Writer) *AuditLogger {
	return &AuditLogger{w: w}
}

// Log は監査エントリを記録する。
func (al *AuditLogger) Log(entry AuditEntry) {
	if al == nil || al.w == nil {
		return
	}
	fmt.Fprintf(al.w, "[audit] tool=%s decision=%s readonly=%v reason=%q\n",
		entry.ToolName, entry.Decision, entry.IsReadOnly, entry.Reason)
}
