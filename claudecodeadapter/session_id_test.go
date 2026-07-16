package claudecodeadapter

import (
	"errors"
	"testing"
)

type failingEntropyReader struct{}

func (failingEntropyReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestNewClaudeSessionIDFailsClosedWithoutEntropy(t *testing.T) {
	if id := newClaudeSessionIDFrom(failingEntropyReader{}); id != "" {
		t.Fatalf("session id = %q, want empty id so the bridge rejects creation", id)
	}
}
