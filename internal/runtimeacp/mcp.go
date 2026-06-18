package runtimeacp

import (
	"context"
	"encoding/json"
)

type MCPMessageParams struct {
	ConnectionID string          `json:"connectionId"`
	Method       string          `json:"method"`
	Params       json.RawMessage `json:"params,omitempty"`
}

func MCPMessage(ctx context.Context, client JSONRPCClient, params MCPMessageParams) (json.RawMessage, error) {
	return requestRaw(ctx, client, "mcp/message", params)
}
