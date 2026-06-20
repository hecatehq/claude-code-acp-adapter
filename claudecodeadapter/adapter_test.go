package claudecodeadapter_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/hecatehq/acp-adapter-kit/acptest"
	"github.com/hecatehq/acp-adapter-kit/adaptertest"
	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	"github.com/hecatehq/acp-adapter-kit/runtimeacp"
	"github.com/hecatehq/claude-code-acp-adapter/claudecodeadapter"
)

const testClaudeSessionID = "550e8400-e29b-41d4-a716-446655440000"

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func TestInfoPinsClaudeCapabilities(t *testing.T) {
	info := claudecodeadapter.Info("1.2.3")

	if info.Name != claudecodeadapter.Name || info.Title != claudecodeadapter.Title || info.Version != "1.2.3" {
		t.Fatalf("info = %#v, want Claude Code adapter metadata", info)
	}
	if !info.Capabilities.Images || !info.Capabilities.EmbeddedContext || !info.Capabilities.MCPHTTP || !info.Capabilities.MCPSSE || !info.Capabilities.LoadSession {
		t.Fatalf("capabilities = %#v, want image + embedded context + MCP HTTP/SSE + load session", info.Capabilities)
	}
	if claudecodeadapter.NewServer("1.2.3") == nil {
		t.Fatal("NewServer returned nil")
	}
	if len(claudecodeadapter.Options()) == 0 {
		t.Fatal("Options returned no ACP handlers")
	}
}

func TestInitializeAdvertisesLoadSession(t *testing.T) {
	adaptertest.AssertInitializeContract(t, claudecodeadapter.NewServer("test"), adaptertest.InitializeContract{
		Name:            claudecodeadapter.Name,
		Title:           claudecodeadapter.Title,
		Version:         "test",
		Images:          true,
		EmbeddedContext: true,
		MCPHTTP:         true,
		MCPSSE:          true,
		LoadSession:     true,
		Logout:          true,
		AuthMethodIDs:   []string{"agent-login"},
	})
}

func TestNewServerExposesHecateControls(t *testing.T) {
	adaptertest.AssertSessionBootstrapContract(t, claudecodeadapter.NewServer("test"), adaptertest.SessionBootstrapContract{
		CWD: t.TempDir(),
		ConfigOptions: []adaptertest.ConfigOptionContract{
			{ID: "model", Category: "model", CurrentValue: "__default__"},
			{ID: "effort", Category: "thought_level", CurrentValue: "__default__"},
			{ID: "permission_mode", Category: "permission", CurrentValue: "dontAsk"},
		},
		AvailableCommands: []string{"init", "review", "code-review", "security-review", "compact", "debug", "run", "verify"},
	})
}

func TestNewServerCreatesUUIDSessionID(t *testing.T) {
	client := acptest.NewClient(t, claudecodeadapter.NewServer("test"))
	responses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "session/new",
		"params":  map[string]any{"cwd": t.TempDir()},
	})
	if len(responses) != 2 {
		t.Fatalf("responses = %#v, want available command update + session response", responses)
	}
	created := responses[1]

	var session struct {
		SessionID string `json:"sessionId"`
	}
	created.ResultInto(t, &session)
	if !uuidPattern.MatchString(session.SessionID) {
		t.Fatalf("session id = %q, want UUID", session.SessionID)
	}
}

func TestNewServerLoadsKnownClaudeSessionIDAfterRestart(t *testing.T) {
	client := acptest.NewClient(t, claudecodeadapter.NewServer("test"))

	responses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "session/load",
		"params": map[string]any{
			"sessionId": testClaudeSessionID,
			"cwd":       t.TempDir(),
		},
	})
	if len(responses) != 2 {
		t.Fatalf("responses = %#v, want available command update + load response", responses)
	}
	loaded := responses[1]
	var loadResult struct {
		ConfigOptions []struct {
			ID           string `json:"id"`
			CurrentValue string `json:"currentValue"`
		} `json:"configOptions"`
	}
	loaded.ResultInto(t, &loadResult)
	if len(loadResult.ConfigOptions) != 3 {
		t.Fatalf("config options = %#v, want Claude selectors for adopted session", loadResult.ConfigOptions)
	}

	listed := client.Request("session/list", map[string]any{})
	var listResult struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
		} `json:"sessions"`
	}
	listed.ResultInto(t, &listResult)
	if len(listResult.Sessions) != 1 || listResult.Sessions[0].SessionID != testClaudeSessionID {
		t.Fatalf("listed sessions = %#v, want adopted Claude session id", listResult.Sessions)
	}
}

func TestNewServerMatchesPortableUpstreamParity(t *testing.T) {
	adaptertest.AssertUpstreamParityContract(t, claudecodeadapter.NewServer("test"), adaptertest.UpstreamParityContract{
		CWD:          t.TempDir(),
		AuthMethodID: "agent-login",
		ConfigChange: adaptertest.ConfigChangeContract{
			ID:    "model",
			Value: "sonnet",
		},
		LoadUnknownSession: adaptertest.LoadUnknownSessionContract{
			SessionID: "550e8400-e29b-41d4-a716-446655440001",
			CWD:       t.TempDir(),
			Allowed:   true,
		},
	})
}

func TestNewServerPublishesAvailableCommands(t *testing.T) {
	client := acptest.NewClient(t, claudecodeadapter.NewServer("test"))
	client.Request("initialize", map[string]any{})
	responses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "session/new",
		"params":  map[string]any{"cwd": t.TempDir()},
	})
	if len(responses) != 2 {
		t.Fatalf("responses = %#v, want available command update + session response", responses)
	}
	update := decodeSessionUpdate(t, responses[0])
	if update.Update.SessionUpdate != "available_commands_update" ||
		len(update.Update.AvailableCommands) != 8 ||
		update.Update.AvailableCommands[0].Name != "init" ||
		update.Update.AvailableCommands[0].Input.Unstructured.Hint != "optional instruction focus" ||
		update.Update.AvailableCommands[1].Name != "review" ||
		update.Update.AvailableCommands[2].Name != "code-review" ||
		update.Update.AvailableCommands[3].Name != "security-review" ||
		update.Update.AvailableCommands[4].Name != "compact" ||
		update.Update.AvailableCommands[5].Name != "debug" ||
		update.Update.AvailableCommands[6].Name != "run" ||
		update.Update.AvailableCommands[7].Name != "verify" {
		t.Fatalf("available commands = %#v, want Claude command set", update)
	}
}

func TestNewCLISpecExposesLibraryContract(t *testing.T) {
	spec := claudecodeadapter.NewCLISpec("2.0.0", nil, nil, nil)

	if spec.Info.Name != claudecodeadapter.Name || spec.Info.Version != "2.0.0" {
		t.Fatalf("spec.Info = %#v", spec.Info)
	}
	if spec.Command == nil ||
		spec.Command.BuildPrompt == nil ||
		spec.Command.BuildAuthenticate == nil ||
		spec.Command.BuildLogout == nil ||
		spec.Command.NewStreamParser == nil ||
		spec.Command.NewID == nil ||
		!spec.Command.LoadUnknownSessions ||
		len(spec.Command.AuthMethods) != 1 ||
		len(spec.Command.Options) != 3 ||
		len(spec.Command.Commands) != 8 ||
		!spec.Command.IncludeTranscript {
		t.Fatalf("command spec = %#v, want command-backed bridge with config options and commands", spec.Command)
	}
	if spec.Command.AuthMethods[0].ID != "agent-login" || spec.Command.AuthMethods[0].Name != "Claude Code login" {
		t.Fatalf("auth methods = %#v, want Claude Code login", spec.Command.AuthMethods)
	}
	if spec.Command.Commands[0].Name != "init" || spec.Command.Commands[0].InputHint == "" ||
		spec.Command.Commands[1].Name != "review" ||
		spec.Command.Commands[2].Name != "code-review" ||
		spec.Command.Commands[3].Name != "security-review" ||
		spec.Command.Commands[4].Name != "compact" ||
		spec.Command.Commands[5].Name != "debug" ||
		spec.Command.Commands[6].Name != "run" ||
		spec.Command.Commands[7].Name != "verify" {
		t.Fatalf("commands = %#v, want Claude command set with input hints", spec.Command.Commands)
	}
	if id := spec.Command.NewID(); !uuidPattern.MatchString(id) {
		t.Fatalf("generated session id = %q, want UUID", id)
	}
	if spec.Doctor == nil || spec.Doctor.Binary != "claude" {
		t.Fatalf("doctor spec = %#v, want claude doctor", spec.Doctor)
	}
	wantEnv := []string{"PATH", "HOME", "USER", "LOGNAME", "XDG_CONFIG_HOME", "TMPDIR", "ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "CLAUDE_CONFIG_DIR"}
	if !reflect.DeepEqual(spec.Runtime.InheritEnv, wantEnv) || !reflect.DeepEqual(claudecodeadapter.RuntimeEnv(), wantEnv) {
		t.Fatalf("runtime env = %#v / %#v, want %#v", spec.Runtime.InheritEnv, claudecodeadapter.RuntimeEnv(), wantEnv)
	}
}

func TestPromptCommandUsesNativeClaudeCLIOnly(t *testing.T) {
	got, err := claudecodeadapter.PromptCommand(commandbridge.Session{
		ID:  testClaudeSessionID,
		CWD: "/work",
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello claude"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	assertNoPackageRunnerCommand(t, got.Command)
	if got.Command != "claude" {
		t.Fatalf("process command = %q, want native claude CLI", got.Command)
	}
	if !reflect.DeepEqual(got.Env.Inherit, claudecodeadapter.RuntimeEnv()) {
		t.Fatalf("process env = %#v, want runtime env allowlist", got.Env)
	}

	spec := claudecodeadapter.NewCLISpec("2.0.0", nil, nil, nil)
	if spec.Doctor == nil {
		t.Fatal("doctor spec is nil")
	}
	assertNoPackageRunnerCommand(t, spec.Doctor.Binary)
	if spec.Doctor.Binary != "claude" {
		t.Fatalf("doctor binary = %q, want native claude CLI", spec.Doctor.Binary)
	}
}

func TestLogoutCommandUsesNativeClaudeCLIOnly(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	got, err := claudecodeadapter.LogoutCommand()
	if err != nil {
		t.Fatalf("LogoutCommand: %v", err)
	}
	assertNoPackageRunnerCommand(t, got.Command)
	if got.Command != "claude" || got.Dir != cwd || !reflect.DeepEqual(got.Args, []string{"auth", "logout"}) {
		t.Fatalf("process spec = %#v, want claude auth logout", got)
	}
	if !reflect.DeepEqual(got.Env.Inherit, claudecodeadapter.RuntimeEnv()) {
		t.Fatalf("process env = %#v, want runtime env allowlist", got.Env)
	}
}

func TestAuthenticateCommandUsesNativeClaudeCLIOnly(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	got, err := claudecodeadapter.AuthenticateCommand("agent-login")
	if err != nil {
		t.Fatalf("AuthenticateCommand: %v", err)
	}
	assertNoPackageRunnerCommand(t, got.Command)
	if got.Command != "claude" || got.Dir != cwd || !reflect.DeepEqual(got.Args, []string{"/login"}) {
		t.Fatalf("process spec = %#v, want claude /login", got)
	}
	if !reflect.DeepEqual(got.Env.Inherit, claudecodeadapter.RuntimeEnv()) {
		t.Fatalf("process env = %#v, want runtime env allowlist", got.Env)
	}
	if _, err := claudecodeadapter.AuthenticateCommand("browser-login"); err == nil || !strings.Contains(err.Error(), "unsupported auth method") {
		t.Fatalf("AuthenticateCommand unsupported error = %v, want unsupported auth method", err)
	}
}

func TestPromptCommandBuildsClaudePrint(t *testing.T) {
	got, err := claudecodeadapter.PromptCommand(commandbridge.Session{
		ID:                    testClaudeSessionID,
		CWD:                   "/work",
		AdditionalDirectories: []string{"/extra", ""},
		Config: map[string]string{
			"model":           "sonnet",
			"effort":          "high",
			"permission_mode": "plan",
		},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello claude"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	wantArgs := []string{
		"--print",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--session-id", testClaudeSessionID,
		"--permission-mode", "plan",
		"--add-dir", "/extra",
		"--model", "sonnet",
		"--effort", "high",
		"hello claude",
	}
	if got.Command != "claude" || got.Dir != "/work" || !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("process spec = %#v, want claude args %#v", got, wantArgs)
	}
}

func TestConfigOptionsExposeClaudeBypassPermissionsMode(t *testing.T) {
	options := claudecodeadapter.ConfigOptions()
	for _, option := range options {
		if option.ID != "permission_mode" {
			continue
		}
		for _, value := range option.Options {
			if value.Value == "bypassPermissions" {
				return
			}
		}
		t.Fatalf("permission_mode values = %#v, want bypassPermissions", option.Options)
	}
	t.Fatal("permission_mode config option not found")
}

func TestPromptCommandBuildsClaudeInitAsPrint(t *testing.T) {
	got, err := claudecodeadapter.PromptCommand(commandbridge.Session{
		ID:  testClaudeSessionID,
		CWD: "/work",
		Config: map[string]string{
			"model":           "sonnet",
			"effort":          "high",
			"permission_mode": "plan",
		},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "/init focus on repo guidance"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	wantArgs := []string{
		"--print",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--session-id", testClaudeSessionID,
		"--permission-mode", "plan",
		"--model", "sonnet",
		"--effort", "high",
		"/init focus on repo guidance",
	}
	if got.Command != "claude" || got.Dir != "/work" || !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("process spec = %#v, want claude args %#v", got, wantArgs)
	}
}

func TestPromptCommandBuildsClaudeReviewCommandAsPrint(t *testing.T) {
	got, err := claudecodeadapter.PromptCommand(commandbridge.Session{
		ID:  testClaudeSessionID,
		CWD: "/work",
		Config: map[string]string{
			"permission_mode": "dontAsk",
		},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "/security-review focus on auth changes"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	wantArgs := []string{
		"--print",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--session-id", testClaudeSessionID,
		"--permission-mode", "dontAsk",
		"/security-review focus on auth changes",
	}
	if got.Command != "claude" || got.Dir != "/work" || !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("process spec = %#v, want claude args %#v", got, wantArgs)
	}
}

func TestPromptCommandBuildsClaudeBypassPermissionsMode(t *testing.T) {
	got, err := claudecodeadapter.PromptCommand(commandbridge.Session{
		ID:  testClaudeSessionID,
		CWD: "/work",
		Config: map[string]string{
			"permission_mode": "bypassPermissions",
		},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "use full access"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	wantArgs := []string{
		"--print",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--session-id", testClaudeSessionID,
		"--permission-mode", "bypassPermissions",
		"use full access",
	}
	if got.Command != "claude" || got.Dir != "/work" || !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("process spec = %#v, want claude args %#v", got, wantArgs)
	}
}

func TestPromptCommandBuildsClaudePrintWithMCPServers(t *testing.T) {
	got, err := claudecodeadapter.PromptCommand(commandbridge.Session{
		ID:  testClaudeSessionID,
		CWD: "/work",
		MCPServers: []runtimeacp.MCPServer{
			{
				Name:    "filesystem",
				Command: "uvx",
				Args:    []string{"mcp-server-filesystem", "/work"},
				Env: []runtimeacp.EnvVariable{{
					Name:  "MCP_TOKEN",
					Value: "secret",
				}},
			},
			{
				Type: "http",
				Name: "docs",
				URL:  "https://docs.example/mcp",
				Headers: []runtimeacp.HTTPHeader{{
					Name:  "Authorization",
					Value: "Bearer token",
				}},
			},
		},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello claude"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	idx := indexOfArg(got.Args, "--mcp-config")
	if idx < 1 || got.Args[idx-1] != "--strict-mcp-config" {
		t.Fatalf("args = %#v, want strict mcp config before --mcp-config", got.Args)
	}
	if got.Args[len(got.Args)-1] != "hello claude" {
		t.Fatalf("last arg = %q, want prompt", got.Args[len(got.Args)-1])
	}
	var config struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(got.Args[idx+1]), &config); err != nil {
		t.Fatalf("decode mcp config %q: %v", got.Args[idx+1], err)
	}
	if fs := config.MCPServers["filesystem"]; fs.Command != "uvx" || len(fs.Args) != 2 || fs.Args[0] != "mcp-server-filesystem" || fs.Env["MCP_TOKEN"] != "secret" {
		t.Fatalf("filesystem MCP config = %#v, want stdio server", fs)
	}
	if docs := config.MCPServers["docs"]; docs.Type != "http" || docs.URL != "https://docs.example/mcp" || docs.Headers["Authorization"] != "Bearer token" {
		t.Fatalf("docs MCP config = %#v, want HTTP server", docs)
	}
}

func TestPromptCommandRequiresNativeSessionID(t *testing.T) {
	_, err := claudecodeadapter.PromptCommand(commandbridge.Session{
		ID:  "session-1",
		CWD: "/work",
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "session id must be a UUID") {
		t.Fatalf("PromptCommand error = %v, want UUID session id required", err)
	}
}

func TestPromptCommandRejectsUnsupportedMCPServer(t *testing.T) {
	_, err := claudecodeadapter.PromptCommand(commandbridge.Session{
		ID:  testClaudeSessionID,
		CWD: "/work",
		MCPServers: []runtimeacp.MCPServer{{
			Type: "acp",
			ID:   "hosted",
			Name: "Hosted",
		}},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello claude"}},
	})
	if err == nil || !strings.Contains(err.Error(), `mcp server "Hosted": command or url is required`) {
		t.Fatalf("PromptCommand error = %v, want unsupported MCP server", err)
	}
}

func TestNewServerRunsLogoutCommand(t *testing.T) {
	installFakeCommand(t, "claude", `
if [ "$1" != "auth" ] || [ "$2" != "logout" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
printf 'logged out\n'
`)
	client := acptest.NewClient(t, claudecodeadapter.NewServer("test"))

	resp := client.Request("logout", map[string]any{})
	var result map[string]any
	resp.ResultInto(t, &result)
	if len(result) != 0 {
		t.Fatalf("logout result = %#v, want empty object", result)
	}
}

func TestNewServerRunsAuthenticateCommand(t *testing.T) {
	installFakeCommand(t, "claude", `
if [ "$1" != "/login" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
printf 'logged in\n'
`)
	client := acptest.NewClient(t, claudecodeadapter.NewServer("test"))

	resp := client.Request("authenticate", map[string]any{"methodId": "agent-login"})
	var result map[string]any
	resp.ResultInto(t, &result)
	if len(result) != 0 {
		t.Fatalf("authenticate result = %#v, want empty object", result)
	}
}

func TestNewServerMapsPromptAuthFailure(t *testing.T) {
	installFakeCommand(t, "claude", `
if [ "$1" != "--print" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
echo "Authentication required. Please run claude /login." >&2
exit 1
`)
	client := acptest.NewClient(t, claudecodeadapter.NewServer("test"))
	client.Request("initialize", map[string]any{})
	createdResponses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "session/new",
		"params":  map[string]any{"cwd": t.TempDir()},
	})
	if len(createdResponses) != 2 {
		t.Fatalf("create responses = %#v, want available commands + session response", createdResponses)
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	createdResponses[1].ResultInto(t, &session)

	responses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": session.SessionID,
			"prompt":    []map[string]any{{"type": "text", "text": "hello"}},
		},
	})
	if len(responses) != 3 {
		t.Fatalf("responses = %#v, want tool start + tool finish + auth error", responses)
	}
	if responses[2].Error == nil || responses[2].Error.Code != -32000 || responses[2].Error.Message != "Authentication required" {
		t.Fatalf("prompt error = %#v, want auth required", responses[2].Error)
	}
	raw, _ := json.Marshal(responses[2].Error.Data)
	if !strings.Contains(string(raw), "claude /login") {
		t.Fatalf("auth error data = %s, want login hint", raw)
	}
}

func TestNewServerStreamsNativeClaudeOutput(t *testing.T) {
	installFakeCommand(t, "claude", `
if [ "$1" != "--print" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
printf '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"go test ./..."}}]}}\n'
sleep 0.05
printf '{"type":"assistant","message":{"content":[{"type":"thinking","id":"thought-1","thinking":"checking"},{"type":"text","text":"chunk one chunk two"}]}}\n'
printf '{"type":"result","usage":{"input_tokens":10,"output_tokens":5,"context_window":100}}\n'
printf '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}\n'
`)
	client := acptest.NewClient(t, claudecodeadapter.NewServer("test"))
	client.Request("initialize", map[string]any{})
	createdResponses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "session/new",
		"params":  map[string]any{"cwd": t.TempDir()},
	})
	if len(createdResponses) != 2 {
		t.Fatalf("create responses = %#v, want available command update + session response", createdResponses)
	}
	created := createdResponses[1]
	var session struct {
		SessionID string `json:"sessionId"`
	}
	created.ResultInto(t, &session)
	if session.SessionID == "" {
		t.Fatal("session id is empty")
	}

	responses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": session.SessionID,
			"prompt":    []map[string]any{{"type": "text", "text": "hello"}},
		},
	})
	if len(responses) < 4 {
		t.Fatalf("got %d responses, want tool start + streamed update(s) + tool finish + prompt response: %#v", len(responses), responses)
	}
	start := decodeSessionUpdate(t, responses[0])
	if start.Update.SessionUpdate != "tool_call" ||
		start.Update.Status != "in_progress" ||
		start.Update.ToolCallID == "" ||
		start.Update.Title != "Run claude" ||
		start.Update.RawInput["command"] == "" {
		t.Fatalf("tool start = %#v, want native Claude command metadata", start)
	}
	innerStart := decodeSessionUpdate(t, responses[1])
	if innerStart.Update.SessionUpdate != "tool_call" ||
		innerStart.Update.ToolCallID != "tool-1" ||
		innerStart.Update.Kind != "execute" ||
		innerStart.Update.Status != "in_progress" {
		t.Fatalf("inner tool start = %#v, want parsed Claude tool start", innerStart)
	}
	thought := decodeSessionUpdate(t, responses[2])
	if thought.Update.SessionUpdate != "agent_thought_chunk" || decodeChunkText(t, thought.Update.Content) != "checking" {
		t.Fatalf("thought = %#v, want parsed thinking chunk", thought)
	}
	message := decodeSessionUpdate(t, responses[3])
	if message.Update.SessionUpdate != "agent_message_chunk" || decodeChunkText(t, message.Update.Content) != "chunk one chunk two" {
		t.Fatalf("message = %#v, want parsed Claude answer", message)
	}
	usage := decodeSessionUpdate(t, responses[4])
	if usage.Update.SessionUpdate != "usage_update" || usage.Update.Used != 15 || usage.Update.Size != 100 {
		t.Fatalf("usage = %#v, want parsed Claude usage", usage)
	}
	innerFinish := decodeSessionUpdate(t, responses[5])
	if innerFinish.Update.SessionUpdate != "tool_call_update" ||
		innerFinish.Update.ToolCallID != "tool-1" ||
		innerFinish.Update.Status != "completed" ||
		!strings.Contains(string(innerFinish.Update.Content), "ok") {
		t.Fatalf("inner tool finish = %#v, want parsed Claude tool finish", innerFinish)
	}
	finish := decodeSessionUpdate(t, responses[len(responses)-3])
	if finish.Update.SessionUpdate != "tool_call_update" ||
		finish.Update.ToolCallID != start.Update.ToolCallID ||
		finish.Update.Status != "completed" ||
		len(finish.Update.Content) != 0 {
		t.Fatalf("tool finish = %#v, want completed native Claude command", finish)
	}
	info := decodeSessionUpdate(t, responses[len(responses)-2])
	if info.Update.SessionUpdate != "session_info_update" ||
		info.Update.Title != "hello" ||
		info.Update.UpdatedAt == "" {
		t.Fatalf("session info = %#v, want transcript metadata", info)
	}
	var promptResult struct {
		StopReason string `json:"stopReason"`
	}
	responses[len(responses)-1].ResultInto(t, &promptResult)
	if promptResult.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn", promptResult.StopReason)
	}
}

func TestNewServerRequestsPermissionFromClaudeStream(t *testing.T) {
	installFakeCommand(t, "claude", `
if [ "$1" != "--print" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
printf '{"type":"permission_request","toolUse":{"toolUseId":"tool-1","name":"Bash","input":{"command":"go test ./..."}},"options":[{"optionId":"allow","name":"Allow","kind":"allow_once"},{"optionId":"reject","name":"Reject","kind":"reject_once"}]}\n'
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"allowed"}]}}\n'
`)
	client := acptest.NewClient(t, claudecodeadapter.NewServer("test"))
	client.Request("initialize", map[string]any{})
	createdResponses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "session/new",
		"params":  map[string]any{"cwd": t.TempDir()},
	})
	var session struct {
		SessionID string `json:"sessionId"`
	}
	createdResponses[1].ResultInto(t, &session)

	responses := client.SendRaw(strings.Join([]string{
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"` + session.SessionID + `","prompt":[{"type":"text","text":"hello"}]}}`,
		`{"jsonrpc":"2.0","id":"server-1","result":{"outcome":{"outcome":"selected","optionId":"allow"}}}`,
	}, "\n") + "\n")
	if len(responses) != 6 {
		t.Fatalf("responses = %#v, want tool start + permission + answer + tool finish + session info + prompt result", responses)
	}
	permission := decodePermissionRequest(t, responses[1])
	if permission.SessionID != session.SessionID ||
		permission.ToolCall.ToolCallID != "tool-1" ||
		permission.ToolCall.Title != "Bash" ||
		permission.ToolCall.Kind != "execute" ||
		permission.ToolCall.Status != "pending" ||
		permission.ToolCall.RawInput["command"] != "go test ./..." ||
		len(permission.Options) != 2 ||
		permission.Options[0].OptionID != "allow" ||
		permission.Options[1].OptionID != "reject" {
		t.Fatalf("permission = %#v, want Claude stream permission request", permission)
	}
	answer := decodeSessionUpdate(t, responses[2])
	if answer.Update.SessionUpdate != "agent_message_chunk" || decodeChunkText(t, answer.Update.Content) != "allowed" {
		t.Fatalf("answer = %#v, want stream continuation after approval", answer)
	}
	info := decodeSessionUpdate(t, responses[4])
	if info.Update.SessionUpdate != "session_info_update" ||
		info.Update.Title != "hello" ||
		info.Update.UpdatedAt == "" {
		t.Fatalf("session info = %#v, want transcript metadata", info)
	}
	var promptResult struct {
		StopReason string `json:"stopReason"`
	}
	responses[5].ResultInto(t, &promptResult)
	if promptResult.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn", promptResult.StopReason)
	}
}

func TestPromptCommandRequiresWorkspace(t *testing.T) {
	_, err := claudecodeadapter.PromptCommand(commandbridge.Session{}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "session cwd is required") {
		t.Fatalf("PromptCommand error = %v, want cwd required", err)
	}
}

func TestClaudeStreamParserMapsJSONL(t *testing.T) {
	parser := claudecodeadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})
	chunks := []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool-1","name":"Edit","input":{"file_path":"main.go"}}]}}` + "\n",
		`{"type":"assistant","message":{"content":[{"type":"thinking","id":"think-1","thinking":"planning"},{"type":"text","text":"done"}]}}` + "\n",
		`{"type":"result","result":"done","usage":{"input_tokens":2,"cache_creation_input_tokens":3,"cache_read_input_tokens":5,"output_tokens":7,"thinking_tokens":11,"context_window":1000}}` + "\n",
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"patched","is_error":false}]}}` + "\n",
	}
	var events []commandbridge.StreamEvent
	for _, chunk := range chunks {
		parsed, err := parser.Parse([]byte(chunk))
		if err != nil {
			t.Fatalf("Parse returned error: %v", err)
		}
		events = append(events, parsed...)
	}
	flushed, err := parser.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	events = append(events, flushed...)
	if len(events) != 5 {
		t.Fatalf("events len = %d, want 5: %#v", len(events), events)
	}
	if events[0].Update["sessionUpdate"] != "tool_call" ||
		events[0].Update["toolCallId"] != "tool-1" ||
		events[0].Update["kind"] != "edit" {
		t.Fatalf("tool start = %#v, want edit tool start", events[0].Update)
	}
	if events[1].Update["sessionUpdate"] != "agent_thought_chunk" {
		t.Fatalf("thought = %#v, want thinking chunk", events[1].Update)
	}
	if events[2].Update["sessionUpdate"] != "agent_message_chunk" || parser.Transcript() != "done" {
		t.Fatalf("message = %#v transcript=%q, want answer transcript", events[2].Update, parser.Transcript())
	}
	if events[3].Update["sessionUpdate"] != "usage_update" ||
		events[3].Update["used"] != 28 ||
		events[3].Update["size"] != 1000 {
		t.Fatalf("usage = %#v, want summed usage", events[3].Update)
	}
	if events[4].Update["sessionUpdate"] != "tool_call_update" ||
		events[4].Update["toolCallId"] != "tool-1" ||
		events[4].Update["status"] != "completed" {
		t.Fatalf("tool finish = %#v, want completed tool", events[4].Update)
	}
}

func TestClaudeStreamParserMapsPermissionRequest(t *testing.T) {
	parser := claudecodeadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})

	events, err := parser.Parse([]byte(`{"type":"permission_request","toolUse":{"toolUseId":"tool-1","name":"Bash","input":{"command":"go test ./..."}},"options":[{"optionId":"allow","name":"Allow","kind":"allow_once"},{"optionId":"reject","name":"Reject","kind":"reject_once"}]}` + "\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want permission request: %#v", len(events), events)
	}
	req := events[0].PermissionRequest
	if req == nil {
		t.Fatalf("event = %#v, want permission request", events[0])
	}
	rawInput, _ := req.RawInput.(map[string]any)
	if req.ToolCallID != "tool-1" ||
		req.Title != "Bash" ||
		req.Kind != "execute" ||
		rawInput["command"] != "go test ./..." {
		t.Fatalf("permission request = %#v, want Claude tool permission", req)
	}
	if len(req.Options) != 2 ||
		req.Options[0].OptionID != "allow" ||
		req.Options[0].Kind != "allow_once" ||
		req.Options[1].OptionID != "reject" ||
		req.Options[1].Kind != "reject_once" {
		t.Fatalf("permission options = %#v, want allow/reject", req.Options)
	}
}

func TestClaudeStreamParserMapsTerminalStopReason(t *testing.T) {
	parser := claudecodeadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})

	events, err := parser.Parse([]byte(`{"type":"result","subtype":"error_max_turns","result":"partial answer","usage":{"input_tokens":4,"output_tokens":6,"context_window":128}}` + "\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want message + usage: %#v", len(events), events)
	}
	if events[0].Update["sessionUpdate"] != "agent_message_chunk" || parser.Transcript() != "partial answer" {
		t.Fatalf("message = %#v transcript=%q, want result text", events[0].Update, parser.Transcript())
	}
	if events[1].Update["sessionUpdate"] != "usage_update" ||
		events[1].Update["used"] != 10 ||
		events[1].Update["size"] != 128 {
		t.Fatalf("usage = %#v, want terminal usage", events[1].Update)
	}
	if got := parser.StopReason(); got != runtimeacp.StopReasonMaxTurnRequests {
		t.Fatalf("StopReason() = %q, want max_turn_requests", got)
	}
}

func TestClaudeStreamParserMapsSourceShapedFixtures(t *testing.T) {
	parser := claudecodeadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})
	fixture := strings.Join([]string{
		`{"type":"permission_request","toolUse":{"toolUseId":"tool-1","name":"Bash","input":{"command":"go test ./..."}},"options":[{"optionId":"allow-session","name":"Allow for session","kind":"allow_always"},{"optionId":"deny-once","name":"Deny once","kind":"reject_once"}]}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","id":"think-1","thinking":"checking tests"},{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"go test ./..."}},{"type":"text","text":"done"}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`,
		`{"type":"result","subtype":"success","usage":{"input_tokens":"4","cache_read_input_tokens":2,"output_tokens":6,"context_window_tokens":128}}`,
		"",
	}, "\n")

	events, err := parser.Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 6 {
		t.Fatalf("events len = %d, want 6: %#v", len(events), events)
	}
	req := events[0].PermissionRequest
	if req == nil {
		t.Fatalf("event = %#v, want permission request", events[0])
	}
	rawInput, _ := req.RawInput.(map[string]any)
	if req.ToolCallID != "tool-1" ||
		req.Title != "Bash" ||
		req.Kind != "execute" ||
		rawInput["command"] != "go test ./..." ||
		len(req.Options) != 2 ||
		req.Options[0].OptionID != "allow-session" ||
		req.Options[0].Kind != "allow_always" ||
		req.Options[1].OptionID != "deny-once" ||
		req.Options[1].Kind != "reject_once" {
		t.Fatalf("permission request = %#v, rawInput=%#v, want source-shaped Bash permission", req, rawInput)
	}
	if events[1].Update["sessionUpdate"] != "agent_thought_chunk" ||
		updateText(events[1].Update) != "checking tests" {
		t.Fatalf("thought = %#v, want thinking text", events[1].Update)
	}
	if events[2].Update["sessionUpdate"] != "tool_call" ||
		events[2].Update["toolCallId"] != "tool-1" ||
		events[2].Update["kind"] != "execute" {
		t.Fatalf("tool start = %#v, want Bash tool start", events[2].Update)
	}
	if events[3].Update["sessionUpdate"] != "agent_message_chunk" ||
		parser.Transcript() != "done" {
		t.Fatalf("message = %#v transcript=%q, want answer transcript", events[3].Update, parser.Transcript())
	}
	if events[4].Update["sessionUpdate"] != "tool_call_update" ||
		events[4].Update["status"] != "completed" ||
		events[4].Update["rawOutput"] != "ok" {
		t.Fatalf("tool finish = %#v, want completed tool output", events[4].Update)
	}
	if events[5].Update["sessionUpdate"] != "usage_update" ||
		events[5].Update["used"] != 12 ||
		events[5].Update["size"] != 128 {
		t.Fatalf("usage = %#v, want source-shaped usage", events[5].Update)
	}
	if got := parser.StopReason(); got != runtimeacp.StopReasonEndTurn {
		t.Fatalf("StopReason() = %q, want end_turn", got)
	}
}

func TestClaudeStreamParserClassifiesProviderTools(t *testing.T) {
	parser := claudecodeadapter.NewStreamParser(commandbridge.Session{}, runtimeacp.PromptParams{})
	events, err := parser.Parse([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"web-1","name":"WebSearch","input":{"query":"acp"}},{"type":"tool_use","id":"task-1","name":"TaskCreate","input":{"description":"review"}},{"type":"tool_use","id":"memory-1","name":"MemoryRecall","input":{"query":"project"}}]}}` + "\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events len = %d, want 3: %#v", len(events), events)
	}
	wants := map[string]string{
		"web-1":    "fetch",
		"task-1":   "task",
		"memory-1": "memory",
	}
	for _, event := range events {
		id, _ := event.Update["toolCallId"].(string)
		if got := event.Update["kind"]; got != wants[id] {
			t.Fatalf("tool %s kind = %v, want %s", id, got, wants[id])
		}
	}
}

type sessionUpdate struct {
	Update struct {
		SessionUpdate     string          `json:"sessionUpdate"`
		ToolCallID        string          `json:"toolCallId"`
		Title             string          `json:"title"`
		Kind              string          `json:"kind"`
		Status            string          `json:"status"`
		RawInput          map[string]any  `json:"rawInput"`
		Used              int             `json:"used"`
		Size              int             `json:"size"`
		Content           json.RawMessage `json:"content"`
		UpdatedAt         string          `json:"updatedAt"`
		AvailableCommands []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Input       struct {
				Unstructured struct {
					Hint string `json:"hint"`
				} `json:"unstructured"`
			} `json:"input"`
		} `json:"availableCommands"`
	} `json:"update"`
}

type permissionRequest struct {
	SessionID string `json:"sessionId"`
	ToolCall  struct {
		ToolCallID string         `json:"toolCallId"`
		Title      string         `json:"title"`
		Kind       string         `json:"kind"`
		Status     string         `json:"status"`
		RawInput   map[string]any `json:"rawInput"`
	} `json:"toolCall"`
	Options []struct {
		OptionID string `json:"optionId"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
	} `json:"options"`
}

func decodeSessionUpdate(t testing.TB, response acptest.Response) sessionUpdate {
	t.Helper()
	if response.Method != "session/update" {
		t.Fatalf("response method = %q, want session/update", response.Method)
	}
	var update sessionUpdate
	response.ParamsInto(t, &update)
	return update
}

func decodePermissionRequest(t testing.TB, response acptest.Response) permissionRequest {
	t.Helper()
	if response.Method != "session/request_permission" {
		t.Fatalf("response method = %q, want session/request_permission", response.Method)
	}
	var req permissionRequest
	response.ParamsInto(t, &req)
	return req
}

func decodeChunkText(t testing.TB, raw json.RawMessage) string {
	t.Helper()
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatalf("decode chunk content: %v\n%s", err, string(raw))
	}
	return content.Text
}

func updateText(update map[string]any) string {
	content, _ := update["content"].(map[string]any)
	text, _ := content["text"].(string)
	return text
}

func installFakeCommand(t testing.TB, name string, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake command is Unix-only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatalf("write fake %s command: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func assertNoPackageRunnerCommand(t testing.TB, command string) {
	t.Helper()
	switch command {
	case "npx", "npm", "node", "bun", "sh", "bash", "zsh", "cmd", "powershell", "pwsh":
		t.Fatalf("command = %q, want fixed native CLI without package runner or shell", command)
	}
}

func indexOfArg(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
	}
	return -1
}
