// Package claudecodeadapter exposes the Claude Code-specific ACP adapter
// wiring for hosts that want to embed the adapter as a library instead of
// launching the claude-code-acp-adapter binary.
package claudecodeadapter

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/hecatehq/acp-adapter-kit/acp"
	"github.com/hecatehq/acp-adapter-kit/adaptercli"
	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	"github.com/hecatehq/acp-adapter-kit/doctor"
	adapterprocess "github.com/hecatehq/acp-adapter-kit/process"
	"github.com/hecatehq/acp-adapter-kit/runtimeacp"
)

const (
	Name  = "claude-code-acp-adapter"
	Title = "Claude Code ACP Adapter"
)

const configDefault = "__default__"

var (
	claudeSessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	fallbackSessionID      atomic.Uint64
)

func NewCLISpec(version string, stdin io.Reader, stdout io.Writer, stderr io.Writer) adaptercli.Spec {
	return adaptercli.Spec{
		Info:   Info(version),
		Short:  "ACP adapter for Claude Code",
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Runtime: adaptercli.RuntimeSpec{
			InheritEnv: RuntimeEnv(),
		},
		Command: CommandSpec(),
		Doctor:  DoctorSpec(),
	}
}

func Info(version string) acp.AdapterInfo {
	return acp.AdapterInfo{
		Name:    Name,
		Title:   Title,
		Version: version,
		Capabilities: acp.Capabilities{
			Images:          true,
			EmbeddedContext: true,
			MCPHTTP:         true,
			MCPSSE:          true,
			LoadSession:     true,
		},
	}
}

func NewServer(version string) *acp.Server {
	return acp.NewServer(Info(version), Options()...)
}

func Options() []acp.Option {
	return commandbridge.New(*CommandSpec()).Options()
}

func CommandSpec() *commandbridge.Spec {
	return &commandbridge.Spec{
		NewID:               newClaudeSessionID,
		LoadUnknownSessions: true,
		Options:             ConfigOptions(),
		Commands:            AvailableCommands(),
		IncludeTranscript:   true,
		BuildPrompt:         PromptCommand,
		NewStreamParser:     NewStreamParser,
	}
}

func AvailableCommands() []commandbridge.AvailableCommand {
	return []commandbridge.AvailableCommand{
		{
			Name:        "init",
			Description: "Ask Claude Code to inspect the workspace and create or update CLAUDE.md.",
			InputHint:   "optional instruction focus",
		},
		{
			Name:        "review",
			Description: "Ask Claude Code to review a pull request locally in this session.",
			InputHint:   "optional PR or review focus",
		},
		{
			Name:        "code-review",
			Description: "Ask Claude Code to review the current diff for correctness bugs and cleanups.",
			InputHint:   "[effort] [--fix] [target]",
		},
		{
			Name:        "security-review",
			Description: "Ask Claude Code to analyze pending changes for security vulnerabilities.",
			InputHint:   "optional target or focus",
		},
		{
			Name:        "compact",
			Description: "Ask Claude Code to compact the current conversation context.",
			InputHint:   "optional focus to preserve",
		},
		{
			Name:        "debug",
			Description: "Ask Claude Code to debug a failure or unexpected behavior.",
			InputHint:   "symptom, error, or target",
		},
		{
			Name:        "run",
			Description: "Ask Claude Code to run the app and inspect the result.",
			InputHint:   "optional launch target",
		},
		{
			Name:        "verify",
			Description: "Ask Claude Code to verify that the current change works.",
			InputHint:   "optional verification focus",
		},
	}
}

func ConfigOptions() []commandbridge.SelectConfigOption {
	return []commandbridge.SelectConfigOption{
		{
			ID:           "model",
			Name:         "Model",
			Description:  "Claude Code model override. Default uses the operator's Claude configuration.",
			Category:     "model",
			DefaultValue: configDefault,
			Options: []commandbridge.SelectValue{
				{Value: configDefault, Name: "Configured default"},
				{Value: "sonnet", Name: "Sonnet"},
				{Value: "opus", Name: "Opus"},
			},
		},
		{
			ID:           "effort",
			Name:         "Effort",
			Description:  "Claude Code effort override. Default uses the operator's Claude configuration.",
			Category:     "thought_level",
			DefaultValue: configDefault,
			Options: []commandbridge.SelectValue{
				{Value: configDefault, Name: "Configured default"},
				{Value: "low", Name: "Low"},
				{Value: "medium", Name: "Medium"},
				{Value: "high", Name: "High"},
				{Value: "xhigh", Name: "xHigh"},
				{Value: "max", Name: "Max"},
			},
		},
		{
			ID:           "permission_mode",
			Name:         "Permission mode",
			Description:  "Claude Code permission mode. Default matches the adapter's non-interactive dontAsk boundary.",
			Category:     "permission",
			DefaultValue: "dontAsk",
			Options: []commandbridge.SelectValue{
				{Value: "dontAsk", Name: "Do not ask"},
				{Value: "default", Name: "Default"},
				{Value: "acceptEdits", Name: "Accept edits"},
				{Value: "auto", Name: "Auto"},
				{Value: "plan", Name: "Plan"},
				{Value: "bypassPermissions", Name: "Bypass permissions", Description: "Run with Claude Code's full permission bypass mode."},
			},
		},
	}
}

func PromptCommand(session commandbridge.Session, params runtimeacp.PromptParams) (adapterprocess.Spec, error) {
	text, err := commandbridge.RequirePromptText(params)
	if err != nil {
		return adapterprocess.Spec{}, err
	}
	if session.CWD == "" {
		return adapterprocess.Spec{}, fmt.Errorf("session cwd is required")
	}
	if !validClaudeSessionID(session.ID) {
		return adapterprocess.Spec{}, fmt.Errorf("session id must be a UUID")
	}
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--session-id", session.ID,
		"--permission-mode", selectedPermissionMode(session),
	}
	for _, dir := range session.AdditionalDirectories {
		if dir != "" {
			args = append(args, "--add-dir", dir)
		}
	}
	if model := selectedConfig(session, "model"); model != "" {
		args = append(args, "--model", model)
	}
	if effort := selectedConfig(session, "effort"); effort != "" {
		args = append(args, "--effort", effort)
	}
	if mcpConfig, ok, err := claudeMCPConfigArg(session.MCPServers); err != nil {
		return adapterprocess.Spec{}, err
	} else if ok {
		args = append(args, "--strict-mcp-config", "--mcp-config", mcpConfig)
	}
	args = append(args, text)
	return adapterprocess.Spec{
		Command: "claude",
		Args:    args,
		Dir:     session.CWD,
	}, nil
}

func newClaudeSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fallbackClaudeSessionID()
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func fallbackClaudeSessionID() string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012x", fallbackSessionID.Add(1))
}

func validClaudeSessionID(id string) bool {
	return claudeSessionIDPattern.MatchString(strings.TrimSpace(id))
}

func claudeMCPConfigArg(servers []runtimeacp.MCPServer) (string, bool, error) {
	if len(servers) == 0 {
		return "", false, nil
	}
	configs := map[string]map[string]any{}
	for _, server := range servers {
		name := strings.TrimSpace(server.Name)
		if name == "" {
			return "", false, fmt.Errorf("mcp server name is required")
		}
		entry, err := claudeMCPServerConfig(server)
		if err != nil {
			return "", false, fmt.Errorf("mcp server %q: %w", name, err)
		}
		configs[name] = entry
	}
	if len(configs) == 0 {
		return "", false, nil
	}
	raw, err := json.Marshal(map[string]any{"mcpServers": configs})
	if err != nil {
		return "", false, err
	}
	return string(raw), true, nil
}

func claudeMCPServerConfig(server runtimeacp.MCPServer) (map[string]any, error) {
	if command := strings.TrimSpace(server.Command); command != "" {
		entry := map[string]any{"command": command}
		if len(server.Args) != 0 {
			entry["args"] = append([]string(nil), server.Args...)
		}
		if env := claudeMCPEnv(server.Env); len(env) != 0 {
			entry["env"] = env
		}
		return entry, nil
	}
	if url := strings.TrimSpace(server.URL); url != "" {
		transport := strings.TrimSpace(server.Type)
		if transport == "" {
			transport = "http"
		}
		entry := map[string]any{
			"type": transport,
			"url":  url,
		}
		if headers := claudeMCPHeaders(server.Headers); len(headers) != 0 {
			entry["headers"] = headers
		}
		return entry, nil
	}
	return nil, fmt.Errorf("command or url is required")
}

func claudeMCPEnv(values []runtimeacp.EnvVariable) map[string]string {
	out := map[string]string{}
	for _, item := range values {
		name := strings.TrimSpace(item.Name)
		if name != "" {
			out[name] = item.Value
		}
	}
	return out
}

func claudeMCPHeaders(values []runtimeacp.HTTPHeader) map[string]string {
	out := map[string]string{}
	for _, item := range values {
		name := strings.TrimSpace(item.Name)
		if name != "" {
			out[name] = item.Value
		}
	}
	return out
}

func RuntimeEnv() []string {
	return []string{
		"PATH",
		"HOME",
		"XDG_CONFIG_HOME",
		"TMPDIR",
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_BASE_URL",
		"CLAUDE_CONFIG_DIR",
	}
}

func DoctorSpec() *adaptercli.DoctorSpec {
	return &adaptercli.DoctorSpec{
		Short:       "Check the local Claude Code runtime boundary",
		Binary:      "claude",
		VersionArgs: []string{"--version"},
		InheritEnv: []string{
			"PATH",
			"HOME",
			"XDG_CONFIG_HOME",
			"TMPDIR",
		},
		EnvVars: []doctor.EnvVar{
			{Name: "ANTHROPIC_API_KEY"},
			{Name: "ANTHROPIC_BASE_URL"},
			{Name: "CLAUDE_CONFIG_DIR"},
		},
	}
}

func selectedConfig(session commandbridge.Session, id string) string {
	value := session.Config[id]
	if value == "" || value == configDefault {
		return ""
	}
	return value
}

func selectedPermissionMode(session commandbridge.Session) string {
	if value := selectedConfig(session, "permission_mode"); value != "" {
		return value
	}
	return "dontAsk"
}
