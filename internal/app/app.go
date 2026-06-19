package app

import (
	"io"

	"github.com/hecatehq/acp-adapter-kit/acp"
	"github.com/hecatehq/acp-adapter-kit/adaptercli"
	"github.com/hecatehq/acp-adapter-kit/doctor"
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
