//go:build real_cli

package claudecodeadapter_test

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	client := acptest.NewLiveClient(t, claudecodeadapter.NewServer("real-cli-smoke"), acptest.WithAutoAllowPermissions())
	client.Request("initialize", "initialize", map[string]any{}, 4*time.Minute)

	session := newRealCLISession(t, client, t.TempDir())
	assertRealCLISessionLifecycle(t, client, session.SessionID, session.CWD)

	responses := client.PromptText("prompt-basic", session.SessionID, "Reply briefly with one sentence confirming the Claude Code ACP adapter real CLI smoke. Do not inspect files or run commands.", 4*time.Minute)
	assertRealCLIPromptCompleted(t, responses, "Claude Code")
	assertRealCLIMissingSessionClassification(t, client)

	setRealCLIConfigOption(t, client, session.SessionID, "permission_mode", "bypassPermissions")
	toolFile := filepath.Join(session.CWD, "acp-real-cli-tool.txt")
	toolResponses := client.PromptText("prompt-tool", session.SessionID, "Use a local shell command or file edit to create acp-real-cli-tool.txt in the current workspace containing exactly claude-code-acp-real-cli-tool. Then reply with one sentence starting with DONE.", 4*time.Minute)
	assertRealCLIPromptCompleted(t, toolResponses, "Claude Code")
	assertRealCLIToolFlow(t, toolResponses, "Claude Code")
	raw, err := os.ReadFile(toolFile)
	if err != nil {
		t.Fatalf("read tool-created file: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "claude-code-acp-real-cli-tool" {
		t.Fatalf("tool-created file = %q, want claude-code-acp-real-cli-tool", string(raw))
	}

	mcpDir := t.TempDir()
	mcpRecord := filepath.Join(mcpDir, "mcp-record.txt")
	mcpSession := newRealCLISessionWithMCP(t, client, t.TempDir(), acptest.BuildMCPStdioEchoServer(t, mcpDir), mcpRecord)
	setRealCLIConfigOption(t, client, mcpSession.SessionID, "permission_mode", "bypassPermissions")
	mcpResponses := client.PromptText("prompt-mcp", mcpSession.SessionID, "Use the MCP tool named echo from the MCP server named hecatesmoke exactly once with text claude-code-mcp-smoke. Then reply with one sentence starting with DONE.", 4*time.Minute)
	assertRealCLIPromptCompleted(t, mcpResponses, "Claude Code")
	assertRealCLIMCPFlow(t, mcpResponses, "Claude Code")
	assertRealCLIMCPRecord(t, mcpRecord, "claude-code-mcp-smoke")

	cancelSession := newRealCLISession(t, client, t.TempDir())
	setRealCLIConfigOption(t, client, cancelSession.SessionID, "permission_mode", "bypassPermissions")
	cancelResponses := client.PromptTextAndCancel("prompt-cancel", cancelSession.SessionID, "Run a local shell command that sleeps for 30 seconds, then reply with the word done.", 4*time.Second, 4*time.Minute)
	assertRealCLIPromptCancelled(t, cancelResponses, "Claude Code")
	client.AssertNoLateResponse("prompt-cancel", time.Second)
}

func assertRealCLIMissingSessionClassification(t testing.TB, client *acptest.LiveClient) {
	t.Helper()
	missingID := randomRealCLIUUID(t)
	loadResponses := client.Request("session-load-missing", "session/load", map[string]any{
		"sessionId": missingID,
		"cwd":       t.TempDir(),
	}, 4*time.Minute)
	if len(loadResponses) == 0 {
		t.Fatal("session/load for missing native conversation returned no responses")
	}
	var loadResult map[string]any
	loadResponses[len(loadResponses)-1].ResultInto(t, &loadResult)

	responses := client.PromptText("prompt-missing-native", missingID, "Reply with the word unreachable.", 4*time.Minute)
	assertMissingClaudeSessionLifecycle(t, responses, missingID)
}

func randomRealCLIUUID(t testing.TB) string {
	t.Helper()
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatalf("generate missing native session UUID: %v", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
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

type realCLISession struct {
	SessionID string
	CWD       string
}

func newRealCLISession(t testing.TB, client *acptest.LiveClient, cwd string) realCLISession {
	t.Helper()
	return createRealCLISession(t, client, map[string]any{"cwd": cwd})
}

func newRealCLISessionWithMCP(t testing.TB, client *acptest.LiveClient, cwd, command, recordPath string) realCLISession {
	t.Helper()
	return createRealCLISession(t, client, map[string]any{
		"cwd": cwd,
		"mcpServers": []map[string]any{{
			"name":    "hecatesmoke",
			"command": command,
			"env": []map[string]string{{
				"name":  "ACP_MCP_ECHO_RECORD_PATH",
				"value": recordPath,
			}},
		}},
	})
}

func createRealCLISession(t testing.TB, client *acptest.LiveClient, params map[string]any) realCLISession {
	t.Helper()
	createdResponses := client.Request(acptest.UniqueID("session-new"), "session/new", params, 4*time.Minute)
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
	cwd, _ := params["cwd"].(string)
	return realCLISession{SessionID: session.SessionID, CWD: cwd}
}

func assertRealCLISessionLifecycle(t testing.TB, client *acptest.LiveClient, sessionID, cwd string) {
	t.Helper()
	listResponses := client.Request("session-list", "session/list", map[string]any{}, 4*time.Minute)
	var list struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
		} `json:"sessions"`
	}
	listResponses[len(listResponses)-1].ResultInto(t, &list)
	var found bool
	for _, session := range list.Sessions {
		if session.SessionID == sessionID {
			found = true
			if session.CWD != cwd {
				t.Fatalf("listed cwd = %q, want %q", session.CWD, cwd)
			}
		}
	}
	if !found {
		t.Fatalf("session/list = %#v, want %q", list.Sessions, sessionID)
	}

	loadResponses := client.Request("session-load", "session/load", map[string]any{"sessionId": sessionID, "cwd": cwd}, 4*time.Minute)
	var load struct {
		ConfigOptions []struct {
			ID string `json:"id"`
		} `json:"configOptions"`
	}
	loadResponses[len(loadResponses)-1].ResultInto(t, &load)
	if len(load.ConfigOptions) == 0 {
		t.Fatalf("session/load result = %#v, want config options", load)
	}
}

func setRealCLIConfigOption(t testing.TB, client *acptest.LiveClient, sessionID, configID, value string) {
	t.Helper()
	responses := client.Request("session-config-"+configID, "session/set_config_option", map[string]any{
		"sessionId": sessionID,
		"configId":  configID,
		"value":     value,
	}, 4*time.Minute)
	if len(responses) != 2 {
		t.Fatalf("session/set_config_option responses = %#v, want update + result", responses)
	}
	var result struct {
		ConfigOptions []struct {
			ID           string `json:"id"`
			CurrentValue string `json:"currentValue"`
		} `json:"configOptions"`
	}
	responses[len(responses)-1].ResultInto(t, &result)
	for _, option := range result.ConfigOptions {
		if option.ID == configID {
			if option.CurrentValue != value {
				t.Fatalf("%s current value = %q, want %q", configID, option.CurrentValue, value)
			}
			return
		}
	}
	t.Fatalf("config options = %#v, want %s", result.ConfigOptions, configID)
}

func assertRealCLIToolFlow(t testing.TB, responses []acptest.Response, provider string) {
	t.Helper()
	var sawToolStart, sawToolFinish, sawPermission bool
	for _, response := range responses {
		if response.Error != nil {
			t.Fatalf("%s tool-flow response error: %+v", provider, *response.Error)
		}
		switch response.Method {
		case "session/request_permission":
			sawPermission = true
		case "session/update":
			update := decodeSessionUpdate(t, response)
			if strings.HasPrefix(update.Update.ToolCallID, "prompt-command-") {
				continue
			}
			switch update.Update.SessionUpdate {
			case "tool_call":
				sawToolStart = true
			case "tool_call_update":
				if update.Update.Status == "completed" {
					sawToolFinish = true
				}
			}
		}
	}
	if !sawToolStart || !sawToolFinish {
		t.Fatalf("%s tool-flow responses did not include completed provider tool updates: start=%v finish=%v permission=%v responses=%#v", provider, sawToolStart, sawToolFinish, sawPermission, responses)
	}
}

func assertRealCLIMCPFlow(t testing.TB, responses []acptest.Response, provider string) {
	t.Helper()
	var sawMCPStart, sawMCPFinish bool
	for _, response := range responses {
		if response.Error != nil {
			t.Fatalf("%s MCP-flow response error: %+v", provider, *response.Error)
		}
		if response.Method != "session/update" {
			continue
		}
		update := decodeSessionUpdate(t, response)
		if update.Update.Kind != "mcp" {
			continue
		}
		switch update.Update.SessionUpdate {
		case "tool_call":
			sawMCPStart = true
		case "tool_call_update":
			if update.Update.Status == "completed" {
				sawMCPFinish = true
			}
		}
	}
	if !sawMCPStart || !sawMCPFinish {
		t.Fatalf("%s MCP-flow responses did not include completed MCP tool updates: start=%v finish=%v responses=%#v", provider, sawMCPStart, sawMCPFinish, responses)
	}
}

func assertRealCLIMCPRecord(t testing.TB, recordPath, want string) {
	t.Helper()
	raw, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read MCP record: %v", err)
	}
	if strings.TrimSpace(string(raw)) != want {
		t.Fatalf("MCP record = %q, want %q", string(raw), want)
	}
}

func assertRealCLIPromptCancelled(t testing.TB, responses []acptest.Response, provider string) {
	t.Helper()
	if len(responses) < 2 {
		t.Fatalf("%s cancel responses = %#v, want command lifecycle and prompt result", provider, responses)
	}
	var terminalResponses int
	for _, response := range responses {
		if response.Error != nil {
			t.Fatalf("%s cancel response error: %+v", provider, *response.Error)
		}
		if response.Method == "" {
			terminalResponses++
		}
	}
	if terminalResponses != 1 {
		t.Fatalf("%s terminal prompt responses = %d, want exactly one: %#v", provider, terminalResponses, responses)
	}
	var promptResult struct {
		StopReason string `json:"stopReason"`
	}
	responses[len(responses)-1].ResultInto(t, &promptResult)
	if promptResult.StopReason != "cancelled" {
		t.Fatalf("%s stop reason = %q, want cancelled", provider, promptResult.StopReason)
	}
}
