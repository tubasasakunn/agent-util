package llm

import (
	"net/http"
	"time"
)

// Option は Client の設定を変更する関数。
type Option func(*config)

type config struct {
	endpoint    string
	model       string
	apiKey      string
	httpClient  *http.Client
	maxRetries  int
	httpTimeout time.Duration
}

func defaultConfig() config {
	return config{
		endpoint:    "http://localhost:8000/v1/chat/completions",
		model:       "gemma-4-E2B-it-Q4_K_M",
		maxRetries:  3,
		httpTimeout: 60 * time.Second,
	}
}

// WithEndpoint はAPIエンドポイントURLを設定する。
func WithEndpoint(url string) Option {
	return func(c *config) { c.endpoint = url }
}

// WithModel はモデル名を設定する。
func WithModel(model string) Option {
	return func(c *config) { c.model = model }
}

// WithAPIKey はAPIキーを設定する。
func WithAPIKey(key string) Option {
	return func(c *config) { c.apiKey = key }
}

// WithHTTPClient はカスタムHTTPクライアントを設定する。
func WithHTTPClient(c *http.Client) Option {
	return func(cfg *config) { cfg.httpClient = c }
}

// WithMaxRetries は最大リトライ回数を設定する。
func WithMaxRetries(n int) Option {
	return func(c *config) { c.maxRetries = n }
}

// WithHTTPTimeout はHTTPリクエストのタイムアウトを設定する。
func WithHTTPTimeout(d time.Duration) Option {
	return func(c *config) { c.httpTimeout = d }
}
