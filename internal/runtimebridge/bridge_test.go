package runtimebridge_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/hecatehq/claude-code-acp-adapter/internal/acp"
	"github.com/hecatehq/claude-code-acp-adapter/internal/acptest"
	"github.com/hecatehq/claude-code-acp-adapter/internal/runtimebridge"
	"github.com/hecatehq/claude-code-acp-adapter/internal/runtimejsonrpc"
)

func TestNewSessionProxiesToRuntime(t *testing.T) {
	runtime := newFakeRuntimeClient()
	client := newBridgeClient(t, runtime)

	response := client.Request("session/new", map[string]any{
		"cwd": "/tmp/project",
	})

	var result struct {
		SessionID string `json:"sessionId"`
	}
	response.ResultInto(t, &result)
	if result.SessionID != "sess-bridge" {
		t.Fatalf("sessionId = %q, want sess-bridge", result.SessionID)
	}
	if runtime.called("session/new") != 1 {
		t.Fatalf("session/new calls = %d, want 1", runtime.called("session/new"))
	}
}

func TestPromptForwardsRuntimeUpdatesBeforeResponse(t *testing.T) {
	runtime := newFakeRuntimeClient()
	client := newBridgeClient(t, runtime)

	envelopes := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      "prompt-1",
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": "sess-bridge",
			"prompt":    []map[string]string{{"type": "text", "text": "hello"}},
		},
	})
	if len(envelopes) != 2 {
		t.Fatalf("got %d envelopes, want notification + response", len(envelopes))
	}
	if envelopes[0].Method != "session/update" {
		t.Fatalf("first envelope method = %q, want session/update", envelopes[0].Method)
	}
	var result struct {
		StopReason string `json:"stopReason"`
	}
	envelopes[1].ResultInto(t, &result)
	if result.StopReason != "end_turn" {
		t.Fatalf("stopReason = %q, want end_turn", result.StopReason)
	}
}

func TestLoadSessionForwardsReplayBeforeResponse(t *testing.T) {
	runtime := newFakeRuntimeClient()
	client := newBridgeClient(t, runtime)

	envelopes := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      "load-1",
		"method":  "session/load",
		"params": map[string]any{
			"sessionId": "sess-bridge",
			"cwd":       "/tmp/project",
		},
	})
	if len(envelopes) != 2 {
		t.Fatalf("got %d envelopes, want replay notification + response", len(envelopes))
	}
	if envelopes[0].Method != "session/update" {
		t.Fatalf("first envelope method = %q, want session/update", envelopes[0].Method)
	}
	if string(envelopes[1].Result) != "null" {
		t.Fatalf("load result = %s, want null", envelopes[1].Result)
	}
}

func TestResumeSessionProxiesRuntimeResult(t *testing.T) {
	runtime := newFakeRuntimeClient()
	client := newBridgeClient(t, runtime)

	response := client.Request("session/resume", map[string]any{
		"sessionId": "sess-bridge",
		"cwd":       "/tmp/project",
	})
	var result map[string]any
	response.ResultInto(t, &result)
	if result["mode"] != "default" {
		t.Fatalf("resume result = %#v, want mode default", result)
	}
}

func TestListSessionsProxiesRuntimeResult(t *testing.T) {
	runtime := newFakeRuntimeClient()
	client := newBridgeClient(t, runtime)

	response := client.Request("session/list", map[string]any{"cwd": "/tmp/project"})
	var result struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
			Title     string `json:"title"`
		} `json:"sessions"`
		NextCursor string `json:"nextCursor"`
	}
	response.ResultInto(t, &result)
	if len(result.Sessions) != 1 || result.Sessions[0].SessionID != "sess-bridge" {
		t.Fatalf("list result = %#v, want sess-bridge", result)
	}
	if result.NextCursor != "next" {
		t.Fatalf("NextCursor = %q, want next", result.NextCursor)
	}
}

func TestCancelNotificationProxiesToRuntime(t *testing.T) {
	runtime := newFakeRuntimeClient()
	client := newBridgeClient(t, runtime)

	client.Notify("session/cancel", map[string]string{"sessionId": "sess-bridge"})

	if runtime.notified("session/cancel") != 1 {
		t.Fatalf("session/cancel notifications = %d, want 1", runtime.notified("session/cancel"))
	}
}

func TestCloseSessionProxiesToRuntime(t *testing.T) {
	runtime := newFakeRuntimeClient()
	client := newBridgeClient(t, runtime)

	response := client.Request("session/close", map[string]string{"sessionId": "sess-bridge"})
	if response.Error != nil {
		t.Fatalf("close error = %+v", response.Error)
	}
	if runtime.called("session/close") != 1 {
		t.Fatalf("session/close calls = %d, want 1", runtime.called("session/close"))
	}
}

func TestDeleteSessionProxiesToRuntime(t *testing.T) {
	runtime := newFakeRuntimeClient()
	client := newBridgeClient(t, runtime)

	response := client.Request("session/delete", map[string]string{"sessionId": "sess-bridge"})
	if response.Error != nil {
		t.Fatalf("delete error = %+v", response.Error)
	}
	if runtime.called("session/delete") != 1 {
		t.Fatalf("session/delete calls = %d, want 1", runtime.called("session/delete"))
	}
}

func TestRuntimeRPCErrorMapsToACPError(t *testing.T) {
	runtime := newFakeRuntimeClient()
	runtime.err = &runtimejsonrpc.RPCError{Code: -32010, Message: "runtime exploded"}
	client := newBridgeClient(t, runtime)

	response := client.Request("session/new", map[string]any{"cwd": "/tmp/project"})
	if response.Error == nil {
		t.Fatal("response error is nil, want runtime error")
	}
	if response.Error.Code != -32010 || response.Error.Message != "runtime exploded" {
		t.Fatalf("response error = %+v, want runtime exploded", response.Error)
	}
}

func TestInvalidParamsReturnInvalidParamsError(t *testing.T) {
	runtime := newFakeRuntimeClient()
	client := newBridgeClient(t, runtime)

	response := client.Request("session/new", "not-an-object")
	if response.Error == nil {
		t.Fatal("response error is nil, want invalid params")
	}
	if response.Error.Code != -32602 {
		t.Fatalf("error code = %d, want -32602", response.Error.Code)
	}
	if runtime.called("session/new") != 0 {
		t.Fatalf("session/new calls = %d, want 0", runtime.called("session/new"))
	}
}

func newBridgeClient(t testing.TB, runtime *fakeRuntimeClient) *acptest.Client {
	t.Helper()
	server := acp.NewServer(acp.AdapterInfo{Name: "test"}, runtimebridge.New(runtime).Options()...)
	return acptest.NewClient(t, server)
}

type fakeRuntimeClient struct {
	mu            sync.Mutex
	calls         map[string]int
	notifications map[string]int
	events        chan runtimejsonrpc.Event
	err           error
}

func newFakeRuntimeClient() *fakeRuntimeClient {
	return &fakeRuntimeClient{
		calls:         map[string]int{},
		notifications: map[string]int{},
		events:        make(chan runtimejsonrpc.Event, 8),
	}
}

func (f *fakeRuntimeClient) Request(_ctx context.Context, method string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	f.calls[method]++
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	switch method {
	case "session/new":
		return json.RawMessage(`{"sessionId":"sess-bridge"}`), nil
	case "session/prompt":
		f.events <- runtimejsonrpc.Event{
			Method: "session/update",
			Params: json.RawMessage(`{"sessionId":"sess-bridge","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}}`),
		}
		return json.RawMessage(`{"stopReason":"end_turn"}`), nil
	case "session/load":
		f.events <- runtimejsonrpc.Event{
			Method: "session/update",
			Params: json.RawMessage(`{"sessionId":"sess-bridge","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"replay"}}}`),
		}
		return json.RawMessage(`null`), nil
	case "session/resume":
		return json.RawMessage(`{"mode":"default"}`), nil
	case "session/list":
		return json.RawMessage(`{"sessions":[{"sessionId":"sess-bridge","cwd":"/tmp/project","title":"Bridge session"}],"nextCursor":"next"}`), nil
	case "session/close":
		return json.RawMessage(`{}`), nil
	case "session/delete":
		return json.RawMessage(`{}`), nil
	default:
		return nil, &runtimejsonrpc.RPCError{Code: -32601, Message: "method not found"}
	}
}

func (f *fakeRuntimeClient) Notify(_ctx context.Context, method string, _params any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.notifications[method]++
	return nil
}

func (f *fakeRuntimeClient) Events() <-chan runtimejsonrpc.Event {
	return f.events
}

func (f *fakeRuntimeClient) called(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

func (f *fakeRuntimeClient) notified(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.notifications[method]
}

var _ runtimebridge.RuntimeClient = (*fakeRuntimeClient)(nil)
