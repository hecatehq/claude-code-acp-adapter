package runtimeacp_test

import (
	"context"
	"encoding/json"
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
