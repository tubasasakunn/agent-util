package protocol

import "encoding/json"

// Version は JSON-RPC のバージョン。
const Version = "2.0"

// Request は JSON-RPC 2.0 のリクエスト。
// ID が nil の場合は通知（レスポンス不要）。
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      *int            `json:"id,omitempty"`
}

// Response は JSON-RPC 2.0 のレスポンス。
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      *int            `json:"id"`
}

// RPCError は JSON-RPC 2.0 のエラーオブジェクト。
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// IsNotification はリクエストが通知（ID なし）かどうかを判定する。
func (r *Request) IsNotification() bool {
	return r.ID == nil
}

// NewResponse は成功レスポンスを生成する。
func NewResponse(id *int, result any) (*Response, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &Response{
		JSONRPC: Version,
		Result:  data,
		ID:      id,
	}, nil
}

// NewErrorResponse はエラーレスポンスを生成する。
func NewErrorResponse(id *int, code int, message string) *Response {
	return &Response{
		JSONRPC: Version,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
		ID: id,
	}
}

// NewNotification は通知リクエスト（ID なし）を生成する。
func NewNotification(method string, params any) (*Request, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return &Request{
		JSONRPC: Version,
		Method:  method,
		Params:  data,
	}, nil
}

// IntPtr は int のポインタを返すヘルパー。
func IntPtr(n int) *int { return &n }

// BoolPtr は bool のポインタを返すヘルパー。
// omitempty フィールドに true/false を明示的にセットするときに使う。
func BoolPtr(b bool) *bool { return &b }

// Float64Ptr は float64 のポインタを返すヘルパー。
func Float64Ptr(f float64) *float64 { return &f }

// StringPtr は string のポインタを返すヘルパー。
func StringPtr(s string) *string { return &s }
