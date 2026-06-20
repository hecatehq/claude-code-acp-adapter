package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/hecatehq/acp-adapter-kit/adaptercli"
	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	adapterprocess "github.com/hecatehq/acp-adapter-kit/process"
)

const testClaudeSessionID = "550e8400-e29b-41d4-a716-446655440000"

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
	var result struct {
		AgentInfo struct {
			Name    string `json:"name"`
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"agentInfo"`
		AgentCapabilities struct {
			PromptCapabilities struct {
				Image           bool `json:"image"`
				EmbeddedContext bool `json:"embeddedContext"`
			} `json:"promptCapabilities"`
			MCPCapabilities struct {
				HTTP bool `json:"http"`
				SSE  bool `json:"sse,omitempty"`
			} `json:"mcpCapabilities"`
		} `json:"agentCapabilities"`
	}
	rawResult, err := json.Marshal(response["result"])
	if err != nil {
		t.Fatalf("marshal initialize result: %v", err)
	}
	if err := json.Unmarshal(rawResult, &result); err != nil {
		t.Fatalf("decode initialize result: %v\n%s", err, rawResult)
	}
	if result.AgentInfo.Name != "claude-code-acp-adapter" || result.AgentInfo.Title != "Claude Code ACP Adapter" {
		t.Fatalf("agent info = %#v, want Claude adapter metadata", result.AgentInfo)
	}
	if !result.AgentCapabilities.PromptCapabilities.Image || !result.AgentCapabilities.PromptCapabilities.EmbeddedContext {
		t.Fatalf("prompt capabilities = %#v, want image + embedded context", result.AgentCapabilities.PromptCapabilities)
	}
	if !result.AgentCapabilities.MCPCapabilities.HTTP || !result.AgentCapabilities.MCPCapabilities.SSE {
		t.Fatalf("mcp capabilities = %#v, want HTTP + SSE", result.AgentCapabilities.MCPCapabilities)
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
		`{"jsonrpc":"2.0","id":4,"method":"session/list","params":{"cwd":"` + workdir + `"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"session/delete","params":{"sessionId":"app-session"}}`,
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
	if len(responses) != 6 {
		t.Fatalf("got %d envelopes, want initialize, session/new, update, prompt, list, delete\n%s", len(responses), stdout.String())
	}
	if responses[0].Error != nil {
		t.Fatalf("initialize error = %+v", responses[0].Error)
	}
	var initialize struct {
		AgentInfo struct {
			Name string `json:"name"`
		} `json:"agentInfo"`
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
	}
	decodeAppResult(t, responses[0], &initialize)
	if initialize.AgentInfo.Name != "app-helper-runtime" {
		t.Fatalf("initialize agent name = %q, want app-helper-runtime", initialize.AgentInfo.Name)
	}
	if !initialize.AgentCapabilities.LoadSession {
		t.Fatal("initialize loadSession = false, want true")
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
	var list struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
		} `json:"sessions"`
	}
	decodeAppResult(t, responses[4], &list)
	if len(list.Sessions) != 1 || list.Sessions[0].SessionID != "app-session" {
		t.Fatalf("list result = %#v, want app-session", list)
	}
	var deleteResult map[string]any
	decodeAppResult(t, responses[5], &deleteResult)
}

func TestRuntimeFlagsForwardInitializeClientCapabilities(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	workdir := t.TempDir()
	input := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientCapabilities":{"auth":{"terminal":true},"terminal":true}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"` + workdir + `"}}`,
	}, "\n") + "\n")

	code := Run([]string{
		"--runtime-binary", os.Args[0],
		"--runtime-workdir", workdir,
		"--runtime-arg=-test.run=TestAppRuntimeHelper",
		"--runtime-arg=--",
		"--runtime-arg=app-runtime-helper",
		"--runtime-arg=require-terminal-capability",
		"--runtime-arg=require-auth-terminal-capability",
	}, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0; stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	responses := decodeAppResponses(t, stdout.Bytes())
	if len(responses) != 2 {
		t.Fatalf("got %d envelopes, want initialize + session/new\n%s", len(responses), stdout.String())
	}
	if responses[0].Error != nil {
		t.Fatalf("initialize error = %+v", responses[0].Error)
	}
	var initialize struct {
		AgentInfo struct {
			Name string `json:"name"`
		} `json:"agentInfo"`
	}
	decodeAppResult(t, responses[0], &initialize)
	if initialize.AgentInfo.Name != "app-helper-runtime" {
		t.Fatalf("initialize agent name = %q, want app-helper-runtime", initialize.AgentInfo.Name)
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	decodeAppResult(t, responses[1], &session)
	if session.SessionID != "app-session" {
		t.Fatalf("sessionId = %q, want app-session", session.SessionID)
	}
}

func TestRuntimeFlagsInheritClaudeEnvironmentPolicy(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-claude-runtime")
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/claude-runtime-config")
	t.Setenv("UNLISTED_AGENT_SECRET", "must-not-leak")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	workdir := t.TempDir()
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")

	code := Run([]string{
		"--runtime-binary", os.Args[0],
		"--runtime-workdir", workdir,
		"--runtime-arg=-test.run=TestAppRuntimeHelper",
		"--runtime-arg=--",
		"--runtime-arg=app-runtime-helper",
		"--runtime-arg=require-claude-runtime-env",
	}, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0; stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	responses := decodeAppResponses(t, stdout.Bytes())
	if len(responses) != 1 {
		t.Fatalf("got %d envelopes, want initialize response\n%s", len(responses), stdout.String())
	}
	var initialize struct {
		AgentInfo struct {
			Name string `json:"name"`
		} `json:"agentInfo"`
	}
	decodeAppResult(t, responses[0], &initialize)
	if initialize.AgentInfo.Name != "app-helper-runtime" {
		t.Fatalf("initialize agent name = %q, want app-helper-runtime", initialize.AgentInfo.Name)
	}
}

func TestCommandBridgeRunsClaudePrintWithConfigOptions(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	workdir := t.TempDir()
	extraDir := t.TempDir()
	input := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"` + workdir + `","additionalDirectories":["` + extraDir + `"]}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/set_config_option","params":{"sessionId":"` + testClaudeSessionID + `","configId":"model","value":"sonnet"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/set_config_option","params":{"sessionId":"` + testClaudeSessionID + `","configId":"effort","value":"high"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"session/set_config_option","params":{"sessionId":"` + testClaudeSessionID + `","configId":"permission_mode","value":"plan"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"session/prompt","params":{"sessionId":"` + testClaudeSessionID + `","prompt":[{"type":"text","text":"hello claude"}]}}`,
	}, "\n") + "\n")
	spec := adapterSpec(input, &stdout, &stderr)
	spec.Command.NewID = func() string { return testClaudeSessionID }
	spec.Command.Runner = commandbridge.RunnerFunc(func(_ context.Context, got adapterprocess.Spec) (adapterprocess.Result, error) {
		wantArgs := []string{
			"--print",
			"--output-format", "stream-json",
			"--include-partial-messages",
			"--verbose",
			"--session-id", testClaudeSessionID,
			"--permission-mode", "plan",
			"--add-dir", extraDir,
			"--model", "sonnet",
			"--effort", "high",
			"hello claude",
		}
		if got.Command != "claude" || got.Dir != workdir || !reflect.DeepEqual(got.Args, wantArgs) {
			t.Fatalf("process spec = %#v, want claude args %#v", got, wantArgs)
		}
		return adapterprocess.Result{Stdout: []byte("claude answer")}, nil
	})

	code := adaptercli.Run(nil, spec)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0; stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	responses := decodeAppResponses(t, stdout.Bytes())
	if len(responses) != 13 {
		t.Fatalf("got %d envelopes, want available commands, session/new, three config update notifications + responses, tool start, assistant update, tool finish, session info, prompt result\n%s", len(responses), stdout.String())
	}
	commands := decodeAppUpdate(t, responses[0])
	if commands.Update.SessionUpdate != "available_commands_update" ||
		len(commands.Update.AvailableCommands) != 4 ||
		commands.Update.AvailableCommands[0].Name != "init" ||
		commands.Update.AvailableCommands[1].Name != "review" ||
		commands.Update.AvailableCommands[2].Name != "code-review" ||
		commands.Update.AvailableCommands[3].Name != "security-review" {
		t.Fatalf("available commands = %#v, want Claude command set", commands)
	}
	var created struct {
		SessionID     string `json:"sessionId"`
		ConfigOptions []struct {
			ID           string `json:"id"`
			Category     string `json:"category"`
			CurrentValue string `json:"currentValue"`
		} `json:"configOptions"`
	}
	decodeAppResult(t, responses[1], &created)
	if created.SessionID != testClaudeSessionID || len(created.ConfigOptions) != 3 {
		t.Fatalf("created session = %#v, want id and three config options", created)
	}
	if created.ConfigOptions[0].ID != "model" || created.ConfigOptions[0].Category != "model" {
		t.Fatalf("model option = %#v, want model category", created.ConfigOptions[0])
	}
	if created.ConfigOptions[1].ID != "effort" || created.ConfigOptions[1].Category != "thought_level" {
		t.Fatalf("effort option = %#v, want thought_level category", created.ConfigOptions[1])
	}
	if created.ConfigOptions[2].ID != "permission_mode" || created.ConfigOptions[2].Category != "permission" || created.ConfigOptions[2].CurrentValue != "dontAsk" {
		t.Fatalf("permission option = %#v, want permission category with dontAsk default", created.ConfigOptions[2])
	}
	assertConfigOptionUpdate(t, responses[2], "model", "sonnet")
	var modelSet struct {
		ConfigOptions []struct {
			ID           string `json:"id"`
			CurrentValue string `json:"currentValue"`
		} `json:"configOptions"`
	}
	decodeAppResult(t, responses[3], &modelSet)
	if len(modelSet.ConfigOptions) != 3 || modelSet.ConfigOptions[0].CurrentValue != "sonnet" {
		t.Fatalf("model set result = %#v, want selected model", modelSet.ConfigOptions)
	}
	assertConfigOptionUpdate(t, responses[4], "effort", "high")
	assertConfigOptionUpdate(t, responses[6], "permission_mode", "plan")

	start := decodeAppUpdate(t, responses[8])
	if start.Update.SessionUpdate != "tool_call" ||
		start.Update.Status != "in_progress" ||
		start.Update.ToolCallID == "" {
		t.Fatalf("tool start = %#v, want running command", start)
	}
	update := decodeAppUpdate(t, responses[9])
	if update.Update.SessionUpdate != "agent_message_chunk" || decodeAppChunkText(t, update.Update.Content) != "claude answer" {
		t.Fatalf("assistant update = %#v, want claude answer", update)
	}
	finish := decodeAppUpdate(t, responses[10])
	if finish.Update.SessionUpdate != "tool_call_update" ||
		finish.Update.ToolCallID != start.Update.ToolCallID ||
		finish.Update.Status != "completed" {
		t.Fatalf("tool finish = %#v, want completed command", finish)
	}
	info := decodeAppUpdate(t, responses[11])
	if info.Update.SessionUpdate != "session_info_update" ||
		info.Update.Title != "hello claude" ||
		info.Update.UpdatedAt == "" {
		t.Fatalf("session info = %#v, want transcript metadata", info)
	}
	var prompt struct {
		StopReason string `json:"stopReason"`
	}
	decodeAppResult(t, responses[12], &prompt)
	if prompt.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn", prompt.StopReason)
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
	requireTerminalCapability := hasArg(os.Args, "require-terminal-capability")
	requireAuthTerminalCapability := hasArg(os.Args, "require-auth-terminal-capability")
	if hasArg(os.Args, "require-claude-runtime-env") {
		if os.Getenv("ANTHROPIC_API_KEY") != "sk-claude-runtime" {
			fmt.Fprintf(os.Stderr, "ANTHROPIC_API_KEY=%q\n", os.Getenv("ANTHROPIC_API_KEY"))
			os.Exit(7)
		}
		if os.Getenv("CLAUDE_CONFIG_DIR") != "/tmp/claude-runtime-config" {
			fmt.Fprintf(os.Stderr, "CLAUDE_CONFIG_DIR=%q\n", os.Getenv("CLAUDE_CONFIG_DIR"))
			os.Exit(8)
		}
		if os.Getenv("UNLISTED_AGENT_SECRET") != "" {
			fmt.Fprint(os.Stderr, "UNLISTED_AGENT_SECRET leaked\n")
			os.Exit(9)
		}
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
				ClientCapabilities struct {
					Auth struct {
						Terminal bool `json:"terminal"`
					} `json:"auth"`
					Terminal bool `json:"terminal"`
				} `json:"clientCapabilities"`
			}
			if err := json.Unmarshal(msg.Params, &req); err != nil {
				_ = encoder.Encode(appRuntimeError(msg.ID, -32602, "invalid initialize params", err.Error()))
				continue
			}
			if requireTerminalCapability && !req.ClientCapabilities.Terminal {
				_ = encoder.Encode(appRuntimeError(msg.ID, -32050, "missing terminal capability", string(msg.Params)))
				continue
			}
			if requireAuthTerminalCapability && !req.ClientCapabilities.Auth.Terminal {
				_ = encoder.Encode(appRuntimeError(msg.ID, -32050, "missing auth terminal capability", string(msg.Params)))
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
					"agentCapabilities": map[string]any{
						"loadSession": true,
					},
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
		case "session/list":
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(msg.ID),
				"result": map[string]any{
					"sessions": []map[string]any{{
						"sessionId": "app-session",
						"cwd":       ".",
						"title":     "App helper session",
					}},
				},
			})
		case "session/delete":
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(msg.ID),
				"result":  map[string]any{},
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

type appSessionUpdate struct {
	Update struct {
		SessionUpdate string          `json:"sessionUpdate"`
		ToolCallID    string          `json:"toolCallId"`
		Status        string          `json:"status"`
		Content       json.RawMessage `json:"content"`
		Title         string          `json:"title"`
		UpdatedAt     string          `json:"updatedAt"`
		ConfigOptions []struct {
			ID           string `json:"id"`
			CurrentValue string `json:"currentValue"`
		} `json:"configOptions"`
		AvailableCommands []struct {
			Name string `json:"name"`
		} `json:"availableCommands"`
	} `json:"update"`
}

func decodeAppUpdate(t testing.TB, response appResponse) appSessionUpdate {
	t.Helper()
	if response.Method != "session/update" {
		t.Fatalf("response method = %q, want session/update", response.Method)
	}
	var update appSessionUpdate
	if err := json.Unmarshal(response.Params, &update); err != nil {
		t.Fatalf("decode update: %v\n%s", err, string(response.Params))
	}
	return update
}

func assertConfigOptionUpdate(t testing.TB, response appResponse, optionID, currentValue string) {
	t.Helper()
	update := decodeAppUpdate(t, response)
	if update.Update.SessionUpdate != "config_option_update" {
		t.Fatalf("session update = %q, want config_option_update", update.Update.SessionUpdate)
	}
	for _, option := range update.Update.ConfigOptions {
		if option.ID == optionID {
			if option.CurrentValue != currentValue {
				t.Fatalf("config option %q current value = %q, want %q", optionID, option.CurrentValue, currentValue)
			}
			return
		}
	}
	t.Fatalf("config options = %#v, missing %q", update.Update.ConfigOptions, optionID)
}

func decodeAppChunkText(t testing.TB, raw json.RawMessage) string {
	t.Helper()
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatalf("decode chunk content: %v\n%s", err, string(raw))
	}
	return content.Text
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

func TestDoctorCommandUsesClaudeDefaults(t *testing.T) {
	cmd := adaptercli.NewRootCommand(adapterSpec(nil, &bytes.Buffer{}, &bytes.Buffer{}))
	doctorCmd, _, err := cmd.Find([]string{"doctor"})
	if err != nil {
		t.Fatalf("find doctor command: %v", err)
	}
	if got := doctorCmd.Flags().Lookup("binary").DefValue; got != "claude" {
		t.Fatalf("doctor binary default = %q, want claude", got)
	}
}

func TestDoctorCommandJSONReportsClaudeEnvironment(t *testing.T) {
	t.Setenv("GO_WANT_APP_DOCTOR_HELPER", "1")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-app-doctor")
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/claude-app-doctor")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{
		"doctor",
		"--binary", os.Args[0],
		"--version-arg=-test.run=TestAppDoctorHelper",
		"--version-arg=--",
		"--version-arg=app-doctor-helper",
		"--json",
	}, nil, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("Run returned %d, want 0; stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	var payload struct {
		OK     bool `json:"ok"`
		Report struct {
			VersionStdout string `json:"version_stdout"`
			Environment   []struct {
				Name      string `json:"name"`
				Present   bool   `json:"present"`
				Sensitive bool   `json:"sensitive"`
			} `json:"environment"`
		} `json:"report"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode doctor JSON: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("doctor payload OK = false: %#v", payload)
	}
	if strings.Contains(payload.Report.VersionStdout, "sk-ant-app-doctor") {
		t.Fatalf("doctor stdout leaked secret: %q", payload.Report.VersionStdout)
	}
	if !envPresentAndSensitive(payload.Report.Environment, "ANTHROPIC_API_KEY") {
		t.Fatalf("ANTHROPIC_API_KEY status missing or not sensitive: %#v", payload.Report.Environment)
	}
	if !envPresent(payload.Report.Environment, "CLAUDE_CONFIG_DIR") {
		t.Fatalf("CLAUDE_CONFIG_DIR status missing: %#v", payload.Report.Environment)
	}
	if !envNamed(payload.Report.Environment, "ANTHROPIC_BASE_URL") {
		t.Fatalf("ANTHROPIC_BASE_URL status missing: %#v", payload.Report.Environment)
	}
}

func TestAppDoctorHelper(t *testing.T) {
	if os.Getenv("GO_WANT_APP_DOCTOR_HELPER") != "1" || !hasArg(os.Args, "app-doctor-helper") {
		return
	}
	fmt.Printf("fake-claude 1.2.3 token=%s\n", os.Getenv("ANTHROPIC_API_KEY"))
	os.Exit(0)
}

func envPresentAndSensitive(statuses []struct {
	Name      string `json:"name"`
	Present   bool   `json:"present"`
	Sensitive bool   `json:"sensitive"`
}, name string) bool {
	for _, status := range statuses {
		if status.Name == name {
			return status.Present && status.Sensitive
		}
	}
	return false
}

func envPresent(statuses []struct {
	Name      string `json:"name"`
	Present   bool   `json:"present"`
	Sensitive bool   `json:"sensitive"`
}, name string) bool {
	for _, status := range statuses {
		if status.Name == name {
			return status.Present
		}
	}
	return false
}

func envNamed(statuses []struct {
	Name      string `json:"name"`
	Present   bool   `json:"present"`
	Sensitive bool   `json:"sensitive"`
}, name string) bool {
	for _, status := range statuses {
		if status.Name == name {
			return true
		}
	}
	return false
}
