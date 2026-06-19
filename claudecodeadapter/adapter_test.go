package claudecodeadapter_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/hecatehq/acp-adapter-kit/acptest"
	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	"github.com/hecatehq/acp-adapter-kit/runtimeacp"
	"github.com/hecatehq/claude-code-acp-adapter/claudecodeadapter"
)

func TestInfoPinsClaudeCapabilities(t *testing.T) {
	info := claudecodeadapter.Info("1.2.3")

	if info.Name != claudecodeadapter.Name || info.Title != claudecodeadapter.Title || info.Version != "1.2.3" {
		t.Fatalf("info = %#v, want Claude Code adapter metadata", info)
	}
	if !info.Capabilities.Images || !info.Capabilities.EmbeddedContext || !info.Capabilities.MCPHTTP || !info.Capabilities.MCPSSE {
		t.Fatalf("capabilities = %#v, want image + embedded context + MCP HTTP/SSE", info.Capabilities)
	}
	if claudecodeadapter.NewServer("1.2.3") == nil {
		t.Fatal("NewServer returned nil")
	}
	if len(claudecodeadapter.Options()) == 0 {
		t.Fatal("Options returned no ACP handlers")
	}
}

func TestNewCLISpecExposesLibraryContract(t *testing.T) {
	spec := claudecodeadapter.NewCLISpec("2.0.0", nil, nil, nil)

	if spec.Info.Name != claudecodeadapter.Name || spec.Info.Version != "2.0.0" {
		t.Fatalf("spec.Info = %#v", spec.Info)
	}
	if spec.Command == nil || spec.Command.BuildPrompt == nil || len(spec.Command.Options) != 2 {
		t.Fatalf("command spec = %#v, want command-backed bridge with config options", spec.Command)
	}
	if spec.Doctor == nil || spec.Doctor.Binary != "claude" {
		t.Fatalf("doctor spec = %#v, want claude doctor", spec.Doctor)
	}
	wantEnv := []string{"PATH", "HOME", "XDG_CONFIG_HOME", "TMPDIR", "ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "CLAUDE_CONFIG_DIR"}
	if !reflect.DeepEqual(spec.Runtime.InheritEnv, wantEnv) || !reflect.DeepEqual(claudecodeadapter.RuntimeEnv(), wantEnv) {
		t.Fatalf("runtime env = %#v / %#v, want %#v", spec.Runtime.InheritEnv, claudecodeadapter.RuntimeEnv(), wantEnv)
	}
}

func TestPromptCommandUsesNativeClaudeCLIOnly(t *testing.T) {
	got, err := claudecodeadapter.PromptCommand(commandbridge.Session{
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

	spec := claudecodeadapter.NewCLISpec("2.0.0", nil, nil, nil)
	if spec.Doctor == nil {
		t.Fatal("doctor spec is nil")
	}
	assertNoPackageRunnerCommand(t, spec.Doctor.Binary)
	if spec.Doctor.Binary != "claude" {
		t.Fatalf("doctor binary = %q, want native claude CLI", spec.Doctor.Binary)
	}
}

func TestPromptCommandBuildsClaudePrint(t *testing.T) {
	got, err := claudecodeadapter.PromptCommand(commandbridge.Session{
		CWD:                   "/work",
		AdditionalDirectories: []string{"/extra", ""},
		Config: map[string]string{
			"model":  "sonnet",
			"effort": "high",
		},
	}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello claude"}},
	})
	if err != nil {
		t.Fatalf("PromptCommand: %v", err)
	}
	wantArgs := []string{
		"--print",
		"--output-format", "text",
		"--permission-mode", "dontAsk",
		"--add-dir", "/extra",
		"--model", "sonnet",
		"--effort", "high",
		"hello claude",
	}
	if got.Command != "claude" || got.Dir != "/work" || !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("process spec = %#v, want claude args %#v", got, wantArgs)
	}
}

func TestNewServerStreamsNativeClaudeOutput(t *testing.T) {
	installFakeCommand(t, "claude", `
if [ "$1" != "--print" ]; then
  echo "unexpected command: $*" >&2
  exit 64
fi
printf 'chunk one '
sleep 0.05
printf 'chunk two'
`)
	client := acptest.NewClient(t, claudecodeadapter.NewServer("test"))
	client.Request("initialize", map[string]any{})
	created := client.Request("session/new", map[string]any{"cwd": t.TempDir()})
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
	var streamed strings.Builder
	for i, response := range responses[1 : len(responses)-2] {
		update := decodeSessionUpdate(t, response)
		if update.Update.SessionUpdate != "agent_message_chunk" {
			t.Fatalf("response %d update = %#v, want agent_message_chunk", i, update.Update)
		}
		streamed.WriteString(decodeChunkText(t, update.Update.Content))
	}
	if streamed.String() != "chunk one chunk two" {
		t.Fatalf("streamed text = %q, want chunked command stdout", streamed.String())
	}
	finish := decodeSessionUpdate(t, responses[len(responses)-2])
	if finish.Update.SessionUpdate != "tool_call_update" ||
		finish.Update.ToolCallID != start.Update.ToolCallID ||
		finish.Update.Status != "completed" {
		t.Fatalf("tool finish = %#v, want completed native Claude command", finish)
	}
	var promptResult struct {
		StopReason string `json:"stopReason"`
	}
	responses[len(responses)-1].ResultInto(t, &promptResult)
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

type sessionUpdate struct {
	Update struct {
		SessionUpdate string          `json:"sessionUpdate"`
		ToolCallID    string          `json:"toolCallId"`
		Title         string          `json:"title"`
		Status        string          `json:"status"`
		RawInput      map[string]any  `json:"rawInput"`
		Content       json.RawMessage `json:"content"`
	} `json:"update"`
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
