package protocol

// JSON-RPC 2.0 標準エラーコード。
const (
	ErrCodeParse          = -32700 // 不正な JSON
	ErrCodeInvalidRequest = -32600 // 無効なリクエスト
	ErrCodeMethodNotFound = -32601 // メソッド未定義
	ErrCodeInvalidParams  = -32602 // 無効なパラメータ
	ErrCodeInternal       = -32603 // 内部エラー
)

// アプリケーション固有エラーコード (-32000 ~ -32099)。
const (
	ErrCodeToolNotFound    = -32000 // tool.execute で未登録ツール
	ErrCodeToolExecFailed  = -32001 // ツール実行失敗
	ErrCodeAgentBusy       = -32002 // agent.run が既に実行中
	ErrCodeAborted         = -32003 // agent.abort により中断
	ErrCodeMessageTooLarge = -32004 // メッセージサイズ上限超過
)
