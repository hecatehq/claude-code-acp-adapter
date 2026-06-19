package app

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

var Version = "0.0.0-dev"

func Run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	return adaptercli.Run(args, adapterSpec(stdin, stdout, stderr))
}

func adapterSpec(stdin io.Reader, stdout io.Writer, stderr io.Writer) adaptercli.Spec {
	return adaptercli.Spec{
		Info:   adapterInfo(),
		Short:  "ACP adapter for Claude Code",
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Runtime: adaptercli.RuntimeSpec{
			InheritEnv: []string{
				"PATH",
				"HOME",
				"XDG_CONFIG_HOME",
				"TMPDIR",
				"ANTHROPIC_API_KEY",
				"ANTHROPIC_BASE_URL",
				"CLAUDE_CONFIG_DIR",
			},
		},
		Command: &commandbridge.Spec{
			Options:     claudeConfigOptions(),
			BuildPrompt: claudePromptCommand,
		},
		Doctor: &adaptercli.DoctorSpec{
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
		},
	}
}

const configDefault = "__default__"

func claudeConfigOptions() []commandbridge.SelectConfigOption {
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
	}
}

func claudePromptCommand(session commandbridge.Session, params runtimeacp.PromptParams) (adapterprocess.Spec, error) {
	text, err := commandbridge.RequirePromptText(params)
	if err != nil {
		return adapterprocess.Spec{}, err
	}
	if session.CWD == "" {
		return adapterprocess.Spec{}, fmt.Errorf("session cwd is required")
	}
	args := []string{
		"--print",
		"--output-format", "text",
		"--permission-mode", "dontAsk",
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

func selectedConfig(session commandbridge.Session, id string) string {
	value := session.Config[id]
	if value == "" || value == configDefault {
		return ""
	}
	return value
}

func adapterInfo() acp.AdapterInfo {
	return acp.AdapterInfo{
		Name:    Name,
		Title:   Title,
		Version: Version,
		Capabilities: acp.Capabilities{
			Images:          true,
			EmbeddedContext: true,
			MCPHTTP:         true,
			MCPSSE:          true,
		},
	}
}
