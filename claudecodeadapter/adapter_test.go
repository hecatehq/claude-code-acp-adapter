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
	client := acptest.NewClient(t, claudecodeadapter.NewServer("test"))

	resp := client.Request("initialize", map[string]any{})
	var result struct {
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
	}
	resp.ResultInto(t, &result)
	if !result.AgentCapabilities.LoadSession {
		t.Fatal("loadSession = false, want true")
	}
}

func TestNewCLISpecExposesLibraryContract(t *testing.T) {
	spec := claudecodeadapter.NewCLISpec("2.0.0", nil, nil, nil)

	if spec.Info.Name != claudecodeadapter.Name || spec.Info.Version != "2.0.0" {
		t.Fatalf("spec.Info = %#v", spec.Info)
	}
	if spec.Command == nil || spec.Command.BuildPrompt == nil || spec.Command.NewStreamParser == nil || len(spec.Command.Options) != 3 || !spec.Command.IncludeTranscript {
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

type sessionUpdate struct {
	Update struct {
		SessionUpdate string          `json:"sessionUpdate"`
		ToolCallID    string          `json:"toolCallId"`
		Title         string          `json:"title"`
		Kind          string          `json:"kind"`
		Status        string          `json:"status"`
		RawInput      map[string]any  `json:"rawInput"`
		Used          int             `json:"used"`
		Size          int             `json:"size"`
		Content       json.RawMessage `json:"content"`
		UpdatedAt     string          `json:"updatedAt"`
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
