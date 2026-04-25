package rpc

import "ai-agent/pkg/protocol"

// Notifier はストリーミング通知を JSON-RPC で送信するヘルパー。
type Notifier struct {
	server *Server
}

// NewNotifier は Notifier を生成する。
func NewNotifier(server *Server) *Notifier {
	return &Notifier{server: server}
}

// StreamDelta はテキスト差分を送信する。
func (n *Notifier) StreamDelta(text string, turn int) error {
	return n.server.Notify(protocol.MethodStreamDelta, protocol.StreamDeltaParams{
		Text: text,
		Turn: turn,
	})
}

// StreamEnd はストリーム完了を送信する。
func (n *Notifier) StreamEnd(reason string, turns int) error {
	return n.server.Notify(protocol.MethodStreamEnd, protocol.StreamEndParams{
		Reason: reason,
		Turns:  turns,
	})
}

// ContextStatus はコンテキスト使用率を送信する。
func (n *Notifier) ContextStatus(ratio float64, count, limit int) error {
	return n.server.Notify(protocol.MethodContextStatus, protocol.ContextStatusParams{
		UsageRatio: ratio,
		TokenCount: count,
		TokenLimit: limit,
	})
}
