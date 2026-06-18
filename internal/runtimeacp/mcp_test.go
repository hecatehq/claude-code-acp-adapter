package runtimeacp_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/hecatehq/claude-code-acp-adapter/internal/runtimeacp"
)

func TestMCPMessageReturnsRawRuntimeResult(t *testing.T) {
	client := &recordingACPClient{result: json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)}

	raw, err := runtimeacp.MCPMessage(context.Background(), client, runtimeacp.MCPMessageParams{
		ConnectionID: "mcp-conn",
		Method:       "tools/list",
		Params:       json.RawMessage(`{"cursor":"next"}`),
	})
	if err != nil {
		t.Fatalf("MCPMessage returned error: %v", err)
	}
	if client.method != "mcp/message" {
		t.Fatalf("method = %q, want mcp/message", client.method)
	}
	var params runtimeacp.MCPMessageParams
	mustJSONRoundTrip(t, client.params, &params)
	if params.ConnectionID != "mcp-conn" || params.Method != "tools/list" || string(params.Params) != `{"cursor":"next"}` {
		t.Fatalf("params = %#v, want connection, method, and raw params preserved", params)
	}
	if !strings.Contains(string(raw), `"tools"`) {
		t.Fatalf("raw result = %s, want tools payload", raw)
	}
}

func TestACPMCPServerPreservesIDAndMeta(t *testing.T) {
	client := &recordingACPClient{result: json.RawMessage(`{"sessionId":"sess-test"}`)}

	_, err := runtimeacp.NewSession(context.Background(), client, runtimeacp.NewSessionParams{
		CWD: "/tmp/project",
		MCPServers: []runtimeacp.MCPServer{{
			Type: "acp",
			ID:   "mcp-acp-1",
			Name: "Hosted MCP",
			Meta: map[string]any{"owner": "client"},
		}},
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	var params runtimeacp.NewSessionParams
	mustJSONRoundTrip(t, client.params, &params)
	if len(params.MCPServers) != 1 {
		t.Fatalf("mcpServers len = %d, want 1", len(params.MCPServers))
	}
	server := params.MCPServers[0]
	if server.Type != "acp" || server.ID != "mcp-acp-1" || server.Name != "Hosted MCP" {
		t.Fatalf("mcp server = %#v, want ACP id preserved", server)
	}
	if !reflect.DeepEqual(server.Meta, map[string]any{"owner": "client"}) {
		t.Fatalf("mcp server meta = %#v, want owner client", server.Meta)
	}
}
