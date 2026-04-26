package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"ai-agent/pkg/protocol"
)

// MaxMessageSize はメッセージサイズの上限（DoS 防止）。
const MaxMessageSize = 10 * 1024 * 1024 // 10MB

// HandlerFunc は JSON-RPC メソッドのハンドラ関数。
type HandlerFunc func(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError)

// Server は JSON-RPC over stdio のサーバー。
type Server struct {
	reader io.Reader
	writer io.Writer
	logw   io.Writer

	mu       sync.Mutex // stdout 書き込みの排他制御
	handlers map[string]HandlerFunc
	pending  *PendingRequests

	nextID int        // コア → ラッパーのリクエスト ID 生成
	idMu   sync.Mutex // nextID の排他制御

	wg sync.WaitGroup // 実行中のハンドラ goroutine の完了待ち
}

// Option はサーバーのオプション。
type Option func(*Server)

// WithLogWriter はログ出力先を設定する。
func WithLogWriter(w io.Writer) Option {
	return func(s *Server) { s.logw = w }
}

// New はサーバーを生成する。
func New(r io.Reader, w io.Writer, opts ...Option) *Server {
	s := &Server{
		reader:   r,
		writer:   w,
		handlers: make(map[string]HandlerFunc),
		pending:  NewPendingRequests(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Handle はメソッドハンドラを登録する。
func (s *Server) Handle(method string, fn HandlerFunc) {
	s.handlers[method] = fn
}

// Serve はメインループを開始する。
// ctx のキャンセルで graceful shutdown する。
func (s *Server) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.reader)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxMessageSize)

	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		// コンテキストキャンセル時にペンディングリクエストをクリーンアップ
		s.pending.CancelAll()
		close(done)
	}()

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			s.wg.Wait()
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// dispatch はメッセージの種類を判別して処理する
		s.dispatch(ctx, line)
	}

	s.wg.Wait()
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return nil // EOF = 正常終了
}

// dispatch はメッセージを判別してリクエストまたはレスポンスとして処理する。
func (s *Server) dispatch(ctx context.Context, line []byte) {
	// method フィールドの有無で Request/Response を判別
	var probe struct {
		Method *string            `json:"method"`
		ID     *int               `json:"id"`
		Result json.RawMessage    `json:"result"`
		Error  *protocol.RPCError `json:"error"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		s.writeError(nil, protocol.ErrCodeParse, "Parse error")
		return
	}

	// method フィールドなし + (result or error) あり → レスポンス（コア → ラッパーの応答）
	if probe.Method == nil {
		var resp protocol.Response
		if err := json.Unmarshal(line, &resp); err != nil {
			s.logf("invalid response: %v", err)
			return
		}
		s.pending.Resolve(resp.ID, &resp)
		return
	}

	// method フィールドあり → リクエスト
	var req protocol.Request
	if err := json.Unmarshal(line, &req); err != nil {
		s.writeError(nil, protocol.ErrCodeParse, "Parse error")
		return
	}

	if req.JSONRPC != protocol.Version {
		s.writeError(req.ID, protocol.ErrCodeInvalidRequest, "Invalid JSONRPC version")
		return
	}

	handler, ok := s.handlers[req.Method]
	if !ok {
		if !req.IsNotification() {
			s.writeError(req.ID, protocol.ErrCodeMethodNotFound,
				fmt.Sprintf("Method not found: %s", req.Method))
		}
		return
	}

	// リクエストのハンドリングを goroutine で並列実行
	s.wg.Go(func() {
		result, rpcErr := handler(ctx, req.Params)
		if req.IsNotification() {
			return // 通知にはレスポンスを返さない
		}
		if rpcErr != nil {
			s.writeError(req.ID, rpcErr.Code, rpcErr.Message)
			return
		}
		s.writeResult(req.ID, result)
	})
}

// SendRequest はコア → ラッパーのリクエストを送信し、レスポンスを待つ。
func (s *Server) SendRequest(ctx context.Context, method string, params any) (*protocol.Response, error) {
	id := s.nextRequestID()

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	req := &protocol.Request{
		JSONRPC: protocol.Version,
		Method:  method,
		Params:  paramsJSON,
		ID:      protocol.IntPtr(id),
	}

	ch := s.pending.Register(id)
	if err := s.writeRequest(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("pending request cancelled")
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Notify は通知（ID なし）を送信する。
func (s *Server) Notify(method string, params any) error {
	req, err := protocol.NewNotification(method, params)
	if err != nil {
		return fmt.Errorf("build notification: %w", err)
	}
	return s.writeRequest(req)
}

// writeResult は成功レスポンスを書き込む。
func (s *Server) writeResult(id *int, result any) {
	resp, err := protocol.NewResponse(id, result)
	if err != nil {
		s.logf("marshal result: %v", err)
		s.writeError(id, protocol.ErrCodeInternal, "Internal error")
		return
	}
	if err := s.writeJSON(resp); err != nil {
		s.logf("write result: %v", err)
	}
}

// writeError はエラーレスポンスを書き込む。
func (s *Server) writeError(id *int, code int, message string) {
	resp := protocol.NewErrorResponse(id, code, message)
	if err := s.writeJSON(resp); err != nil {
		s.logf("write error: %v", err)
	}
}

// writeRequest はリクエスト/通知メッセージを書き込む。
func (s *Server) writeRequest(req *protocol.Request) error {
	return s.writeJSON(req)
}

// writeJSON は任意の値をJSON化して stdout に改行付きで書き込む。
// sync.Mutex で排他制御する。
func (s *Server) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.writer.Write(data)
	return err
}

// nextRequestID はコア → ラッパーのリクエスト ID を採番する。
func (s *Server) nextRequestID() int {
	s.idMu.Lock()
	defer s.idMu.Unlock()
	s.nextID++
	return s.nextID
}

// logf はログメッセージを出力する。logw が nil の場合は何もしない。
func (s *Server) logf(format string, args ...any) {
	if s.logw != nil {
		fmt.Fprintf(s.logw, "[rpc] "+format+"\n", args...)
	}
}
