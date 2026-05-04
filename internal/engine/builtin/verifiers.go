package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"ai-agent/internal/engine"
)

// NonEmptyVerifier はツール結果が空でないことを検証する。
type NonEmptyVerifier struct{}

// Name は識別名を返す。
func (v *NonEmptyVerifier) Name() string { return "non_empty" }

// Verify は結果が空文字や空白のみでないかを判定する。
func (v *NonEmptyVerifier) Verify(ctx context.Context, toolName string, args json.RawMessage, result string) (*engine.VerifyResult, error) {
	if strings.TrimSpace(result) == "" {
		return &engine.VerifyResult{
			Passed:  false,
			Summary: "tool result is empty",
		}, nil
	}
	return &engine.VerifyResult{Passed: true, Summary: "result is non-empty"}, nil
}

// JSONValidVerifier はツール結果が JSON っぽい場合に有効性を検証する。
// JSON 形に見えない（{ や [ で始まらない）結果はスキップする。
type JSONValidVerifier struct{}

// Name は識別名を返す。
func (v *JSONValidVerifier) Name() string { return "json_valid" }

// Verify は結果の JSON 構文を検証する。
func (v *JSONValidVerifier) Verify(ctx context.Context, toolName string, args json.RawMessage, result string) (*engine.VerifyResult, error) {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return &engine.VerifyResult{Passed: true, Summary: "empty, skipped"}, nil
	}
	first := trimmed[0]
	if first != '{' && first != '[' {
		return &engine.VerifyResult{Passed: true, Summary: "not JSON-shaped, skipped"}, nil
	}
	var any_ any
	if err := json.Unmarshal([]byte(trimmed), &any_); err != nil {
		return &engine.VerifyResult{
			Passed:  false,
			Summary: "result is not valid JSON: " + err.Error(),
		}, nil
	}
	return &engine.VerifyResult{Passed: true, Summary: "valid JSON"}, nil
}
