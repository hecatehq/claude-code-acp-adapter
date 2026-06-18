package runtimeacp

import "context"

type AuthenticateParams struct {
	MethodID string `json:"methodId"`
}

func Authenticate(ctx context.Context, client JSONRPCClient, params AuthenticateParams) error {
	var result map[string]any
	return requestInto(ctx, client, "authenticate", params, &result)
}

func Logout(ctx context.Context, client JSONRPCClient) error {
	var result map[string]any
	return requestInto(ctx, client, "logout", map[string]any{}, &result)
}
