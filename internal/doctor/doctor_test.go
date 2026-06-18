package doctor_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adapterprocess "github.com/hecatehq/acp-adapter-kit/process"
	"github.com/hecatehq/claude-code-acp-adapter/internal/doctor"
)

func TestRunHappyPath(t *testing.T) {
	t.Setenv("GO_WANT_DOCTOR_HELPER", "1")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-secret-doctor")
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/claude-config")

	report, err := doctor.Run(context.Background(), doctor.Spec{
		AdapterName: "test adapter",
		Binary:      os.Args[0],
		VersionArgs: []string{"-test.run=TestDoctorHelper", "--", "version"},
		WorkDir:     t.TempDir(),
		InheritEnv:  []string{"GO_WANT_DOCTOR_HELPER"},
		EnvVars: []doctor.EnvVar{
			{Name: "ANTHROPIC_API_KEY"},
			{Name: "CLAUDE_CONFIG_DIR"},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if report.ResolvedCommand == "" {
		t.Fatal("ResolvedCommand is empty")
	}
	if !strings.Contains(report.VersionStdout, "fake-claude 1.2.3") {
		t.Fatalf("VersionStdout = %q, want fake version", report.VersionStdout)
	}
	if strings.Contains(report.VersionStdout, "sk-ant-secret-doctor") {
		t.Fatalf("VersionStdout leaked secret: %q", report.VersionStdout)
	}
	if !strings.Contains(report.VersionStdout, adapterprocess.RedactedValue) {
		t.Fatalf("VersionStdout = %q, want redacted value", report.VersionStdout)
	}
	if got := envPresent(report, "ANTHROPIC_API_KEY"); !got {
		t.Fatalf("ANTHROPIC_API_KEY present = %v, want true", got)
	}
	if got := envSensitive(report, "ANTHROPIC_API_KEY"); !got {
		t.Fatalf("ANTHROPIC_API_KEY sensitive = %v, want true", got)
	}
}

func TestRunMissingBinaryReturnsTypedError(t *testing.T) {
	_, err := doctor.Run(context.Background(), doctor.Spec{
		Binary:  filepath.Join(t.TempDir(), "missing-claude"),
		WorkDir: t.TempDir(),
	})
	var missing *adapterprocess.CommandNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %T %[1]v, want CommandNotFoundError", err)
	}
}

func TestRunRejectsShellBinary(t *testing.T) {
	_, err := doctor.Run(context.Background(), doctor.Spec{
		Binary:  "/bin/sh",
		WorkDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "is a shell") {
		t.Fatalf("error = %v, want shell rejection", err)
	}
}

func TestRunVersionProbeFailureIsSanitized(t *testing.T) {
	t.Setenv("GO_WANT_DOCTOR_HELPER", "1")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-failing-secret")

	report, err := doctor.Run(context.Background(), doctor.Spec{
		Binary:      os.Args[0],
		VersionArgs: []string{"-test.run=TestDoctorHelper", "--", "fail"},
		WorkDir:     t.TempDir(),
		InheritEnv:  []string{"GO_WANT_DOCTOR_HELPER"},
		EnvVars:     []doctor.EnvVar{{Name: "ANTHROPIC_API_KEY"}},
	})
	var exitErr *adapterprocess.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T %[1]v, want ExitError", err)
	}
	if strings.Contains(report.VersionStderr, "sk-ant-failing-secret") {
		t.Fatalf("VersionStderr leaked secret: %q", report.VersionStderr)
	}
	if !strings.Contains(report.VersionStderr, adapterprocess.RedactedValue) {
		t.Fatalf("VersionStderr = %q, want redacted value", report.VersionStderr)
	}
}

func TestRunRejectsInvalidWorkingDirectory(t *testing.T) {
	_, err := doctor.Run(context.Background(), doctor.Spec{
		Binary:  os.Args[0],
		WorkDir: filepath.Join(t.TempDir(), "missing"),
	})
	if err == nil || !strings.Contains(err.Error(), "stat process working directory") {
		t.Fatalf("error = %v, want working-directory error", err)
	}
}

func envPresent(report doctor.Report, name string) bool {
	for _, status := range report.Environment {
		if status.Name == name {
			return status.Present
		}
	}
	return false
}

func envSensitive(report doctor.Report, name string) bool {
	for _, status := range report.Environment {
		if status.Name == name {
			return status.Sensitive
		}
	}
	return false
}

func TestDoctorHelper(t *testing.T) {
	if os.Getenv("GO_WANT_DOCTOR_HELPER") != "1" {
		return
	}
	args := os.Args
	sep := -1
	for i, arg := range args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep == -1 || sep+1 >= len(args) {
		os.Exit(2)
	}
	switch args[sep+1] {
	case "version":
		fmt.Printf("fake-claude 1.2.3 token=%s\n", os.Getenv("ANTHROPIC_API_KEY"))
	case "fail":
		fmt.Fprintf(os.Stderr, "bad token=%s\n", os.Getenv("ANTHROPIC_API_KEY"))
		os.Exit(9)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
