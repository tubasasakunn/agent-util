---
paths:
  - "internal/rpc/**/*.go"
---

# internal/rpc/ ルール

## 責務

JSON-RPC over stdin/stdout のサーバー実装（ADR-001）:
- stdin からのリクエスト読み取り
- stdout への レスポンス書き込み
- メソッドのディスパッチ
- ツール実行要求（コア→ラッパーへの逆方向呼び出し）
- ストリーミング通知

## プロトコル

JSON-RPC 2.0 準拠。1行1メッセージ（改行区切り）。

```json
// リクエスト
{"jsonrpc":"2.0","method":"agent.run","params":{"prompt":"..."},"id":1}

// レスポンス
{"jsonrpc":"2.0","result":{"content":"..."},"id":1}

// 通知（id なし）
{"jsonrpc":"2.0","method":"stream.delta","params":{"text":"..."}}

// エラー
{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":1}
```

## stdin/stdout の分離

- stdin: JSON-RPCリクエストの読み取り専用
- stdout: JSON-RPCレスポンス/通知の書き込み専用
- stderr: ログ出力専用（JSON-RPCメッセージを混ぜない）

この分離は厳密に守る。ログや debug print が stdout に混入すると、
ラッパー側のJSONパースが壊れる。

## 並行処理

- リクエストの読み取りは単一goroutine（stdin は並列読み取り不可）
- リクエストのハンドリングは goroutine で並列実行可能にする
- stdout への書き込みは `sync.Mutex` で排他制御する
- ツール実行要求（コア→ラッパー）は、リクエストIDで対応するレスポンスを紐付ける

## ルール

- JSON のエンコード/デコードには `encoding/json` を使う（サードパーティ禁止）
- メッセージサイズに上限を設ける（DoS防止）
- 不正なJSONメッセージにはJSON-RPCエラーを返す（パニックしない）
- サーバーのライフサイクルは context.Context で管理する
- Graceful shutdown: context キャンセル時に実行中のリクエストを完了してから終了する
