package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var stdout bytes.Buffer
	code := Run([]string{"--version"}, nil, &stdout, nil)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0", code)
	}
	if got, want := stdout.String(), "claude-code-acp-adapter 0.0.0-dev\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	code := Run([]string{"version"}, nil, &stdout, nil)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0", code)
	}
	if got, want := stdout.String(), "claude-code-acp-adapter 0.0.0-dev\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestNoArgsStartsACPStdio(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")

	code := Run(nil, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0; stderr=%q", code, stderr.String())
	}

	var response map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, stdout.String())
	}
	if response["result"] == nil {
		t.Fatalf("response missing result: %#v", response)
	}
}

func TestRuntimeFlagsStartProcessBackedACPBridge(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	workdir := t.TempDir()
	input := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"` + workdir + `"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"app-session","prompt":[{"type":"text","text":"hello"}]}}`,
	}, "\n") + "\n")

	code := Run([]string{
		"--runtime-binary", os.Args[0],
		"--runtime-workdir", workdir,
		"--runtime-arg=-test.run=TestAppRuntimeHelper",
		"--runtime-arg=--",
		"--runtime-arg=app-runtime-helper",
	}, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0; stderr=%q", code, stderr.String())
	}

	responses := decodeAppResponses(t, stdout.Bytes())
	if len(responses) != 4 {
		t.Fatalf("got %d envelopes, want initialize, session/new, update, prompt response\n%s", len(responses), stdout.String())
	}
	if responses[0].Error != nil {
		t.Fatalf("initialize error = %+v", responses[0].Error)
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	decodeAppResult(t, responses[1], &session)
	if session.SessionID != "app-session" {
		t.Fatalf("sessionId = %q, want app-session", session.SessionID)
	}
	if responses[2].Method != "session/update" {
		t.Fatalf("third envelope method = %q, want session/update", responses[2].Method)
	}
	var prompt struct {
		StopReason string `json:"stopReason"`
	}
	decodeAppResult(t, responses[3], &prompt)
	if prompt.StopReason != "end_turn" {
		t.Fatalf("stopReason = %q, want end_turn", prompt.StopReason)
	}
}

func TestRuntimeBinaryRequiresRuntimeWorkdir(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")

	code := Run([]string{"--runtime-binary", os.Args[0]}, input, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("Run returned %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "--runtime-workdir is required") {
		t.Fatalf("stderr = %q, want runtime workdir error", got)
	}
}

func TestUnknownArgDoesNotPrintUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"wat"}, nil, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("Run returned %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, `unknown command "wat"`) {
		t.Fatalf("stderr = %q, want unknown command", got)
	}
}

func TestAppRuntimeHelper(t *testing.T) {
	if !hasArg(os.Args, "app-runtime-helper") {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var msg struct {
			ID     json.RawMessage `json:"id,omitempty"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params,omitempty"`
		}
		if err := decoder.Decode(&msg); err != nil {
			return
		}
		switch msg.Method {
		case "initialize":
			var req struct {
				ProtocolVersion int `json:"protocolVersion"`
				ClientInfo      struct {
					Name    string `json:"name"`
					Title   string `json:"title"`
					Version string `json:"version"`
				} `json:"clientInfo"`
			}
			if err := json.Unmarshal(msg.Params, &req); err != nil {
				_ = encoder.Encode(appRuntimeError(msg.ID, -32602, "invalid initialize params", err.Error()))
				continue
			}
			if req.ProtocolVersion != 1 || req.ClientInfo.Name != "claude-code-acp-adapter" {
				_ = encoder.Encode(appRuntimeError(msg.ID, -32050, "unexpected initialize params", string(msg.Params)))
				continue
			}
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(msg.ID),
				"result": map[string]any{
					"protocolVersion": 1,
					"agentInfo":       map[string]any{"name": "app-helper-runtime"},
				},
			})
		case "session/new":
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(msg.ID),
				"result":  map[string]any{"sessionId": "app-session"},
			})
		case "session/prompt":
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"method":  "session/update",
				"params": map[string]any{
					"sessionId": "app-session",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": "hello from app helper"},
					},
				},
			})
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(msg.ID),
				"result":  map[string]any{"stopReason": "end_turn"},
			})
		default:
			_ = encoder.Encode(appRuntimeError(msg.ID, -32601, fmt.Sprintf("method %s not found", msg.Method), nil))
		}
	}
}

type appResponse struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func decodeAppResponses(t testing.TB, raw []byte) []appResponse {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	responses := make([]appResponse, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var response appResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("decode response line %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	return responses
}

func decodeAppResult(t testing.TB, response appResponse, target any) {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("response has error: %+v", response.Error)
	}
	if err := json.Unmarshal(response.Result, target); err != nil {
		t.Fatalf("decode result: %v\n%s", err, string(response.Result))
	}
}

func appRuntimeError(id json.RawMessage, code int, message string, data any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error": map[string]any{
			"code":    code,
			"message": message,
			"data":    data,
		},
	}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestDoctorCommandReportsFailureWithoutUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"doctor", "--binary", "/definitely/missing/claude"}, nil, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("Run returned %d, want 1", code)
	}
	if got := stdout.String(); !strings.Contains(got, "claude-code-acp-adapter doctor: failed") {
		t.Fatalf("stdout = %q, want doctor failure report", got)
	}
	if got := stderr.String(); !strings.Contains(got, "find runtime binary") {
		t.Fatalf("stderr = %q, want runtime binary error", got)
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr = %q, want no usage", stderr.String())
	}
}
