---
paths:
  - "internal/llm/**/*.go"
---

# internal/llm/ ルール

## 責務

OpenAI互換APIとの通信を担当する:
- HTTPリクエストの構築と送信
- レスポンスのパースとデコード
- SLM出力のJSON補正（パース補正ミドルウェア）
- リトライとバックオフ
- ストリーミングレスポンスの処理

## クライアント設計

Functional Optionsで構成する。

```go
type Client struct {
    endpoint   string
    model      string
    apiKey     string
    httpClient *http.Client
    maxRetries int
}

func NewClient(opts ...Option) *Client
func (c *Client) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
func (c *Client) ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error)
```

## リクエスト/レスポンス型

- OpenAI互換の型を定義するが、SLLMに不要なフィールドは省略してよい
- JSON タグは `omitempty` を適切に使い、不要なフィールドを送信しない
- `json.RawMessage` を活用して柔軟なスキーマに対応する

## パース補正ミドルウェア

SLMの出力は不完全なJSONを返すことがある（investigation/001で確認済み）。
以下の補正を適用する:

1. 末尾の不完全な閉じ括弧を補完
2. `{null}` → `null` の修正
3. シングルクォート → ダブルクォートの変換
4. 末尾カンマの除去
5. JSONとして無効な制御文字の除去

補正はレスポンスパースの直前に1回だけ適用する。補正後のJSONをログに残す（デバッグ用）。

## リトライ

- 指数バックオフ: 初回1秒, 最大32秒, 25%のジッタを加える
- `Retry-After` ヘッダがある場合はそちらを優先
- リトライ可能なステータスコード: 429, 500, 502, 503, 529
- 400, 401, 403, 404 はリトライしない（設定ミスなので即失敗）
- 最大リトライ回数はFunctional Optionで設定可能（デフォルト3回）
- リトライ時は context.Context のキャンセルを毎回チェックする

```go
func (c *Client) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
    for attempt := 0; attempt <= c.maxRetries; attempt++ {
        // context チェック
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }

        resp, err := c.httpClient.Do(req)
        if err == nil && !isRetryable(resp.StatusCode) {
            return resp, nil
        }
        wait := backoff(attempt)
        // ...
    }
}
```

## ルール

- HTTP通信は `*http.Client` を使い、`http.DefaultClient` は使わない（タイムアウト制御のため）
- レスポンスボディは必ず `defer resp.Body.Close()` で閉じる
- APIキーは構造体フィールドに保持し、ログに出力しない
- ストリーミングは `<-chan StreamEvent` で返し、channelのクローズで完了を通知する
