package rpc

import (
	"testing"

	"ai-agent/pkg/protocol"
)

func TestPendingRequests_RegisterAndResolve(t *testing.T) {
	p := NewPendingRequests()

	ch := p.Register(1)
	if p.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", p.Len())
	}

	resp := &protocol.Response{
		JSONRPC: protocol.Version,
		Result:  []byte(`{"ok":true}`),
		ID:      protocol.IntPtr(1),
	}
	p.Resolve(protocol.IntPtr(1), resp)

	got := <-ch
	if got == nil {
		t.Fatal("got nil, want response")
	}
	if *got.ID != 1 {
		t.Errorf("ID = %d, want 1", *got.ID)
	}

	if p.Len() != 0 {
		t.Errorf("Len() = %d, want 0 after resolve", p.Len())
	}
}

func TestPendingRequests_ResolveNilID(t *testing.T) {
	p := NewPendingRequests()
	p.Register(1)

	// nil ID での Resolve は何もしない
	p.Resolve(nil, &protocol.Response{})

	if p.Len() != 1 {
		t.Errorf("Len() = %d, want 1 (unchanged)", p.Len())
	}
}

func TestPendingRequests_ResolveUnknownID(t *testing.T) {
	p := NewPendingRequests()
	p.Register(1)

	// 存在しない ID での Resolve は何もしない（パニックしない）
	p.Resolve(protocol.IntPtr(999), &protocol.Response{})

	if p.Len() != 1 {
		t.Errorf("Len() = %d, want 1 (unchanged)", p.Len())
	}
}

func TestPendingRequests_CancelAll(t *testing.T) {
	p := NewPendingRequests()
	ch1 := p.Register(1)
	ch2 := p.Register(2)

	p.CancelAll()

	if p.Len() != 0 {
		t.Errorf("Len() = %d, want 0 after CancelAll", p.Len())
	}

	// close された channel からの受信は ok=false
	_, ok1 := <-ch1
	_, ok2 := <-ch2
	if ok1 {
		t.Error("ch1: ok = true, want false (closed)")
	}
	if ok2 {
		t.Error("ch2: ok = true, want false (closed)")
	}
}
