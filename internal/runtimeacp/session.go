package runtimeacp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type Notifier interface {
	Notify(ctx context.Context, method string, params any) error
}

type NewSessionParams struct {
	CWD                   string      `json:"cwd"`
	MCPServers            []MCPServer `json:"mcpServers,omitempty"`
	AdditionalDirectories []string    `json:"additionalDirectories,omitempty"`
}

type NewSessionResult struct {
	SessionID     string            `json:"sessionId"`
	ConfigOptions []json.RawMessage `json:"configOptions,omitempty"`
	Modes         json.RawMessage   `json:"modes,omitempty"`

	raw json.RawMessage
}

type LoadSessionParams struct {
	SessionID             string      `json:"sessionId"`
	CWD                   string      `json:"cwd"`
	MCPServers            []MCPServer `json:"mcpServers,omitempty"`
	AdditionalDirectories []string    `json:"additionalDirectories,omitempty"`
}

type ResumeSessionParams struct {
	SessionID             string      `json:"sessionId"`
	CWD                   string      `json:"cwd"`
	MCPServers            []MCPServer `json:"mcpServers,omitempty"`
	AdditionalDirectories []string    `json:"additionalDirectories,omitempty"`
}

type ListSessionsParams struct {
	CWD    string `json:"cwd,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type ListSessionsResult struct {
	Sessions   []SessionInfo `json:"sessions"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type SessionInfo struct {
	SessionID             string          `json:"sessionId"`
	CWD                   string          `json:"cwd"`
	AdditionalDirectories []string        `json:"additionalDirectories,omitempty"`
	Title                 string          `json:"title,omitempty"`
	UpdatedAt             string          `json:"updatedAt,omitempty"`
	Meta                  json.RawMessage `json:"_meta,omitempty"`
}

type MCPServer struct {
	Type    string        `json:"type,omitempty"`
	Name    string        `json:"name"`
	Command string        `json:"command,omitempty"`
	Args    []string      `json:"args,omitempty"`
	Env     []EnvVariable `json:"env,omitempty"`
	URL     string        `json:"url,omitempty"`
	Headers []HTTPHeader  `json:"headers,omitempty"`
}

type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HTTPHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type ContentBlock struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	MimeType string            `json:"mimeType,omitempty"`
	Data     string            `json:"data,omitempty"`
	URI      string            `json:"uri,omitempty"`
	Name     string            `json:"name,omitempty"`
	Resource *EmbeddedResource `json:"resource,omitempty"`
}

type EmbeddedResource struct {
	URI      string `json:"uri"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type PromptResult struct {
	StopReason StopReason `json:"stopReason"`
}

type StopReason string

const (
	StopReasonEndTurn         StopReason = "end_turn"
	StopReasonMaxTokens       StopReason = "max_tokens"
	StopReasonMaxTurnRequests StopReason = "max_turn_requests"
	StopReasonRefusal         StopReason = "refusal"
	StopReasonCancelled       StopReason = "cancelled"
)

type CancelParams struct {
	SessionID string `json:"sessionId"`
}

type CloseSessionParams struct {
	SessionID string `json:"sessionId"`
}

type DeleteSessionParams struct {
	SessionID string `json:"sessionId"`
}

func NewSession(ctx context.Context, client JSONRPCClient, params NewSessionParams) (NewSessionResult, error) {
	resultData, err := requestRaw(ctx, client, "session/new", params)
	if err != nil {
		return NewSessionResult{}, err
	}
	var result NewSessionResult
	if err := json.Unmarshal(resultData, &result); err != nil {
		return NewSessionResult{}, fmt.Errorf("decode session/new result: %w", err)
	}
	if result.SessionID == "" {
		return NewSessionResult{}, errors.New("session/new result missing sessionId")
	}
	result.raw = append(json.RawMessage(nil), resultData...)
	return result, nil
}

func (r NewSessionResult) MarshalJSON() ([]byte, error) {
	if len(r.raw) != 0 {
		return append([]byte(nil), r.raw...), nil
	}
	type alias NewSessionResult
	return json.Marshal(alias(r))
}

func Prompt(ctx context.Context, client JSONRPCClient, params PromptParams) (PromptResult, error) {
	var result PromptResult
	if err := requestInto(ctx, client, "session/prompt", params, &result); err != nil {
		return PromptResult{}, err
	}
	if result.StopReason == "" {
		return PromptResult{}, errors.New("session/prompt result missing stopReason")
	}
	return result, nil
}

func LoadSession(ctx context.Context, client JSONRPCClient, params LoadSessionParams) (json.RawMessage, error) {
	return requestRaw(ctx, client, "session/load", params)
}

func ResumeSession(ctx context.Context, client JSONRPCClient, params ResumeSessionParams) (json.RawMessage, error) {
	return requestRaw(ctx, client, "session/resume", params)
}

func ListSessions(ctx context.Context, client JSONRPCClient, params ListSessionsParams) (ListSessionsResult, error) {
	var result ListSessionsResult
	if err := requestInto(ctx, client, "session/list", params, &result); err != nil {
		return ListSessionsResult{}, err
	}
	if result.Sessions == nil {
		result.Sessions = []SessionInfo{}
	}
	return result, nil
}

func Cancel(ctx context.Context, client Notifier, params CancelParams) error {
	if client == nil {
		return errors.New("runtime ACP notifier is required")
	}
	return client.Notify(ctx, "session/cancel", params)
}

func CloseSession(ctx context.Context, client JSONRPCClient, params CloseSessionParams) error {
	var result map[string]any
	return requestInto(ctx, client, "session/close", params, &result)
}

func DeleteSession(ctx context.Context, client JSONRPCClient, params DeleteSessionParams) error {
	var result map[string]any
	return requestInto(ctx, client, "session/delete", params, &result)
}

func requestRaw(ctx context.Context, client JSONRPCClient, method string, params any) (json.RawMessage, error) {
	if client == nil {
		return nil, errors.New("runtime ACP client is required")
	}
	resultData, err := client.Request(ctx, method, params)
	if err != nil {
		return nil, err
	}
	if len(resultData) == 0 {
		return json.RawMessage("null"), nil
	}
	return append(json.RawMessage(nil), resultData...), nil
}

func requestInto(ctx context.Context, client JSONRPCClient, method string, params any, out any) error {
	if client == nil {
		return errors.New("runtime ACP client is required")
	}
	resultData, err := client.Request(ctx, method, params)
	if err != nil {
		return err
	}
	if len(resultData) == 0 || string(resultData) == "null" {
		return nil
	}
	if err := json.Unmarshal(resultData, out); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}
