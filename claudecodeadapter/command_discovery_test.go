package claudecodeadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hecatehq/acp-adapter-kit/acptest"
	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	adapterprocess "github.com/hecatehq/acp-adapter-kit/process"
)

func TestDiscoverAvailableCommandsUsesBareControlInventory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake command is Unix-only")
	}
	workdir := t.TempDir()
	extraDir := t.TempDir()
	metadataPath := filepath.Join(t.TempDir(), "metadata")
	stdinPath := filepath.Join(t.TempDir(), "stdin")
	response := "{\"type\":\"control_response\",\"response\":{\"request_id\":\"hecate-command-discovery\",\"subtype\":\"success\",\"response\":{\"commands\":[{\"name\":\"goal\",\"description\":\"Set a durable goal.\",\"argumentHint\":\"outcome\"},{\"name\":\"loop\",\"description\":\"Run a loop until complete.\",\"argumentHint\":\"[interval]\",\"aliases\":[\"proactive\"]},{\"name\":\"/workspace-skill\",\"description\":\"Use the workspace skill.\"}],\"account\":{\"email\":\"private@example.test\"},\"models\":[{\"id\":\"private-model\"}],\"agents\":[{\"name\":\"private-agent\"}]}}}"
	script := "#!/bin/sh\nset -eu\n" +
		"{\n" +
		"  printf 'cwd=%s\\n' \"$PWD\"\n" +
		"  printf 'args='\n" +
		"  printf '%s\\037' \"$@\"\n" +
		"  printf '\\n'\n" +
		"  printf 'unrelated=%s\\n' \"${HECATE_DISCOVERY_UNRELATED-}\"\n" +
		"} > " + shellQuote(metadataPath) + "\n" +
		"cat > " + shellQuote(stdinPath) + "\n" +
		"printf '%s\\n' " + shellQuote("{\"type\":\"system\",\"account\":{\"email\":\"private@example.test\"}}") + "\n" +
		"printf '%s\\n' " + shellQuote(response) + "\n"
	installDiscoveryFakeClaude(t, script)
	t.Setenv("HECATE_DISCOVERY_UNRELATED", "must-not-leak")

	commands, err := discoverAvailableCommands(context.Background(), commandbridge.CommandDiscoverySession{
		ID:                    "session-1",
		CWD:                   workdir,
		AdditionalDirectories: []string{extraDir},
	}, commandbridge.ProcessRunner{})
	if err != nil {
		t.Fatalf("discoverAvailableCommands: %v", err)
	}
	wantCommands := []commandbridge.AvailableCommand{
		{Name: "goal", Description: "Set a durable goal.", InputHint: "outcome"},
		{Name: "loop", Description: "Run a loop until complete.", InputHint: "[interval]"},
		{Name: "workspace-skill", Description: "Use the workspace skill."},
		{Name: "proactive", Description: "Alias for /loop: Run a loop until complete.", InputHint: "[interval]"},
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}

	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	canonicalWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	metadataLines := strings.Split(strings.TrimSuffix(string(metadata), "\n"), "\n")
	if len(metadataLines) != 3 || metadataLines[0] != "cwd="+canonicalWorkdir || metadataLines[2] != "unrelated=" {
		t.Fatalf("discovery metadata = %q, want cwd and no unrelated environment", metadata)
	}
	args := strings.Split(strings.TrimSuffix(strings.TrimPrefix(metadataLines[1], "args="), "\x1f"), "\x1f")
	wantArgs := []string{
		"--print",
		"--bare",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--no-session-persistence",
		"--permission-mode", "dontAsk",
		"--strict-mcp-config",
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("discovery args = %#v, want %#v", args, wantArgs)
	}
	for _, forbidden := range []string{"--mcp-config", "--session-id", "--resume", "--add-dir"} {
		for _, arg := range args {
			if arg == forbidden {
				t.Fatalf("discovery args = %#v, must not include %q", args, forbidden)
			}
		}
	}

	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read discovery stdin: %v", err)
	}
	var request map[string]any
	if err := json.Unmarshal(stdin, &request); err != nil {
		t.Fatalf("decode discovery control request: %v", err)
	}
	requestBody, _ := request["request"].(map[string]any)
	if request["type"] != "control_request" || request["request_id"] != claudeCommandDiscoveryRequestID ||
		requestBody["subtype"] != "initialize" || !reflect.DeepEqual(requestBody["systemPrompt"], []any{""}) {
		t.Fatalf("discovery control request = %#v, want no-prompt initialize", request)
	}
}

func TestDiscoverAvailableCommandsKeepsProviderOutputPrivateOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake command is Unix-only")
	}
	installDiscoveryFakeClaude(t, "#!/bin/sh\nset -eu\ncat >/dev/null\nprintf '%s\\n' '{\"type\":\"control_response\",\"response\":{\"request_id\":\"another-request\",\"subtype\":\"success\",\"response\":{\"commands\":[]}}}'\nprintf '%s\\n' 'private-token-never-show' >&2\nexit 1\n")

	_, err := discoverAvailableCommands(context.Background(), commandbridge.CommandDiscoverySession{CWD: t.TempDir()}, commandbridge.ProcessRunner{})
	if err == nil {
		t.Fatal("discoverAvailableCommands error = nil, want missing matching response")
	}
	if strings.Contains(err.Error(), "private-token-never-show") || strings.Contains(err.Error(), "another-request") {
		t.Fatalf("discovery error = %q, must not expose provider output", err)
	}
}

func TestDiscoverAvailableCommandsRejectsMissingOrNullCommands(t *testing.T) {
	for _, name := range []string{"missing", "null"} {
		t.Run(name, func(t *testing.T) {
			commands := "{}"
			if name == "null" {
				commands = "{\"commands\":null}"
			}
			line := "{\"type\":\"control_response\",\"response\":{\"request_id\":\"hecate-command-discovery\",\"subtype\":\"success\",\"response\":" + commands + "}}"
			_, matched, err := parseClaudeCommandDiscoveryLine([]byte(line))
			if !matched || err == nil {
				t.Fatalf("parse result = matched:%v err:%v, want matching malformed response", matched, err)
			}
		})
	}

	commands, matched, err := parseClaudeCommandDiscoveryLine([]byte("{\"type\":\"control_response\",\"response\":{\"request_id\":\"hecate-command-discovery\",\"subtype\":\"success\",\"response\":{\"commands\":[]}}}"))
	if err != nil || !matched || commands == nil || len(commands) != 0 {
		t.Fatalf("explicit empty response = commands:%#v matched:%v err:%v, want authoritative empty snapshot", commands, matched, err)
	}
}

func TestDiscoverAvailableCommandsCancelsBoundedProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake command is Unix-only")
	}
	installDiscoveryFakeClaude(t, "#!/bin/sh\nset -eu\ncat >/dev/null\nsleep 10\n")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := discoverAvailableCommands(ctx, commandbridge.CommandDiscoverySession{CWD: t.TempDir()}, commandbridge.ProcessRunner{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("discoverAvailableCommands error = %v, want context deadline", err)
	}
}

func TestClaudeAvailableCommandsToACPPrioritizesCanonicalsAndBoundsProviderFields(t *testing.T) {
	got := claudeAvailableCommandsToACP([]claudeAvailableCommand{
		{Name: "goal", Description: "Goal", Aliases: []string{"loop", "objective"}},
		{Name: "loop", Description: "Canonical loop", Aliases: []string{"proactive"}},
	})
	want := []commandbridge.AvailableCommand{
		{Name: "goal", Description: "Goal"},
		{Name: "loop", Description: "Canonical loop"},
		{Name: "objective", Description: "Alias for /goal: Goal"},
		{Name: "proactive", Description: "Alias for /loop: Canonical loop"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want canonical commands before aliases %#v", got, want)
	}

	source := make([]claudeAvailableCommand, claudeCommandDiscoveryMaxCommands)
	for index := range source {
		source[index] = claudeAvailableCommand{
			Name:         fmt.Sprintf("command-%03d", index),
			Description:  strings.Repeat("\x01", claudeCommandDiscoveryMaxDescriptionBytes*8),
			ArgumentHint: strings.Repeat("\x01", claudeCommandDiscoveryMaxHintBytes*8),
			Aliases:      []string{fmt.Sprintf("alias-%03d", index)},
		}
	}
	bounded := claudeAvailableCommandsToACP(source)
	var total int
	for _, command := range bounded {
		if len(command.Name) > claudeCommandDiscoveryMaxNameBytes ||
			len(command.Description) > claudeCommandDiscoveryMaxDescriptionBytes ||
			len(command.InputHint) > claudeCommandDiscoveryMaxHintBytes {
			t.Fatalf("command field exceeds bound: %#v", command)
		}
		total += claudeAvailableCommandWireSize(command)
	}
	if len(bounded) > claudeCommandDiscoveryMaxCommands || total > claudeCommandDiscoveryMaxCatalogBytes {
		t.Fatalf("bounded catalog count=%d bytes=%d, want <=%d and <=%d", len(bounded), total, claudeCommandDiscoveryMaxCommands, claudeCommandDiscoveryMaxCatalogBytes)
	}
}

func TestClaudeCommandDiscoveryCountingReaderBoundsWholeStream(t *testing.T) {
	limited := &claudeCommandDiscoveryCountingReader{
		Reader: io.LimitReader(strings.NewReader(strings.Repeat("x", claudeCommandDiscoveryMaxTotalBytes*2)), claudeCommandDiscoveryMaxTotalBytes),
	}
	if _, err := io.ReadAll(limited); err != nil {
		t.Fatalf("read limited stream: %v", err)
	}
	if limited.BytesRead != claudeCommandDiscoveryMaxTotalBytes {
		t.Fatalf("read bytes = %d, want total cap %d", limited.BytesRead, claudeCommandDiscoveryMaxTotalBytes)
	}
}

func TestNewServerPublishesDiscoveredCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake command is Unix-only")
	}
	installDiscoveryFakeClaude(t, "#!/bin/sh\nset -eu\ncat >/dev/null\nprintf '%s\\n' '{\"type\":\"control_response\",\"response\":{\"request_id\":\"hecate-command-discovery\",\"subtype\":\"success\",\"response\":{\"commands\":[{\"name\":\"goal\",\"description\":\"Set a goal.\"},{\"name\":\"loop\",\"aliases\":[\"proactive\"]}]}}}'\n")

	updates := make(chan []string, 1)
	client := acptest.NewLiveClient(t, NewServerWithRunner("test", discoveryTestRunner{}), acptest.WithLiveResponseHandler(func(_ *acptest.LiveClient, response acptest.Response) {
		if response.Method != "session/update" {
			return
		}
		var payload map[string]any
		response.ParamsInto(t, &payload)
		update, _ := payload["update"].(map[string]any)
		if update["sessionUpdate"] != "available_commands_update" {
			return
		}
		rawCommands, _ := update["availableCommands"].([]any)
		names := make([]string, 0, len(rawCommands))
		for _, raw := range rawCommands {
			command, _ := raw.(map[string]any)
			if name, _ := command["name"].(string); name != "" {
				names = append(names, name)
			}
		}
		updates <- names
	}))

	responses := client.Request("new", "session/new", map[string]any{"cwd": t.TempDir()}, time.Second)
	var sessionID string
	for _, response := range responses {
		if response.Method != "" || response.Error != nil {
			continue
		}
		var result map[string]any
		response.ResultInto(t, &result)
		sessionID, _ = result["sessionId"].(string)
	}
	if sessionID == "" {
		t.Fatalf("session/new responses = %#v, want session result", responses)
	}
	got := waitForDiscoveredCommandNames(t, client, updates)
	if want := []string{"goal", "loop", "proactive"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered command names = %#v, want %#v", got, want)
	}
}

func TestNewServerKeepsSessionUsableWhenCommandDiscoveryFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake command is Unix-only")
	}
	installDiscoveryFakeClaude(t, "#!/bin/sh\nset -eu\ncat >/dev/null\nexit 1\n")
	client := acptest.NewLiveClient(t, NewServerWithRunner("test", discoveryTestRunner{}))

	responses := client.Request("new", "session/new", map[string]any{"cwd": t.TempDir()}, time.Second)
	for _, response := range responses {
		if response.Method != "" || response.Error != nil {
			continue
		}
		var result map[string]any
		response.ResultInto(t, &result)
		if sessionID, _ := result["sessionId"].(string); sessionID != "" {
			return
		}
	}
	t.Fatalf("session/new responses = %#v, want usable session despite failed discovery", responses)
}

type discoveryTestRunner struct{}

func (discoveryTestRunner) Run(ctx context.Context, spec adapterprocess.Spec) (adapterprocess.Result, error) {
	return adapterprocess.Run(ctx, spec)
}

func (discoveryTestRunner) RunStream(ctx context.Context, spec adapterprocess.Spec, onStdout func([]byte) error) (adapterprocess.Result, error) {
	return adapterprocess.RunStream(ctx, spec, onStdout)
}

func (discoveryTestRunner) Start(ctx context.Context, spec adapterprocess.StartSpec) (*adapterprocess.Child, error) {
	return adapterprocess.Start(ctx, spec)
}

func installDiscoveryFakeClaude(t testing.TB, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake Claude command: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func waitForDiscoveredCommandNames(t testing.TB, client *acptest.LiveClient, updates <-chan []string) []string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		select {
		case update := <-updates:
			return update
		default:
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatal("timed out waiting for discovered command update")
		}
		if remaining > 10*time.Millisecond {
			remaining = 10 * time.Millisecond
		}
		client.AssertNoLateResponse("available-command-wait", remaining)
	}
}
