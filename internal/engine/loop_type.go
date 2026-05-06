package engine

import "context"

// LoopType はエージェントの実行ループパターンを指定する。
type LoopType int

const (
	// LoopTypeReAct は Reason-Act ループ（デフォルト）。
	// ルーターがツールを選択し、ツール実行後に次のターンへ進む。
	// goal_judge・verifier はオプション。LLM が "none" を選択した時点で
	// chatStep を実行し、GoalJudge が未設定なら "completed" で終了する。
	LoopTypeReAct LoopType = iota

	// LoopTypeREAF は Reason-Execute-Assess-Finalize ループ。
	// 各ツール実行後に GoalJudge が呼ばれ、タスク完了を判定する。
	// goal_judge が未設定の場合は LLM 応答のヒューリスティック判定にフォールバックする。
	// verifier（Assess）は両モード共通だが、REAF では必須として扱う。
	LoopTypeREAF
)

// LoopTypeFromString は文字列から LoopType に変換する。
// 未知の文字列は (LoopTypeReAct, false) を返す。
func LoopTypeFromString(s string) (LoopType, bool) {
	switch s {
	case "react", "":
		return LoopTypeReAct, true
	case "reaf":
		return LoopTypeREAF, true
	default:
		return LoopTypeReAct, false
	}
}

// String は LoopType を文字列に変換する。
func (t LoopType) String() string {
	switch t {
	case LoopTypeREAF:
		return "reaf"
	default:
		return "react"
	}
}

// GoalJudge はエージェントのゴール達成を判定するインターフェース。
// ShouldTerminate が true を返すと現在のターンを Terminal にして Run を終了する。
type GoalJudge interface {
	// ShouldTerminate はエージェントの最新応答 response と現在のターン番号 turn を受け取り、
	// タスクが完了したか (terminate=true) を判定する。
	// reason は AgentRunResult.Reason に使われる終了理由文字列。
	// エラー時は terminate=false として扱い、ループを継続する（fail-open）。
	ShouldTerminate(ctx context.Context, response string, turn int) (terminate bool, reason string, err error)
}

// GoalJudgeFunc は関数を GoalJudge インターフェースに適合させるアダプター。
type GoalJudgeFunc func(ctx context.Context, response string, turn int) (bool, string, error)

// ShouldTerminate は f を呼び出す。
func (f GoalJudgeFunc) ShouldTerminate(ctx context.Context, response string, turn int) (bool, string, error) {
	return f(ctx, response, turn)
}
