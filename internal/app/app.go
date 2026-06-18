package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/hecatehq/claude-code-acp-adapter/internal/acp"
	"github.com/hecatehq/claude-code-acp-adapter/internal/doctor"
	"github.com/hecatehq/claude-code-acp-adapter/internal/runtimeacp"
	"github.com/hecatehq/claude-code-acp-adapter/internal/runtimehost"
	"github.com/hecatehq/claude-code-acp-adapter/internal/runtimeproc"
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
	var runtimeBinary string
	var runtimeWorkDir string
	var runtimeArgs []string

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
			info := adapterInfo()
			var opts []acp.Option
			if runtimeBinary != "" {
				if runtimeWorkDir == "" {
					return fmt.Errorf("--runtime-workdir is required when --runtime-binary is set")
				}
				host, err := runtimehost.Start(cmd.Context(), runtimehost.Spec{
					Launch: runtimeproc.LaunchSpec{
						Binary:  runtimeBinary,
						Args:    runtimeArgs,
						WorkDir: runtimeWorkDir,
					},
					ClientInfo: runtimeacp.ImplementationInfo{
						Name:    info.Name,
						Title:   info.Title,
						Version: info.Version,
					},
				})
				if err != nil {
					return fmt.Errorf("start runtime host: %w", err)
				}
				defer func() {
					_ = host.Close()
				}()
				opts = append([]acp.Option{acp.WithInitializeResult(host.InitializeResult())}, host.Options()...)
			}
			server := acp.NewServer(info, opts...)
			if err := server.Serve(stdin, stdout); err != nil {
				return fmt.Errorf("adapter error: %w", err)
			}
			return nil
		},
	}
	cmd.SetIn(stdin)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&runtimeBinary, "runtime-binary", "", "runtime executable to launch instead of scaffold handlers")
	cmd.Flags().StringVar(&runtimeWorkDir, "runtime-workdir", "", "absolute working directory for the runtime process")
	cmd.Flags().StringArrayVar(&runtimeArgs, "runtime-arg", nil, "argument to pass to the runtime process; repeat to pass multiple arguments")

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
	cmd.AddCommand(newDoctorCommand(stdout))
	cmd.SetVersionTemplate(fmt.Sprintf("%s %s\n", Name, Version))
	return cmd
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

func newDoctorCommand(stdout io.Writer) *cobra.Command {
	var binary string
	var workDir string
	var jsonOutput bool
	var versionArgs []string

	cmd := &cobra.Command{
		Use:           "doctor",
		Short:         "Check the local Claude Code runtime boundary",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := doctor.Run(context.Background(), doctor.Spec{
				AdapterName: Name,
				Binary:      binary,
				VersionArgs: versionArgs,
				WorkDir:     workDir,
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
			})
			if jsonOutput {
				payload := struct {
					OK     bool          `json:"ok"`
					Error  string        `json:"error,omitempty"`
					Report doctor.Report `json:"report"`
				}{
					OK:     err == nil,
					Report: report,
				}
				if err != nil {
					payload.Error = err.Error()
				}
				encoder := json.NewEncoder(stdout)
				encoder.SetIndent("", "  ")
				if encodeErr := encoder.Encode(payload); encodeErr != nil {
					return encodeErr
				}
			} else {
				writeDoctorReport(stdout, report, err)
			}
			if err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&binary, "binary", "claude", "Claude Code executable to probe")
	cmd.Flags().StringVar(&workDir, "workdir", "", "working directory for the probe (defaults to current directory)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "write a JSON report")
	cmd.Flags().StringArrayVar(&versionArgs, "version-arg", []string{"--version"}, "argument for the version probe; repeat to pass multiple arguments")
	return cmd
}

func writeDoctorReport(w io.Writer, report doctor.Report, runErr error) {
	status := "ok"
	if runErr != nil {
		status = "failed"
	}
	_, _ = fmt.Fprintf(w, "%s doctor: %s\n", report.AdapterName, status)
	_, _ = fmt.Fprintf(w, "binary: %s\n", report.Binary)
	if report.ResolvedCommand != "" {
		_, _ = fmt.Fprintf(w, "resolved: %s\n", report.ResolvedCommand)
	}
	if report.WorkDir != "" {
		_, _ = fmt.Fprintf(w, "workdir: %s\n", report.WorkDir)
	}
	if len(report.VersionArgs) != 0 {
		_, _ = fmt.Fprintf(w, "version args: %s\n", strings.Join(report.VersionArgs, " "))
	}
	for _, status := range report.Environment {
		state := "missing"
		if status.Present {
			state = "present"
		}
		suffix := ""
		if status.Sensitive {
			suffix = " (redacted)"
		}
		if status.Required {
			suffix += " (required)"
		}
		_, _ = fmt.Fprintf(w, "env %s: %s%s\n", status.Name, state, suffix)
	}
	writeProbeOutput(w, "stdout", report.VersionStdout, report.StdoutTruncated)
	writeProbeOutput(w, "stderr", report.VersionStderr, report.StderrTruncated)
}

func writeProbeOutput(w io.Writer, label string, output string, truncated bool) {
	if output == "" && !truncated {
		return
	}
	output = strings.TrimRight(output, "\n")
	if output != "" {
		_, _ = fmt.Fprintf(w, "%s: %s\n", label, output)
	}
	if truncated {
		_, _ = fmt.Fprintf(w, "%s: [truncated]\n", label)
	}
}
