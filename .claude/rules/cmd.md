---
paths:
  - "cmd/**/*.go"
---

# cmd/ ルール

## 責務

cmd/ はバイナリのエントリポイントのみ。以下だけを行う:
- フラグ/環境変数のパース
- 設定の組み立て（Functional Options）
- internal/ の呼び出し
- シグナルハンドリング（graceful shutdown）

ビジネスロジック、HTTP通信、データ変換をここに書かない。

## 構成

```go
func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()

    // フラグ/環境変数 → 設定
    cfg := parseFlags()

    // 依存の組み立て
    client := llm.NewClient(
        llm.WithEndpoint(cfg.endpoint),
        llm.WithModel(cfg.model),
    )
    eng := engine.New(client, engine.WithMaxTurns(cfg.maxTurns))

    // 実行
    if err := eng.Run(ctx); err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
}
```

## ルール

- `os.Exit` は main() 内のみ。他パッケージからは error を返す
- ログ出力は stderr に統一する（stdout はJSON-RPCや結果出力に使うため）
- 環境変数のキーは `SLLM_` プレフィックスで統一: `SLLM_ENDPOINT`, `SLLM_MODEL`, `SLLM_API_KEY`
