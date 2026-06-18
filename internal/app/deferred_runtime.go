package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/hecatehq/acp-adapter-kit/acp"
	"github.com/hecatehq/acp-adapter-kit/runtimeacp"
	"github.com/hecatehq/acp-adapter-kit/runtimebridge"
	"github.com/hecatehq/acp-adapter-kit/runtimehost"
	"github.com/hecatehq/acp-adapter-kit/runtimejsonrpc"
)

var errRuntimeHostNotInitialized = errors.New("runtime host is not initialized")

type deferredRuntimeHost struct {
	ctx    context.Context
	spec   runtimehost.Spec
	events chan runtimejsonrpc.Event

	mu   sync.RWMutex
	host *runtimehost.Host
}

func newDeferredRuntimeHost(ctx context.Context, spec runtimehost.Spec) *deferredRuntimeHost {
	if ctx == nil {
		ctx = context.Background()
	}
	events := make(chan runtimejsonrpc.Event)
	close(events)
	return &deferredRuntimeHost{
		ctx:    ctx,
		spec:   spec,
		events: events,
	}
}

func (h *deferredRuntimeHost) Initialize(params json.RawMessage) (any, *acp.RPCError) {
	var req struct {
		ClientCapabilities runtimeacp.ClientCapabilities `json:"clientCapabilities,omitempty"`
	}
	if len(params) != 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &acp.RPCError{Code: -32602, Message: "invalid params", Data: err.Error()}
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.host != nil {
		return h.host.InitializeResult(), nil
	}

	spec := h.spec
	spec.ClientCapabilities = req.ClientCapabilities
	host, err := runtimehost.Start(h.ctx, spec)
	if err != nil {
		return nil, &acp.RPCError{Code: -32000, Message: "start runtime host", Data: err.Error()}
	}
	h.host = host
	return host.InitializeResult(), nil
}

func (h *deferredRuntimeHost) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	client, err := h.runtimeClient()
	if err != nil {
		return nil, err
	}
	return client.Request(ctx, method, params)
}

func (h *deferredRuntimeHost) Notify(ctx context.Context, method string, params any) error {
	client, err := h.runtimeClient()
	if err != nil {
		return err
	}
	return client.Notify(ctx, method, params)
}

func (h *deferredRuntimeHost) Respond(ctx context.Context, id json.RawMessage, result any, rpcErr *runtimejsonrpc.RPCError) error {
	client, err := h.runtimeClient()
	if err != nil {
		return err
	}
	return client.Respond(ctx, id, result, rpcErr)
}

func (h *deferredRuntimeHost) Events() <-chan runtimejsonrpc.Event {
	h.mu.RLock()
	host := h.host
	h.mu.RUnlock()
	if host == nil {
		return h.events
	}
	client := host.RuntimeClient()
	if client == nil {
		return h.events
	}
	return client.Events()
}

func (h *deferredRuntimeHost) Close() error {
	h.mu.Lock()
	host := h.host
	h.host = nil
	h.mu.Unlock()
	if host == nil {
		return nil
	}
	return host.Close()
}

func (h *deferredRuntimeHost) runtimeClient() (runtimebridge.RuntimeClient, error) {
	h.mu.RLock()
	host := h.host
	h.mu.RUnlock()
	if host == nil {
		return nil, errRuntimeHostNotInitialized
	}
	client := host.RuntimeClient()
	if client == nil {
		return nil, errRuntimeHostNotInitialized
	}
	return client, nil
}
