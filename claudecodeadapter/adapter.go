// Package claudecodeadapter exposes the Claude Code-specific ACP adapter
// wiring for hosts that want to embed the adapter as a library instead of
// launching the claude-code-acp-adapter binary.
package claudecodeadapter

import (
	"fmt"
	"io"

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
		Options:           ConfigOptions(),
		IncludeTranscript: true,
		BuildPrompt:       PromptCommand,
		NewStreamParser:   NewStreamParser,
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
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
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
	args = append(args, text)
	return adapterprocess.Spec{
		Command: "claude",
		Args:    args,
		Dir:     session.CWD,
	}, nil
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
