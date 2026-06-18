package runtimebridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hecatehq/claude-code-acp-adapter/internal/acp"
	"github.com/hecatehq/claude-code-acp-adapter/internal/runtimeacp"
	"github.com/hecatehq/claude-code-acp-adapter/internal/runtimejsonrpc"
)

type RuntimeClient interface {
	runtimeacp.JSONRPCClient
	runtimeacp.Notifier
	Events() <-chan runtimejsonrpc.Event
}

type Bridge struct {
	runtime RuntimeClient
}

func New(runtime RuntimeClient) *Bridge {
	return &Bridge{runtime: runtime}
}

func (b *Bridge) Options() []acp.Option {
	return []acp.Option{
		acp.WithMethod("session/new", b.newSession),
		acp.WithMethod("session/load", b.loadSession),
		acp.WithMethod("session/resume", b.resumeSession),
		acp.WithMethod("session/list", b.listSessions),
		acp.WithMethod("session/prompt", b.prompt),
		acp.WithMethod("session/cancel", b.cancelMethod),
		acp.WithMethod("session/close", b.closeSession),
		acp.WithMethod("session/delete", b.deleteSession),
		acp.WithNotification("session/cancel", b.cancelNotification),
	}
}

func (b *Bridge) newSession(_ *acp.MethodContext, params json.RawMessage) (any, *acp.RPCError) {
	var req runtimeacp.NewSessionParams
	if rpcErr := decodeParams(params, &req); rpcErr != nil {
		return nil, rpcErr
	}
	result, err := runtimeacp.NewSession(context.Background(), b.runtime, req)
	if err != nil {
		return nil, mapRuntimeError(err)
	}
	return result, nil
}

func (b *Bridge) loadSession(ctx *acp.MethodContext, params json.RawMessage) (any, *acp.RPCError) {
	var req runtimeacp.LoadSessionParams
	if rpcErr := decodeParams(params, &req); rpcErr != nil {
		return nil, rpcErr
	}
	return b.callWithEvents(ctx, func() (any, error) {
		return runtimeacp.LoadSession(context.Background(), b.runtime, req)
	})
}

func (b *Bridge) resumeSession(_ *acp.MethodContext, params json.RawMessage) (any, *acp.RPCError) {
	var req runtimeacp.ResumeSessionParams
	if rpcErr := decodeParams(params, &req); rpcErr != nil {
		return nil, rpcErr
	}
	result, err := runtimeacp.ResumeSession(context.Background(), b.runtime, req)
	if err != nil {
		return nil, mapRuntimeError(err)
	}
	return result, nil
}

func (b *Bridge) listSessions(_ *acp.MethodContext, params json.RawMessage) (any, *acp.RPCError) {
	var req runtimeacp.ListSessionsParams
	if rpcErr := decodeParams(params, &req); rpcErr != nil {
		return nil, rpcErr
	}
	result, err := runtimeacp.ListSessions(context.Background(), b.runtime, req)
	if err != nil {
		return nil, mapRuntimeError(err)
	}
	return result, nil
}

func (b *Bridge) prompt(ctx *acp.MethodContext, params json.RawMessage) (any, *acp.RPCError) {
	var req runtimeacp.PromptParams
	if rpcErr := decodeParams(params, &req); rpcErr != nil {
		return nil, rpcErr
	}
	return b.callWithEvents(ctx, func() (any, error) {
		return runtimeacp.Prompt(context.Background(), b.runtime, req)
	})
}

func (b *Bridge) cancelMethod(_ *acp.MethodContext, params json.RawMessage) (any, *acp.RPCError) {
	var req runtimeacp.CancelParams
	if rpcErr := decodeParams(params, &req); rpcErr != nil {
		return nil, rpcErr
	}
	if err := runtimeacp.Cancel(context.Background(), b.runtime, req); err != nil {
		return nil, mapRuntimeError(err)
	}
	return map[string]any{"cancelled": true}, nil
}

func (b *Bridge) cancelNotification(params json.RawMessage) error {
	var req runtimeacp.CancelParams
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("invalid session/cancel params: %w", err)
	}
	return runtimeacp.Cancel(context.Background(), b.runtime, req)
}

func (b *Bridge) closeSession(_ *acp.MethodContext, params json.RawMessage) (any, *acp.RPCError) {
	var req runtimeacp.CloseSessionParams
	if rpcErr := decodeParams(params, &req); rpcErr != nil {
		return nil, rpcErr
	}
	if err := runtimeacp.CloseSession(context.Background(), b.runtime, req); err != nil {
		return nil, mapRuntimeError(err)
	}
	return map[string]any{}, nil
}

func (b *Bridge) deleteSession(_ *acp.MethodContext, params json.RawMessage) (any, *acp.RPCError) {
	var req runtimeacp.DeleteSessionParams
	if rpcErr := decodeParams(params, &req); rpcErr != nil {
		return nil, rpcErr
	}
	if err := runtimeacp.DeleteSession(context.Background(), b.runtime, req); err != nil {
		return nil, mapRuntimeError(err)
	}
	return map[string]any{}, nil
}

func (b *Bridge) callWithEvents(ctx *acp.MethodContext, call func() (any, error)) (any, *acp.RPCError) {
	type callResult struct {
		result any
		err    error
	}
	done := make(chan callResult, 1)
	go func() {
		result, err := call()
		done <- callResult{result: result, err: err}
	}()

	for {
		select {
		case result := <-done:
			if result.err != nil {
				return nil, mapRuntimeError(result.err)
			}
			return result.result, nil
		case event, ok := <-b.runtime.Events():
			if !ok {
				return nil, mapRuntimeError(errors.New("runtime event stream closed"))
			}
			if len(event.ID) != 0 {
				return nil, mapRuntimeError(fmt.Errorf("runtime client request %s is not supported yet", string(event.ID)))
			}
			if err := ctx.Notify(event.Method, eventParams(event)); err != nil {
				return nil, &acp.RPCError{Code: -32000, Message: "notification failed", Data: err.Error()}
			}
		}
	}
}

func eventParams(event runtimejsonrpc.Event) any {
	if len(event.Params) == 0 {
		return nil
	}
	return json.RawMessage(event.Params)
}

func decodeParams(params json.RawMessage, target any) *acp.RPCError {
	if err := json.Unmarshal(params, target); err != nil {
		return &acp.RPCError{Code: -32602, Message: "invalid params", Data: err.Error()}
	}
	return nil
}

func mapRuntimeError(err error) *acp.RPCError {
	var rpcErr *runtimejsonrpc.RPCError
	if errors.As(err, &rpcErr) {
		var data any
		if len(rpcErr.Data) != 0 {
			data = json.RawMessage(rpcErr.Data)
		}
		return &acp.RPCError{Code: rpcErr.Code, Message: rpcErr.Message, Data: data}
	}
	return &acp.RPCError{Code: -32000, Message: "runtime bridge error", Data: err.Error()}
}
