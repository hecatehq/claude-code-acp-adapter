package runtimeacp_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/hecatehq/claude-code-acp-adapter/internal/runtimeacp"
	"github.com/hecatehq/claude-code-acp-adapter/internal/runtimejsonrpc"
	"github.com/hecatehq/claude-code-acp-adapter/internal/runtimeproc"
)

func TestNewSessionSendsWorkspaceAndMCPServers(t *testing.T) {
	client := newSessionClient(t)

	result, err := runtimeacp.NewSession(context.Background(), client, runtimeacp.NewSessionParams{
		CWD:                   "/tmp/project",
		AdditionalDirectories: []string{"/tmp/shared"},
		MCPServers: []runtimeacp.MCPServer{{
			Name:    "filesystem",
			Command: "/bin/mcp",
			Args:    []string{"--stdio"},
			Env:     []runtimeacp.EnvVariable{{Name: "TOKEN", Value: "secret"}},
		}},
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	if result.SessionID != "sess-test" {
		t.Fatalf("SessionID = %q, want sess-test", result.SessionID)
	}
}

func TestPromptSendsTextContentAndParsesStopReason(t *testing.T) {
	client := newSessionClient(t)

	resultCh := make(chan error, 1)
	go func() {
		result, err := runtimeacp.Prompt(context.Background(), client, runtimeacp.PromptParams{
			SessionID: "sess-test",
			Prompt: []runtimeacp.ContentBlock{{
				Type: "text",
				Text: "hello",
			}},
		})
		if err != nil {
			resultCh <- err
			return
		}
		if result.StopReason != runtimeacp.StopReasonEndTurn {
			resultCh <- errors.New("unexpected stop reason: " + string(result.StopReason))
			return
		}
		resultCh <- nil
	}()

	event := nextRuntimeACPEvent(t, client)
	if event.Method != "session/update" {
		t.Fatalf("event method = %q, want session/update", event.Method)
	}
	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("Prompt returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for prompt result")
	}
}

func TestCancelSendsNotification(t *testing.T) {
	client := newSessionClient(t)

	if err := runtimeacp.Cancel(context.Background(), client, runtimeacp.CancelParams{SessionID: "sess-test"}); err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	event := nextRuntimeACPEvent(t, client)
	if event.Method != "runtime/cancel_seen" {
		t.Fatalf("event method = %q, want runtime/cancel_seen", event.Method)
	}
}

func TestCloseSessionSendsRequest(t *testing.T) {
	client := newSessionClient(t)

	if err := runtimeacp.CloseSession(context.Background(), client, runtimeacp.CloseSessionParams{SessionID: "sess-test"}); err != nil {
		t.Fatalf("CloseSession returned error: %v", err)
	}
}

func TestSessionRPCErrorPropagates(t *testing.T) {
	client := newSessionClient(t)

	_, err := runtimeacp.NewSession(context.Background(), client, runtimeacp.NewSessionParams{CWD: "/tmp/fail"})
	var rpcErr *runtimejsonrpc.RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("NewSession error = %T %[1]v, want RPCError", err)
	}
	if rpcErr.Code != -32010 {
		t.Fatalf("RPCError.Code = %d, want -32010", rpcErr.Code)
	}
}

func newSessionClient(t testing.TB) *runtimejsonrpc.Client {
	t.Helper()
	t.Setenv("GO_WANT_RUNTIMEACP_SESSION_HELPER", "1")
	launcher := runtimeproc.NewLauncher(runtimeproc.Config{
		Binary:     os.Args[0],
		Args:       []string{"-test.run=TestRuntimeACPSessionHelper", "--"},
		InheritEnv: []string{"GO_WANT_RUNTIMEACP_SESSION_HELPER"},
	})
	client, err := runtimejsonrpc.Connect(context.Background(), runtimejsonrpc.ConnectSpec{
		Launcher: launcher,
		Launch: runtimeproc.LaunchSpec{
			WorkDir: t.TempDir(),
		},
	})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Kill()
		_ = client.Wait()
	})
	return client
}

func nextRuntimeACPEvent(t testing.TB, client *runtimejsonrpc.Client) runtimejsonrpc.Event {
	t.Helper()
	select {
	case event := <-client.Events():
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime event")
	}
	return runtimejsonrpc.Event{}
}

func TestRuntimeACPSessionHelper(t *testing.T) {
	if os.Getenv("GO_WANT_RUNTIMEACP_SESSION_HELPER") != "1" {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id,omitempty"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := decoder.Decode(&req); err != nil {
			return
		}
		if len(req.ID) == 0 {
			if req.Method == "session/cancel" && string(req.Params) == `{"sessionId":"sess-test"}` {
				_ = encoder.Encode(map[string]any{
					"jsonrpc": "2.0",
					"method":  "runtime/cancel_seen",
					"params":  map[string]any{"ok": true},
				})
			}
			continue
		}
		switch req.Method {
		case "session/new":
			var params runtimeacp.NewSessionParams
			if err := json.Unmarshal(req.Params, &params); err != nil ||
				params.CWD == "" ||
				(params.CWD != "/tmp/project" && params.CWD != "/tmp/fail") {
				writeSessionError(encoder, req.ID, -32602, "bad session/new params")
				continue
			}
			if params.CWD == "/tmp/fail" {
				writeSessionError(encoder, req.ID, -32010, "session failed")
				continue
			}
			if len(params.MCPServers) != 1 ||
				params.MCPServers[0].Name != "filesystem" ||
				len(params.AdditionalDirectories) != 1 {
				writeSessionError(encoder, req.ID, -32602, "missing mcp/additional dirs")
				continue
			}
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(req.ID),
				"result":  map[string]any{"sessionId": "sess-test"},
			})
		case "session/prompt":
			var params runtimeacp.PromptParams
			if err := json.Unmarshal(req.Params, &params); err != nil ||
				params.SessionID != "sess-test" ||
				len(params.Prompt) != 1 ||
				params.Prompt[0].Type != "text" ||
				params.Prompt[0].Text != "hello" {
				writeSessionError(encoder, req.ID, -32602, "bad prompt params")
				continue
			}
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"method":  "session/update",
				"params": map[string]any{
					"sessionId": "sess-test",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": "hi"},
					},
				},
			})
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(req.ID),
				"result":  map[string]any{"stopReason": "end_turn"},
			})
		case "session/close":
			var params runtimeacp.CloseSessionParams
			if err := json.Unmarshal(req.Params, &params); err != nil || params.SessionID != "sess-test" {
				writeSessionError(encoder, req.ID, -32602, "bad close params")
				continue
			}
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(req.ID),
				"result":  map[string]any{},
			})
		default:
			writeSessionError(encoder, req.ID, -32601, "method not found")
		}
	}
}

func writeSessionError(encoder *json.Encoder, id json.RawMessage, code int, message string) {
	_ = encoder.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error":   map[string]any{"code": code, "message": message},
	})
}
