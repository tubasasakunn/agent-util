package rpc

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"ai-agent/pkg/protocol"
)

func TestNotifier_StreamDelta(t *testing.T) {
	var buf bytes.Buffer
	srv := New(strings.NewReader(""), &buf)
	n := NewNotifier(srv)

	if err := n.StreamDelta("hello", 1); err != nil {
		t.Fatalf("StreamDelta: %v", err)
	}

	var req protocol.Request
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Method != protocol.MethodStreamDelta {
		t.Errorf("Method = %q, want %q", req.Method, protocol.MethodStreamDelta)
	}
	if req.ID != nil {
		t.Errorf("ID = %v, want nil", *req.ID)
	}

	var params protocol.StreamDeltaParams
	json.Unmarshal(req.Params, &params)
	if params.Text != "hello" || params.Turn != 1 {
		t.Errorf("params = %+v, want text=hello turn=1", params)
	}
}

func TestNotifier_StreamEnd(t *testing.T) {
	var buf bytes.Buffer
	srv := New(strings.NewReader(""), &buf)
	n := NewNotifier(srv)

	if err := n.StreamEnd("completed", 3); err != nil {
		t.Fatalf("StreamEnd: %v", err)
	}

	var req protocol.Request
	json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &req)
	if req.Method != protocol.MethodStreamEnd {
		t.Errorf("Method = %q, want %q", req.Method, protocol.MethodStreamEnd)
	}

	var params protocol.StreamEndParams
	json.Unmarshal(req.Params, &params)
	if params.Reason != "completed" || params.Turns != 3 {
		t.Errorf("params = %+v, want reason=completed turns=3", params)
	}
}

func TestNotifier_ContextStatus(t *testing.T) {
	var buf bytes.Buffer
	srv := New(strings.NewReader(""), &buf)
	n := NewNotifier(srv)

	if err := n.ContextStatus(0.75, 6000, 8192); err != nil {
		t.Fatalf("ContextStatus: %v", err)
	}

	var req protocol.Request
	json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &req)
	if req.Method != protocol.MethodContextStatus {
		t.Errorf("Method = %q, want %q", req.Method, protocol.MethodContextStatus)
	}

	var params protocol.ContextStatusParams
	json.Unmarshal(req.Params, &params)
	if params.UsageRatio != 0.75 {
		t.Errorf("UsageRatio = %f, want 0.75", params.UsageRatio)
	}
	if params.TokenCount != 6000 {
		t.Errorf("TokenCount = %d, want 6000", params.TokenCount)
	}
	if params.TokenLimit != 8192 {
		t.Errorf("TokenLimit = %d, want 8192", params.TokenLimit)
	}
}
