package acp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestInitializeResponse(t *testing.T) {
	server := NewServer(AdapterInfo{
		Name:    "claude-code-acp-adapter",
		Title:   "Claude Code ACP Adapter",
		Version: "test",
		Capabilities: Capabilities{
			Images:          true,
			EmbeddedContext: true,
			MCPHTTP:         true,
		},
	})

	var out bytes.Buffer
	err := server.Serve(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n"), &out)
	if err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v\n%s", err, out.String())
	}
	result := got["result"].(map[string]any)
	info := result["agentInfo"].(map[string]any)
	if info["name"] != "claude-code-acp-adapter" {
		t.Fatalf("agent name = %v", info["name"])
	}
	caps := result["agentCapabilities"].(map[string]any)
	promptCaps := caps["promptCapabilities"].(map[string]any)
	if promptCaps["image"] != true || promptCaps["embeddedContext"] != true {
		t.Fatalf("prompt capabilities = %#v", promptCaps)
	}
}

func TestPromptReturnsStructuredNotImplemented(t *testing.T) {
	server := NewServer(AdapterInfo{Name: "claude-code-acp-adapter"})

	var out bytes.Buffer
	err := server.Serve(strings.NewReader(`{"jsonrpc":"2.0","id":"p1","method":"session/prompt"}`+"\n"), &out)
	if err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	var got response
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Error == nil {
		t.Fatal("expected error")
	}
	if got.Error.Code != -32004 {
		t.Fatalf("error code = %d, want -32004", got.Error.Code)
	}
}

func TestMethodContextRequestWaitsForClientResponse(t *testing.T) {
	server := NewServer(AdapterInfo{Name: "claude-code-acp-adapter"}, WithMethod("test/needs_client", func(ctx *MethodContext, _ json.RawMessage) (any, *RPCError) {
		result, rpcErr, err := ctx.Request("client/approve", map[string]any{"toolCallId": "tool-1"})
		if err != nil {
			return nil, &RPCError{Code: -32000, Message: "request failed", Data: err.Error()}
		}
		if rpcErr != nil {
			return nil, rpcErr
		}
		var decoded map[string]any
		if err := json.Unmarshal(result, &decoded); err != nil {
			return nil, &RPCError{Code: -32000, Message: "decode failed", Data: err.Error()}
		}
		return map[string]any{"decision": decoded["decision"]}, nil
	}))

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"test/needs_client","params":{}}`,
		`{"jsonrpc":"2.0","id":"server-1","result":{"decision":"approved"}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := server.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	envelopes := decodeServerEnvelopes(t, out.Bytes())
	if len(envelopes) != 2 {
		t.Fatalf("got %d envelopes, want client request + final response\n%s", len(envelopes), out.String())
	}
	if envelopes[0].Method != "client/approve" {
		t.Fatalf("first method = %q, want client/approve", envelopes[0].Method)
	}
	if string(envelopes[0].ID) != `"server-1"` {
		t.Fatalf("client request id = %s, want server-1", envelopes[0].ID)
	}
	var result map[string]any
	if err := json.Unmarshal(envelopes[1].Result, &result); err != nil {
		t.Fatalf("decode final result: %v", err)
	}
	if result["decision"] != "approved" {
		t.Fatalf("final result = %#v, want approved", result)
	}
}

func TestMethodContextRequestReturnsClientRPCError(t *testing.T) {
	server := NewServer(AdapterInfo{Name: "claude-code-acp-adapter"}, WithMethod("test/needs_client", func(ctx *MethodContext, _ json.RawMessage) (any, *RPCError) {
		_, rpcErr, err := ctx.Request("client/approve", nil)
		if err != nil {
			return nil, &RPCError{Code: -32000, Message: "request failed", Data: err.Error()}
		}
		if rpcErr != nil {
			return nil, rpcErr
		}
		return map[string]any{"unexpected": true}, nil
	}))

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"test/needs_client","params":{}}`,
		`{"jsonrpc":"2.0","id":"server-1","error":{"code":-32060,"message":"denied","data":{"reason":"policy"}}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := server.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	envelopes := decodeServerEnvelopes(t, out.Bytes())
	if len(envelopes) != 2 {
		t.Fatalf("got %d envelopes, want client request + final response", len(envelopes))
	}
	if envelopes[1].Error == nil {
		t.Fatalf("final response error is nil: %#v", envelopes[1])
	}
	if envelopes[1].Error.Code != -32060 || envelopes[1].Error.Message != "denied" {
		t.Fatalf("final error = %+v, want denied -32060", envelopes[1].Error)
	}
}

type serverEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

func decodeServerEnvelopes(t testing.TB, raw []byte) []serverEnvelope {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	envelopes := make([]serverEnvelope, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var envelope serverEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("decode envelope %q: %v", line, err)
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}
