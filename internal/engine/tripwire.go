package engine

import "fmt"

// TripwireError はトリップワイヤ発動を示すセンチネルエラー。
// ガードレールが危険な入力/呼び出し/出力を検知した場合に返され、
// エージェントループを即時停止させる。
type TripwireError struct {
	Source string // どのガードレールが発動したか（"input", "tool_call", "output"）
	Reason string // 人間向けの説明
}

func (e *TripwireError) Error() string {
	return fmt.Sprintf("tripwire [%s]: %s", e.Source, e.Reason)
}
