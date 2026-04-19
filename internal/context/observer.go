package context

// EventKind はコンテキストイベントの種類。
type EventKind int

const (
	// ThresholdExceeded はコンテキスト使用率が閾値を超えたことを示す。
	ThresholdExceeded EventKind = iota
	// ThresholdRecovered はコンテキスト使用率が閾値以下に回復したことを示す。
	ThresholdRecovered
)

// Event はコンテキスト状態の変化を表すイベント。
type Event struct {
	Kind       EventKind
	UsageRatio float64
	TokenCount int
	TokenLimit int
}

// Observer はコンテキストイベントを受け取るコールバック関数。
type Observer func(Event)
