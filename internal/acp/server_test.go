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
			MCPSSE:          true,
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
	mcpCaps := caps["mcpCapabilities"].(map[string]any)
	if mcpCaps["http"] != true || mcpCaps["sse"] != true {
		t.Fatalf("mcp capabilities = %#v", mcpCaps)
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
