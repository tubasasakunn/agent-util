package context

// Option は Manager の設定を変更する関数。
type Option func(*managerConfig)

type managerConfig struct {
	threshold float64
}

func defaultManagerConfig() managerConfig {
	return managerConfig{
		threshold: 0.8,
	}
}

// WithThreshold は閾値イベントを発火する使用率を設定する。
// デフォルトは 0.8（80%）。
func WithThreshold(ratio float64) Option {
	return func(c *managerConfig) { c.threshold = ratio }
}
