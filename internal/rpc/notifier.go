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

// ContextStatus はコンテキスト使用率を送信する (event 情報なしの旧シグネチャ)。
func (n *Notifier) ContextStatus(ratio float64, count, limit int) error {
	return n.ContextStatusWithEvent(ratio, count, limit, "", "", 0)
}

// ContextStatusWithEvent は event/lastRole/compactionDelta を含めて送信する (C1/C2/C3/C5)。
//
//	event:           "user_added" / "assistant_added" / "tool_added" / "compacted" / ""
//	lastRole:        最後に追加されたメッセージの role
//	compactionDelta: event=="compacted" のとき、削減されたトークン数
func (n *Notifier) ContextStatusWithEvent(
	ratio float64, count, limit int,
	event, lastRole string, compactionDelta int,
) error {
	return n.server.Notify(protocol.MethodContextStatus, protocol.ContextStatusParams{
		UsageRatio:      ratio,
		TokenCount:      count,
		TokenLimit:      limit,
		LastEvent:       event,
		LastMessageRole: lastRole,
		CompactionDelta: compactionDelta,
	})
}
