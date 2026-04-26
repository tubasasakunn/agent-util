package builtin

import (
	"fmt"
	"sort"

	"ai-agent/internal/engine"
)

// DefaultMaxInputLength は max_length ガードのデフォルト上限文字数。
const DefaultMaxInputLength = 50000

// InputGuard は名前から InputGuard を生成する。未知の名前は error。
func InputGuard(name string) (engine.InputGuard, error) {
	switch name {
	case "prompt_injection":
		return &PromptInjectionGuard{}, nil
	case "max_length":
		return &MaxLengthGuard{Max: DefaultMaxInputLength}, nil
	default:
		return nil, fmt.Errorf("unknown input guard: %q (available: %v)", name, InputGuardNames())
	}
}

// ToolCallGuard は名前から ToolCallGuard を生成する。
func ToolCallGuard(name string) (engine.ToolCallGuard, error) {
	switch name {
	case "dangerous_shell":
		return &DangerousShellGuard{}, nil
	default:
		return nil, fmt.Errorf("unknown tool_call guard: %q (available: %v)", name, ToolCallGuardNames())
	}
}

// OutputGuard は名前から OutputGuard を生成する。
func OutputGuard(name string) (engine.OutputGuard, error) {
	switch name {
	case "secret_leak":
		return &SecretLeakGuard{}, nil
	default:
		return nil, fmt.Errorf("unknown output guard: %q (available: %v)", name, OutputGuardNames())
	}
}

// Verifier は名前から Verifier を生成する。
func Verifier(name string) (engine.Verifier, error) {
	switch name {
	case "non_empty":
		return &NonEmptyVerifier{}, nil
	case "json_valid":
		return &JSONValidVerifier{}, nil
	default:
		return nil, fmt.Errorf("unknown verifier: %q (available: %v)", name, VerifierNames())
	}
}

// InputGuardNames は利用可能な InputGuard 名を返す（ソート済み）。
func InputGuardNames() []string {
	names := []string{"prompt_injection", "max_length"}
	sort.Strings(names)
	return names
}

// ToolCallGuardNames は利用可能な ToolCallGuard 名を返す。
func ToolCallGuardNames() []string {
	return []string{"dangerous_shell"}
}

// OutputGuardNames は利用可能な OutputGuard 名を返す。
func OutputGuardNames() []string {
	return []string{"secret_leak"}
}

// VerifierNames は利用可能な Verifier 名を返す（ソート済み）。
func VerifierNames() []string {
	names := []string{"non_empty", "json_valid"}
	sort.Strings(names)
	return names
}
