package app

import (
	"fmt"
	"io"

	"github.com/hecatehq/claude-code-acp-adapter/internal/acp"
	"github.com/spf13/cobra"
)

const (
	Name    = "claude-code-acp-adapter"
	Title   = "Claude Code ACP Adapter"
	Version = "0.0.0-dev"
)

func Run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	cmd := newRootCommand(stdin, stdout, stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func newRootCommand(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:           Name,
		Short:         "ACP adapter for Claude Code",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("unknown argument: %s", args[0])
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
				return fmt.Errorf("adapter error: %w", err)
			}
			return nil
		},
	}
	cmd.SetIn(stdin)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	cmd.AddCommand(&cobra.Command{
		Use:           "version",
		Short:         "Print version information",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintf(stdout, "%s %s\n", Name, Version)
		},
	})
	cmd.SetVersionTemplate(fmt.Sprintf("%s %s\n", Name, Version))
	return cmd
}
