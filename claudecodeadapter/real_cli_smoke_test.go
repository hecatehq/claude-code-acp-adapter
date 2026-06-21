//go:build real_cli

package claudecodeadapter_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

	client := newRealCLIACPClient(t, claudecodeadapter.NewServer("real-cli-smoke"))
	client.request("initialize", "initialize", map[string]any{})

	session := newRealCLISession(t, client, t.TempDir())
	assertRealCLISessionLifecycle(t, client, session.SessionID, session.CWD)

	responses := client.prompt("prompt-basic", session.SessionID, "Reply briefly with one sentence confirming the Claude Code ACP adapter real CLI smoke. Do not inspect files or run commands.")
	assertRealCLIPromptCompleted(t, responses, "Claude Code")

	setRealCLIConfigOption(t, client, session.SessionID, "permission_mode", "bypassPermissions")
	toolFile := filepath.Join(session.CWD, "acp-real-cli-tool.txt")
	toolResponses := client.prompt("prompt-tool", session.SessionID, "Use a local shell command or file edit to create acp-real-cli-tool.txt in the current workspace containing exactly claude-code-acp-real-cli-tool. Then reply with one sentence starting with DONE.")
	assertRealCLIPromptCompleted(t, toolResponses, "Claude Code")
	assertRealCLIToolFlow(t, toolResponses, "Claude Code")
	raw, err := os.ReadFile(toolFile)
	if err != nil {
		t.Fatalf("read tool-created file: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "claude-code-acp-real-cli-tool" {
		t.Fatalf("tool-created file = %q, want claude-code-acp-real-cli-tool", string(raw))
	}

	cancelSession := newRealCLISession(t, client, t.TempDir())
	setRealCLIConfigOption(t, client, cancelSession.SessionID, "permission_mode", "bypassPermissions")
	cancelResponses := client.promptAndCancel("prompt-cancel", cancelSession.SessionID, "Run a local shell command that sleeps for 30 seconds, then reply with the word done.")
	assertRealCLIPromptCancelled(t, cancelResponses, "Claude Code")
	client.assertNoLateResponse("prompt-cancel", time.Second)
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

type acpServer interface {
	Serve(io.Reader, io.Writer) error
}

type realCLIACPClient struct {
	t          testing.TB
	input      *io.PipeWriter
	responses  chan acptest.Response
	decodeDone chan error
	serveDone  chan error
	writeMu    sync.Mutex
}

func newRealCLIACPClient(t testing.TB, server acpServer) *realCLIACPClient {
	t.Helper()
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	client := &realCLIACPClient{
		t:          t,
		input:      inputWriter,
		responses:  make(chan acptest.Response, 64),
		decodeDone: make(chan error, 1),
		serveDone:  make(chan error, 1),
	}
	go func() {
		err := server.Serve(inputReader, outputWriter)
		_ = outputWriter.Close()
		client.serveDone <- err
	}()
	go func() {
		decoder := json.NewDecoder(outputReader)
		for {
			var response acptest.Response
			if err := decoder.Decode(&response); err != nil {
				if err == io.EOF {
					client.decodeDone <- nil
				} else {
					client.decodeDone <- err
				}
				close(client.responses)
				return
			}
			client.responses <- response
		}
	}()
	t.Cleanup(func() {
		_ = inputWriter.Close()
		select {
		case err := <-client.serveDone:
			if err != nil {
				t.Errorf("ACP server returned error during cleanup: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("timed out waiting for ACP server cleanup")
		}
	})
	return client
}

func (c *realCLIACPClient) request(id string, method string, params any) []acptest.Response {
	c.t.Helper()
	c.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	return c.collectUntilResponse(id, 4*time.Minute)
}

func (c *realCLIACPClient) prompt(id, sessionID, prompt string) []acptest.Response {
	c.t.Helper()
	return c.request(id, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []map[string]any{{"type": "text", "text": prompt}},
	})
}

func (c *realCLIACPClient) promptAndCancel(id, sessionID, prompt string) []acptest.Response {
	c.t.Helper()
	c.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": sessionID,
			"prompt":    []map[string]any{{"type": "text", "text": prompt}},
		},
	})
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	cancelled := false
	var out []acptest.Response
	deadline := time.After(4 * time.Minute)
	for {
		select {
		case <-timer.C:
			c.write(map[string]any{
				"jsonrpc": "2.0",
				"method":  "session/cancel",
				"params":  map[string]any{"sessionId": sessionID},
			})
			cancelled = true
		case response, ok := <-c.responses:
			if !ok {
				c.failDecodeClosed()
			}
			out = append(out, response)
			c.maybeAllowPermission(response)
			if responseIDEquals(response.ID, id) && response.Method == "" {
				if !cancelled {
					c.t.Fatalf("prompt %q completed before cancellation was sent: %#v", id, out)
				}
				return out
			}
		case <-deadline:
			c.t.Fatalf("timed out waiting for cancelled prompt %q", id)
		}
	}
}

func (c *realCLIACPClient) collectUntilResponse(id string, timeout time.Duration) []acptest.Response {
	c.t.Helper()
	deadline := time.After(timeout)
	var out []acptest.Response
	for {
		select {
		case response, ok := <-c.responses:
			if !ok {
				c.failDecodeClosed()
			}
			out = append(out, response)
			c.maybeAllowPermission(response)
			if responseIDEquals(response.ID, id) && response.Method == "" {
				return out
			}
		case <-deadline:
			c.t.Fatalf("timed out waiting for response %q", id)
		}
	}
}

func (c *realCLIACPClient) assertNoLateResponse(id string, duration time.Duration) {
	c.t.Helper()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	for {
		select {
		case response, ok := <-c.responses:
			if !ok {
				c.failDecodeClosed()
			}
			if responseIDEquals(response.ID, id) && response.Method == "" {
				c.t.Fatalf("late response for %q after cancellation: %#v", id, response)
			}
			c.maybeAllowPermission(response)
		case <-timer.C:
			return
		}
	}
}

func (c *realCLIACPClient) write(envelope any) {
	c.t.Helper()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := json.NewEncoder(c.input).Encode(envelope); err != nil {
		c.t.Fatalf("write ACP envelope: %v", err)
	}
}

func (c *realCLIACPClient) maybeAllowPermission(response acptest.Response) {
	c.t.Helper()
	if response.Method != "session/request_permission" || len(response.ID) == 0 {
		return
	}
	var req permissionRequest
	response.ParamsInto(c.t, &req)
	optionID := firstAllowOption(req.Options)
	if optionID == "" {
		c.t.Fatalf("permission request has no allow option: %#v", req.Options)
	}
	c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{
		JSONRPC: "2.0",
		ID:      append(json.RawMessage(nil), response.ID...),
		Result: map[string]any{
			"outcome": map[string]any{
				"outcome":  "selected",
				"optionId": optionID,
			},
		},
	})
}

func (c *realCLIACPClient) failDecodeClosed() {
	c.t.Helper()
	select {
	case err := <-c.decodeDone:
		if err != nil {
			c.t.Fatalf("decode ACP response: %v", err)
		}
		c.t.Fatal("ACP response stream closed before expected response")
	default:
		c.t.Fatal("ACP response stream closed before expected response")
	}
}

func responseIDEquals(raw json.RawMessage, want string) bool {
	var got string
	return json.Unmarshal(raw, &got) == nil && got == want
}

func firstAllowOption(options []struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}) string {
	for _, option := range options {
		if strings.Contains(strings.ToLower(option.Kind), "allow") || strings.Contains(strings.ToLower(option.OptionID), "allow") {
			return option.OptionID
		}
	}
	return ""
}

func newRealCLISession(t testing.TB, client *realCLIACPClient, cwd string) realCLISession {
	t.Helper()
	createdResponses := client.request("session-new-"+fmt.Sprint(time.Now().UnixNano()), "session/new", map[string]any{"cwd": cwd})
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
	return realCLISession{SessionID: session.SessionID, CWD: cwd}
}

func assertRealCLISessionLifecycle(t testing.TB, client *realCLIACPClient, sessionID, cwd string) {
	t.Helper()
	listResponses := client.request("session-list", "session/list", map[string]any{})
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

	loadResponses := client.request("session-load", "session/load", map[string]any{"sessionId": sessionID, "cwd": cwd})
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

func setRealCLIConfigOption(t testing.TB, client *realCLIACPClient, sessionID, configID, value string) {
	t.Helper()
	responses := client.request("session-config-"+configID, "session/set_config_option", map[string]any{
		"sessionId": sessionID,
		"configId":  configID,
		"value":     value,
	})
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
