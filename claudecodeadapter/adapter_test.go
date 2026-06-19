package claudecodeadapter_test

import (
	"reflect"
	"strings"
	"testing"

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

func TestPromptCommandRequiresWorkspace(t *testing.T) {
	_, err := claudecodeadapter.PromptCommand(commandbridge.Session{}, runtimeacp.PromptParams{
		Prompt: []runtimeacp.ContentBlock{{Type: "text", Text: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "session cwd is required") {
		t.Fatalf("PromptCommand error = %v, want cwd required", err)
	}
}

func assertNoPackageRunnerCommand(t testing.TB, command string) {
	t.Helper()
	switch command {
	case "npx", "npm", "node", "bun", "sh", "bash", "zsh", "cmd", "powershell", "pwsh":
		t.Fatalf("command = %q, want fixed native CLI without package runner or shell", command)
	}
}
