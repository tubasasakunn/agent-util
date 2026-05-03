package engine

import "context"

// Verifier はツール実行後の検証を実行するインターフェース。
// ルールベース検証（テスト、リンター）と LLM-as-judge の両方を
// このインターフェースで抽象化する。
type Verifier interface {
	// Name は検証器の名前を返す（ログ出力用）。
	Name() string
	// Verify はツール実行結果を検証する。
	// toolName: 実行されたツール名
	// args: ツールに渡された引数
	// result: ツール実行結果の文字列
	Verify(ctx context.Context, toolName string, args []byte, result string) (*VerifyResult, error)
}

// VerifyResult は検証の結果。
type VerifyResult struct {
	Passed  bool     // 全検証が通過したか
	Summary string   // 結果の要約（LLMへのフィードバック用）
	Details []string // 個別の検証項目の結果
}

// VerifierRegistry は検証器を管理し、順に実行する。
type VerifierRegistry struct {
	verifiers []Verifier
}

// NewVerifierRegistry は空の VerifierRegistry を返す。
func NewVerifierRegistry(vs ...Verifier) *VerifierRegistry {
	return &VerifierRegistry{verifiers: vs}
}

// Add は検証器を追加する。
func (vr *VerifierRegistry) Add(v Verifier) {
	vr.verifiers = append(vr.verifiers, v)
}

// Len は登録されている検証器の数を返す。
func (vr *VerifierRegistry) Len() int {
	return len(vr.verifiers)
}

// All は登録されている全検証器を返す。Fork() での継承用。
func (vr *VerifierRegistry) All() []Verifier {
	return vr.verifiers
}

// RunAll は全検証器を順に実行する。
// 1つでも失敗した場合、失敗した検証結果を集約して返す。
// 検証器自体がエラーを返した場合はログして続行する（検証器の障害で本体を止めない）。
// logf は検証器エラーのログ出力用コールバック。
func (vr *VerifierRegistry) RunAll(ctx context.Context, toolName string, args []byte, result string, logf func(string, ...any)) *VerifyResult {
	if len(vr.verifiers) == 0 {
		return &VerifyResult{Passed: true, Summary: "no verifiers"}
	}

	allPassed := true
	var details []string
	var failSummaries []string

	for _, v := range vr.verifiers {
		select {
		case <-ctx.Done():
			return &VerifyResult{
				Passed:  false,
				Summary: "verification canceled",
				Details: details,
			}
		default:
		}

		res, err := v.Verify(ctx, toolName, args, result)
		if err != nil {
			logf("[verify] %s error (skipped): %s", v.Name(), err)
			details = append(details, v.Name()+": error (skipped)")
			continue
		}

		if res.Passed {
			details = append(details, v.Name()+": passed")
		} else {
			allPassed = false
			details = append(details, v.Name()+": FAILED - "+res.Summary)
			failSummaries = append(failSummaries, v.Name()+": "+res.Summary)
		}
	}

	summary := "all verifiers passed"
	if !allPassed {
		summary = ""
		for i, s := range failSummaries {
			if i > 0 {
				summary += "; "
			}
			summary += s
		}
	}

	return &VerifyResult{
		Passed:  allPassed,
		Summary: summary,
		Details: details,
	}
}
