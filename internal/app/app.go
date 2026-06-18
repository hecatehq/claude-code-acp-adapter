package app

import (
	"fmt"
	"io"

	"github.com/hecatehq/claude-code-acp-adapter/internal/acp"
)

const (
	Name    = "claude-code-acp-adapter"
	Title   = "Claude Code ACP Adapter"
	Version = "0.0.0-dev"
)

func Run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "version":
			_, _ = fmt.Fprintf(stdout, "%s %s\n", Name, Version)
			return 0
		case "--help", "-h", "help":
			_, _ = fmt.Fprintf(stdout, "%s speaks ACP over stdio.\n\nUsage:\n  %s [--version]\n", Name, Name)
			return 0
		default:
			_, _ = fmt.Fprintf(stderr, "unknown argument: %s\n", args[0])
			return 2
		}
	}

	server := acp.NewServer(acp.AdapterInfo{
		Name:    Name,
		Title:   Title,
		Version: Version,
		Capabilities: acp.Capabilities{
			Images:          true,
			EmbeddedContext: true,
			MCPHTTP:         true,
			MCPSSE:          true,
		},
	})
	if err := server.Serve(stdin, stdout); err != nil {
		_, _ = fmt.Fprintf(stderr, "adapter error: %v\n", err)
		return 1
	}
	return 0
}
