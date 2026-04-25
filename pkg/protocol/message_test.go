package protocol

import (
	"encoding/json"
	"testing"
)

func TestRequest_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		req  Request
	}{
		{
			name: "request with id",
			req: Request{
				JSONRPC: Version,
				Method:  MethodAgentRun,
				Params:  json.RawMessage(`{"prompt":"hello"}`),
				ID:      IntPtr(1),
			},
		},
		{
			name: "notification without id",
			req: Request{
				JSONRPC: Version,
				Method:  MethodStreamDelta,
				Params:  json.RawMessage(`{"text":"hi"}`),
			},
		},
		{
			name: "request without params",
			req: Request{
				JSONRPC: Version,
				Method:  MethodAgentAbort,
				ID:      IntPtr(42),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var got Request
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if got.JSONRPC != tt.req.JSONRPC {
				t.Errorf("JSONRPC = %q, want %q", got.JSONRPC, tt.req.JSONRPC)
			}
			if got.Method != tt.req.Method {
				t.Errorf("Method = %q, want %q", got.Method, tt.req.Method)
			}
			if string(got.Params) != string(tt.req.Params) {
				t.Errorf("Params = %s, want %s", got.Params, tt.req.Params)
			}

			if tt.req.ID == nil {
				if got.ID != nil {
					t.Errorf("ID = %v, want nil", *got.ID)
				}
			} else {
				if got.ID == nil {
					t.Error("ID = nil, want non-nil")
				} else if *got.ID != *tt.req.ID {
					t.Errorf("ID = %d, want %d", *got.ID, *tt.req.ID)
				}
			}
		})
	}
}

func TestResponse_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		resp Response
	}{
		{
			name: "success response",
			resp: Response{
				JSONRPC: Version,
				Result:  json.RawMessage(`{"response":"ok"}`),
				ID:      IntPtr(1),
			},
		},
		{
			name: "error response",
			resp: Response{
				JSONRPC: Version,
				Error: &RPCError{
					Code:    ErrCodeMethodNotFound,
					Message: "method not found",
				},
				ID: IntPtr(2),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.resp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var got Response
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if got.JSONRPC != tt.resp.JSONRPC {
				t.Errorf("JSONRPC = %q, want %q", got.JSONRPC, tt.resp.JSONRPC)
			}
			if string(got.Result) != string(tt.resp.Result) {
				t.Errorf("Result = %s, want %s", got.Result, tt.resp.Result)
			}
			if tt.resp.Error != nil {
				if got.Error == nil {
					t.Fatal("Error = nil, want non-nil")
				}
				if got.Error.Code != tt.resp.Error.Code {
					t.Errorf("Error.Code = %d, want %d", got.Error.Code, tt.resp.Error.Code)
				}
				if got.Error.Message != tt.resp.Error.Message {
					t.Errorf("Error.Message = %q, want %q", got.Error.Message, tt.resp.Error.Message)
				}
			}
		})
	}
}

func TestIsNotification(t *testing.T) {
	t.Run("with id", func(t *testing.T) {
		req := Request{ID: IntPtr(1)}
		if req.IsNotification() {
			t.Error("IsNotification() = true, want false")
		}
	})

	t.Run("without id", func(t *testing.T) {
		req := Request{}
		if !req.IsNotification() {
			t.Error("IsNotification() = false, want true")
		}
	})
}

func TestNewResponse(t *testing.T) {
	type result struct {
		Value string `json:"value"`
	}

	resp, err := NewResponse(IntPtr(5), result{Value: "ok"})
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	if resp.JSONRPC != Version {
		t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, Version)
	}
	if *resp.ID != 5 {
		t.Errorf("ID = %d, want 5", *resp.ID)
	}
	if resp.Error != nil {
		t.Errorf("Error = %v, want nil", resp.Error)
	}

	var got result
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got.Value != "ok" {
		t.Errorf("Value = %q, want %q", got.Value, "ok")
	}
}

func TestNewErrorResponse(t *testing.T) {
	resp := NewErrorResponse(IntPtr(3), ErrCodeInvalidParams, "bad params")
	if resp.JSONRPC != Version {
		t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, Version)
	}
	if *resp.ID != 3 {
		t.Errorf("ID = %d, want 3", *resp.ID)
	}
	if resp.Error == nil {
		t.Fatal("Error = nil, want non-nil")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Errorf("Code = %d, want %d", resp.Error.Code, ErrCodeInvalidParams)
	}
	if resp.Error.Message != "bad params" {
		t.Errorf("Message = %q, want %q", resp.Error.Message, "bad params")
	}
}

func TestNewNotification(t *testing.T) {
	params := StreamDeltaParams{Text: "hello", Turn: 1}
	req, err := NewNotification(MethodStreamDelta, params)
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	if req.JSONRPC != Version {
		t.Errorf("JSONRPC = %q, want %q", req.JSONRPC, Version)
	}
	if req.Method != MethodStreamDelta {
		t.Errorf("Method = %q, want %q", req.Method, MethodStreamDelta)
	}
	if req.ID != nil {
		t.Errorf("ID = %v, want nil", *req.ID)
	}
	if !req.IsNotification() {
		t.Error("IsNotification() = false, want true")
	}
}

func TestMethodParams_RoundTrip(t *testing.T) {
	t.Run("AgentRunParams", func(t *testing.T) {
		p := AgentRunParams{Prompt: "hello", MaxTurns: 5}
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got AgentRunParams
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Prompt != p.Prompt || got.MaxTurns != p.MaxTurns {
			t.Errorf("got %+v, want %+v", got, p)
		}
	})

	t.Run("ToolExecuteParams", func(t *testing.T) {
		p := ToolExecuteParams{Name: "read_file", Args: json.RawMessage(`{"path":"a.txt"}`)}
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got ToolExecuteParams
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Name != p.Name || string(got.Args) != string(p.Args) {
			t.Errorf("got %+v, want %+v", got, p)
		}
	})

	t.Run("ToolRegisterParams", func(t *testing.T) {
		p := ToolRegisterParams{
			Tools: []ToolDefinition{
				{Name: "greet", Description: "Greets", Parameters: json.RawMessage(`{}`), ReadOnly: true},
			},
		}
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got ToolRegisterParams
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(got.Tools) != 1 {
			t.Fatalf("Tools len = %d, want 1", len(got.Tools))
		}
		if got.Tools[0].Name != "greet" || !got.Tools[0].ReadOnly {
			t.Errorf("got %+v, want name=greet readonly=true", got.Tools[0])
		}
	})
}
