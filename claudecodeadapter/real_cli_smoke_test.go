//go:build real_cli

package claudecodeadapter_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/hecatehq/acp-adapter-kit/acptest"
	"github.com/hecatehq/claude-code-acp-adapter/claudecodeadapter"
)

const realCLISmokeEnv = "ACP_ADAPTER_REAL_CLI_SMOKE"

func TestRealClaudeCodeCLISmoke(t *testing.T) {
	if os.Getenv(realCLISmokeEnv) != "1" {
		t.Skipf("set %s=1 to run the authenticated real Claude Code CLI smoke", realCLISmokeEnv)
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatalf("claude CLI not found in PATH: %v", err)
	}

	client := acptest.NewClient(t, claudecodeadapter.NewServer("real-cli-smoke"))
	client.Request("initialize", map[string]any{})

	createdResponses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "session/new",
		"params":  map[string]any{"cwd": t.TempDir()},
	})
	if len(createdResponses) < 1 {
		t.Fatalf("session/new responses = %#v, want at least a session response", createdResponses)
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	createdResponses[len(createdResponses)-1].ResultInto(t, &session)
	if session.SessionID == "" {
		t.Fatal("session id is empty")
	}

	responses := client.Send(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": session.SessionID,
			"prompt": []map[string]any{{
				"type": "text",
				"text": "Reply briefly with one sentence confirming the Claude Code ACP adapter real CLI smoke. Do not inspect files or run commands.",
			}},
		},
	})
	assertRealCLIPromptCompleted(t, responses, "Claude Code")
}

func assertRealCLIPromptCompleted(t testing.TB, responses []acptest.Response, provider string) {
	t.Helper()
	if len(responses) < 3 {
		t.Fatalf("%s prompt responses = %#v, want command start, assistant output, command finish, and prompt result", provider, responses)
	}

	var message strings.Builder
	for _, response := range responses {
		if response.Error != nil {
			t.Fatalf("%s prompt response error: %+v", provider, *response.Error)
		}
		if response.Method != "session/update" {
			continue
		}
		update := decodeSessionUpdate(t, response)
		if update.Update.SessionUpdate == "agent_message_chunk" {
			message.WriteString(decodeChunkText(t, update.Update.Content))
		}
	}
	if strings.TrimSpace(message.String()) == "" {
		t.Fatalf("%s prompt emitted no assistant message chunk: %#v", provider, responses)
	}

	var promptResult struct {
		StopReason string `json:"stopReason"`
	}
	responses[len(responses)-1].ResultInto(t, &promptResult)
	if promptResult.StopReason != "end_turn" {
		t.Fatalf("%s stop reason = %q, want end_turn", provider, promptResult.StopReason)
	}
}
