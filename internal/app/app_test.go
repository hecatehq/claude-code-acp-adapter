package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var stdout bytes.Buffer
	code := Run([]string{"--version"}, nil, &stdout, nil)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0", code)
	}
	if got, want := stdout.String(), "claude-code-acp-adapter 0.0.0-dev\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	code := Run([]string{"version"}, nil, &stdout, nil)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0", code)
	}
	if got, want := stdout.String(), "claude-code-acp-adapter 0.0.0-dev\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestNoArgsStartsACPStdio(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")

	code := Run(nil, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, want 0; stderr=%q", code, stderr.String())
	}

	var response map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, stdout.String())
	}
	if response["result"] == nil {
		t.Fatalf("response missing result: %#v", response)
	}
}

func TestUnknownArgDoesNotPrintUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"wat"}, nil, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("Run returned %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, `unknown command "wat"`) {
		t.Fatalf("stderr = %q, want unknown command", got)
	}
}
