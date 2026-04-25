package engine

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAuditLogger_Log(t *testing.T) {
	tests := []struct {
		name     string
		entry    AuditEntry
		wantSub  string // 出力に含まれるべき文字列
	}{
		{
			name: "allowed decision",
			entry: AuditEntry{
				Timestamp:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				ToolName:   "read_file",
				Args:       json.RawMessage(`{"path":"test.go"}`),
				Decision:   "allowed",
				Reason:     "read_only auto-approve",
				IsReadOnly: true,
			},
			wantSub: `[audit] tool=read_file decision=allowed readonly=true reason="read_only auto-approve"`,
		},
		{
			name: "denied decision",
			entry: AuditEntry{
				Timestamp:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				ToolName:   "shell",
				Args:       json.RawMessage(`{"command":"rm -rf /"}`),
				Decision:   "denied",
				Reason:     "blocked by policy",
				IsReadOnly: false,
			},
			wantSub: `[audit] tool=shell decision=denied readonly=false reason="blocked by policy"`,
		},
		{
			name: "user_approved decision",
			entry: AuditEntry{
				ToolName:   "write_file",
				Decision:   "user_approved",
				Reason:     "user confirmed",
				IsReadOnly: false,
			},
			wantSub: `[audit] tool=write_file decision=user_approved readonly=false reason="user confirmed"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewAuditLogger(&buf)
			logger.Log(tt.entry)

			got := buf.String()
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("output = %q, want substring %q", got, tt.wantSub)
			}
			if !strings.HasSuffix(got, "\n") {
				t.Errorf("output should end with newline, got %q", got)
			}
		})
	}
}

func TestAuditLogger_NilWriter(t *testing.T) {
	logger := NewAuditLogger(nil)
	// nil writer でパニックしないことを確認
	logger.Log(AuditEntry{
		ToolName: "test",
		Decision: "allowed",
	})
}

func TestAuditLogger_NilLogger(t *testing.T) {
	var logger *AuditLogger
	// nil logger でパニックしないことを確認
	logger.Log(AuditEntry{
		ToolName: "test",
		Decision: "allowed",
	})
}
