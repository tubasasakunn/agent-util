package rpc

import (
	"sync"

	"ai-agent/pkg/protocol"
)

// PendingRequests はコア → ラッパーのリクエストに対するレスポンス待ちを管理する。
type PendingRequests struct {
	mu      sync.Mutex
	pending map[int]chan *protocol.Response
}

// NewPendingRequests は PendingRequests を生成する。
func NewPendingRequests() *PendingRequests {
	return &PendingRequests{
		pending: make(map[int]chan *protocol.Response),
	}
}

// Register は新しいペンディングリクエストを登録し、レスポンス受信用の channel を返す。
func (p *PendingRequests) Register(id int) <-chan *protocol.Response {
	ch := make(chan *protocol.Response, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()
	return ch
}

// Resolve は ID に対応するペンディングリクエストにレスポンスを送信する。
// ID が nil または未登録の場合は何もしない。
func (p *PendingRequests) Resolve(id *int, resp *protocol.Response) {
	if id == nil {
		return
	}
	p.mu.Lock()
	ch, ok := p.pending[*id]
	if ok {
		delete(p.pending, *id)
	}
	p.mu.Unlock()

	if ok {
		ch <- resp
	}
}

// CancelAll は全てのペンディングリクエストをキャンセルする（shutdown 時）。
func (p *PendingRequests) CancelAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, ch := range p.pending {
		close(ch)
		delete(p.pending, id)
	}
}

// Len は現在のペンディング数を返す。
func (p *PendingRequests) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pending)
}
