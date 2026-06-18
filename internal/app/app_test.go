package app

import (
	"bytes"
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
