---
paths:
  - "**/*.go"
---

# Go 全体ルール

## コーディング規約

### エラーハンドリング

- エラーは必ず `fmt.Errorf("操作名: %w", err)` でラップして返す。呼び出し元がerrors.Isで判定できるようにする
- エラーを握りつぶさない。ログに書くか返すかのどちらかを必ず行う
- センチネルエラーは `var ErrXxx = errors.New("xxx")` でパッケージレベルに定義する
- 復帰可能なエラーと致命的なエラーを型で区別する

```go
// 良い例
var ErrContextOverflow = errors.New("context overflow")

func (m *Manager) Add(msg Message) error {
    if m.tokenCount+msg.Tokens > m.limit {
        return fmt.Errorf("add message: %w", ErrContextOverflow)
    }
    // ...
}
```

### 命名

- パッケージ名は短く単数形: `engine`, `llm`, `context`, `rpc`（`utils`, `common`, `helpers` 禁止）
- インターフェースは動詞/役割: `Runner`, `Parser`, `Router`（`IRunner` のようなI接頭辞は使わない）
- 構造体は名詞: `Client`, `Message`, `ToolResult`
- コンストラクタは `New` + 型名: `NewClient`, `NewRouter`
- 非公開関数は動詞で始める: `parseResponse`, `buildRequest`
- テストは `Test関数名_条件` 形式: `TestRouter_EmptyTools`

### Functional Optionsパターン

公開APIのコンストラクタにはFunctional Optionsを使い、拡張性を確保する。
引数の追加が破壊的変更にならないようにする。

```go
type Option func(*config)

func WithEndpoint(url string) Option {
    return func(c *config) { c.endpoint = url }
}

func WithMaxRetries(n int) Option {
    return func(c *config) { c.maxRetries = n }
}

func NewClient(opts ...Option) *Client {
    cfg := defaultConfig()
    for _, opt := range opts {
        opt(&cfg)
    }
    // ...
}
```

### インターフェース

- インターフェースは使う側のパッケージに定義する（定義する側ではない）
- メソッドは最小限にする（1〜3メソッド推奨）
- `io.Reader`, `io.Writer` のような標準ライブラリのインターフェースを最大限活用する

### 構造体の初期化

- ゼロ値が有用になるよう設計する
- フィールドのデフォルト値はコンストラクタ内で設定する（グローバル変数にしない）
- 必須フィールドはコンストラクタの引数に、オプショナルはFunctional Optionsに

## 並行処理

Goの強みを最大限に活かす。

### goroutine

- goroutineの起動には必ずcontext.Contextを渡す
- goroutineのライフサイクルを明確にする: 誰が起動し、誰が停止するか
- `sync.WaitGroup` または `errgroup.Group` でgoroutineの完了を待つ
- goroutineリークを防ぐため、キャンセル可能なcontextを使う

```go
g, ctx := errgroup.WithContext(ctx)
g.Go(func() error {
    return doWork(ctx)
})
if err := g.Wait(); err != nil {
    return fmt.Errorf("parallel work: %w", err)
}
```

### channel

- channelの方向を型で制限する: `<-chan T`（受信専用）, `chan<- T`（送信専用）
- バッファサイズには根拠を持つ（「とりあえず100」は禁止）
- channelのクローズは送信側の責務
- select文でのdefaultケースは意図的な場合のみ使う

### context.Context

- 第一引数に必ず `ctx context.Context` を置く
- context.Background() は main() と テストの最上位のみで使用
- キャンセルされたcontextのエラーは `context.Canceled` と `context.DeadlineExceeded` で判定する
- context.WithValue は型安全なキーを使い、最小限に

## プロジェクト構造

```
cmd/       — バイナリエントリポイント。ここにビジネスロジックを置かない
internal/  — 外部からimport不可。実装の本体
pkg/       — 外部に公開するインターフェース・型定義
```

- `internal/` のパッケージ間で循環依存を作らない
- 依存の方向は `cmd → internal → pkg` （逆方向禁止）
- `pkg/` のパッケージは `internal/` に依存しない

## テスト

- テストファイルは対象と同じパッケージに置く（`_test.go` サフィックス）
- テーブル駆動テストを基本とする
- モックは interface を使い、テストファイル内に定義する（外部モックライブラリは使わない）
- `testdata/` ディレクトリにテスト用フィクスチャを配置する

```go
func TestParseResponse_VariousInputs(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    *Response
        wantErr bool
    }{
        {name: "valid json", input: `{"content":"hello"}`, want: &Response{Content: "hello"}},
        {name: "broken json", input: `{"content":`, wantErr: true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ParseResponse(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
            }
            // ...
        })
    }
}
```

## 依存管理

- 標準ライブラリを最優先で使う
- 外部ライブラリの追加は慎重に判断する（依存が少ないほど良い）
- `go.mod` の indirect 依存が増えすぎていないか定期的に確認する
