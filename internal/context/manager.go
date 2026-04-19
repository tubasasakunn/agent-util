package context

import (
	"sync"

	"ai-agent/internal/llm"
)

// entry はメッセージと推定トークン数のペア。
type entry struct {
	Message llm.Message
	Tokens  int
}

// Manager はメッセージ履歴を管理し、コンテキスト使用量を監視する。
// スレッドセーフ。
type Manager struct {
	mu             sync.Mutex
	entries        []entry
	tokenLimit     int
	tokenCount     int // メッセージ部分のトークン数
	reservedTokens int // システムプロンプト + ツール定義のトークン数
	threshold      float64
	observers      []Observer
	exceeded       bool // 閾値を超過した状態かどうか（重複発火防止）
}

// NewManager は指定されたトークン上限で Manager を生成する。
func NewManager(tokenLimit int, opts ...Option) *Manager {
	cfg := defaultManagerConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Manager{
		tokenLimit: tokenLimit,
		threshold:  cfg.threshold,
	}
}

// Add はメッセージを履歴に追加する。
// トークン数を推定してキャッシュし、閾値チェックを行う。
func (m *Manager) Add(msg llm.Message) {
	tokens := EstimateTokens(msg)
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries = append(m.entries, entry{Message: msg, Tokens: tokens})
	m.tokenCount += tokens
	m.checkThreshold()
}

// Messages は全メッセージを返す。LLMリクエスト構築用。
func (m *Manager) Messages() []llm.Message {
	m.mu.Lock()
	defer m.mu.Unlock()

	msgs := make([]llm.Message, len(m.entries))
	for i, e := range m.entries {
		msgs[i] = e.Message
	}
	return msgs
}

// Len はメッセージ数を返す。
func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// TokenCount は現在のトークン使用量（reservedTokens + メッセージトークン）を返す。
func (m *Manager) TokenCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reservedTokens + m.tokenCount
}

// TokenLimit はトークン上限を返す。
func (m *Manager) TokenLimit() int {
	return m.tokenLimit
}

// UsageRatio は現在のコンテキスト使用率を返す（0.0〜1.0+）。
func (m *Manager) UsageRatio() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tokenLimit == 0 {
		return 0
	}
	return float64(m.reservedTokens+m.tokenCount) / float64(m.tokenLimit)
}

// SetReserved はシステムプロンプトやツール定義による予約トークン数を設定する。
// メッセージ履歴とは別枠で管理され、UsageRatio の計算に含まれる。
func (m *Manager) SetReserved(tokens int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reservedTokens = tokens
	m.checkThreshold()
}

// OnThreshold は閾値イベントの Observer を登録する。
func (m *Manager) OnThreshold(obs Observer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observers = append(m.observers, obs)
}

// checkThreshold は使用率を確認し、閾値を超過/回復した場合にイベントを発火する。
// mu.Lock() を取得した状態で呼び出すこと。
func (m *Manager) checkThreshold() {
	if m.tokenLimit == 0 || len(m.observers) == 0 {
		return
	}

	ratio := float64(m.reservedTokens+m.tokenCount) / float64(m.tokenLimit)
	nowExceeded := ratio >= m.threshold

	if nowExceeded && !m.exceeded {
		m.exceeded = true
		evt := Event{
			Kind:       ThresholdExceeded,
			UsageRatio: ratio,
			TokenCount: m.reservedTokens + m.tokenCount,
			TokenLimit: m.tokenLimit,
		}
		for _, obs := range m.observers {
			obs(evt)
		}
	} else if !nowExceeded && m.exceeded {
		m.exceeded = false
		evt := Event{
			Kind:       ThresholdRecovered,
			UsageRatio: ratio,
			TokenCount: m.reservedTokens + m.tokenCount,
			TokenLimit: m.tokenLimit,
		}
		for _, obs := range m.observers {
			obs(evt)
		}
	}
}
