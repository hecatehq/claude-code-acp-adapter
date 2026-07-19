package claudecodeadapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	adapterprocess "github.com/hecatehq/acp-adapter-kit/process"
)

const (
	claudeCommandDiscoveryRequestID     = "hecate-command-discovery"
	claudeCommandDiscoveryMaxLines      = 64
	claudeCommandDiscoveryMaxBytes      = 1024 * 1024
	claudeCommandDiscoveryMaxTotalBytes = 2 * 1024 * 1024
	claudeCommandDiscoveryStderrMax     = 8 * 1024

	// Keep provider-controlled catalog data well below the ACP transport's
	// 1 MiB message cap, including JSON escaping and update-envelope overhead.
	claudeCommandDiscoveryMaxCommands         = 128
	claudeCommandDiscoveryMaxNameBytes        = 96
	claudeCommandDiscoveryMaxDescriptionBytes = 512
	claudeCommandDiscoveryMaxHintBytes        = 128
	claudeCommandDiscoveryMaxCatalogBytes     = 512 * 1024
	claudeCommandDiscoveryTimeout             = 3 * time.Second
)

// discoverAvailableCommands asks a short-lived, no-prompt Claude stream-json
// session for its current command inventory. The command response is the
// source of truth: no adapter-owned list filters, extends, or constrains it.
//
// Discovery deliberately omits prompt text, native session ids, MCP server
// configuration, and adapter/session metadata. --bare prevents automatic
// workspace hooks, plugin sync, keychain access, and CLAUDE.md discovery;
// --strict-mcp-config prevents configured MCP servers from starting. The
// resulting catalog comes from Claude CLI's documented bare/minimal startup
// boundary, rather than an unrestricted project/plugin startup inventory.
func discoverAvailableCommands(ctx context.Context, session commandbridge.CommandDiscoverySession, starter commandbridge.CommandStarter) ([]commandbridge.AvailableCommand, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if starter == nil {
		return nil, errors.New("Claude command discovery starter is required")
	}

	spec, err := claudeCommandDiscoverySpec(session)
	if err != nil {
		return nil, err
	}
	child, err := starter.Start(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("start Claude command discovery: %w", err)
	}
	defer func() {
		_ = child.Stdin.Close()
		_ = child.Kill()
		_ = child.Wait()
	}()

	request, err := json.Marshal(claudeCommandDiscoveryRequest{
		Type:      "control_request",
		RequestID: claudeCommandDiscoveryRequestID,
		Request: claudeControlRequest{
			Subtype:      "initialize",
			SystemPrompt: []string{""},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode Claude command discovery request: %w", err)
	}
	request = append(request, '\n')
	if _, err := child.Stdin.Write(request); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("send Claude command discovery request")
	}
	if err := child.Stdin.Close(); err != nil && ctx.Err() == nil {
		return nil, errors.New("close Claude command discovery input")
	}

	stdout := &claudeCommandDiscoveryCountingReader{
		Reader: io.LimitReader(child.Stdout, claudeCommandDiscoveryMaxTotalBytes),
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), claudeCommandDiscoveryMaxBytes)
	for lineCount := 0; scanner.Scan(); lineCount++ {
		if lineCount >= claudeCommandDiscoveryMaxLines {
			return nil, errors.New("Claude command discovery response exceeded line limit")
		}
		commands, matched, err := parseClaudeCommandDiscoveryLine(scanner.Bytes())
		if err != nil {
			return nil, err
		}
		if !matched {
			continue
		}
		return commands, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if stdout.BytesRead >= claudeCommandDiscoveryMaxTotalBytes {
		return nil, errors.New("Claude command discovery response exceeded total size limit")
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("read Claude command discovery response")
	}
	if err := child.Wait(); err != nil {
		return nil, errors.New("Claude command discovery exited before responding")
	}
	return nil, errors.New("Claude command discovery response is missing")
}

func claudeCommandDiscoverySpec(session commandbridge.CommandDiscoverySession) (adapterprocess.StartSpec, error) {
	cwd := strings.TrimSpace(session.CWD)
	if cwd == "" {
		return adapterprocess.StartSpec{}, errors.New("Claude command discovery cwd is required")
	}
	args := []string{
		"--print",
		"--bare",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--no-session-persistence",
		"--permission-mode", "dontAsk",
		"--strict-mcp-config",
	}
	return adapterprocess.StartSpec{
		Command:     "claude",
		Args:        args,
		Dir:         cwd,
		Env:         claudeProcessEnv(),
		StderrLimit: claudeCommandDiscoveryStderrMax,
	}, nil
}

type claudeCommandDiscoveryRequest struct {
	Type      string               `json:"type"`
	RequestID string               `json:"request_id"`
	Request   claudeControlRequest `json:"request"`
}

type claudeControlRequest struct {
	Subtype      string   `json:"subtype"`
	SystemPrompt []string `json:"systemPrompt"`
}

type claudeCommandDiscoveryEnvelope struct {
	Type     string          `json:"type"`
	Response json.RawMessage `json:"response"`
}

type claudeCommandDiscoveryResponse struct {
	RequestID string `json:"request_id"`
	Subtype   string `json:"subtype"`
	Response  struct {
		Commands *json.RawMessage `json:"commands"`
	} `json:"response"`
}

type claudeAvailableCommand struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ArgumentHint string   `json:"argumentHint"`
	Aliases      []string `json:"aliases"`
}

func parseClaudeCommandDiscoveryLine(line []byte) ([]commandbridge.AvailableCommand, bool, error) {
	var envelope claudeCommandDiscoveryEnvelope
	if err := json.Unmarshal(line, &envelope); err != nil {
		// Claude's stream can contain non-control events before the response we
		// need. Ignore malformed or unrelated records without exposing their
		// contents through errors, logs, or ACP state.
		return nil, false, nil
	}
	if envelope.Type != "control_response" || len(envelope.Response) == 0 {
		return nil, false, nil
	}
	var response claudeCommandDiscoveryResponse
	if err := json.Unmarshal(envelope.Response, &response); err != nil {
		return nil, false, nil
	}
	if response.RequestID != claudeCommandDiscoveryRequestID {
		return nil, false, nil
	}
	if response.Subtype != "success" {
		return nil, true, errors.New("Claude command discovery was not successful")
	}
	if response.Response.Commands == nil {
		return nil, true, errors.New("Claude command discovery response omitted commands")
	}
	var commands []claudeAvailableCommand
	if err := json.Unmarshal(*response.Response.Commands, &commands); err != nil || commands == nil {
		return nil, true, errors.New("Claude command discovery response has invalid commands")
	}
	return claudeAvailableCommandsToACP(commands), true, nil
}

func claudeAvailableCommandsToACP(source []claudeAvailableCommand) []commandbridge.AvailableCommand {
	if len(source) == 0 {
		return []commandbridge.AvailableCommand{}
	}
	out := make([]commandbridge.AvailableCommand, 0, len(source))
	primary := make(map[string]struct{}, len(source))
	var catalogBytes int
	for _, command := range source {
		name := truncateClaudeCommandField(normalizeClaudeCommandName(command.Name), claudeCommandDiscoveryMaxNameBytes)
		if name == "" {
			continue
		}
		if _, exists := primary[name]; exists {
			continue
		}
		primary[name] = struct{}{}
		description := truncateClaudeCommandField(strings.TrimSpace(command.Description), claudeCommandDiscoveryMaxDescriptionBytes)
		hint := truncateClaudeCommandField(strings.TrimSpace(command.ArgumentHint), claudeCommandDiscoveryMaxHintBytes)
		out, catalogBytes = appendClaudeAvailableCommand(out, catalogBytes, name, description, hint)
	}
	seen := make(map[string]struct{}, len(primary))
	for name := range primary {
		seen[name] = struct{}{}
	}
	for _, command := range source {
		name := truncateClaudeCommandField(normalizeClaudeCommandName(command.Name), claudeCommandDiscoveryMaxNameBytes)
		if name == "" {
			continue
		}
		description := truncateClaudeCommandField(strings.TrimSpace(command.Description), claudeCommandDiscoveryMaxDescriptionBytes)
		hint := truncateClaudeCommandField(strings.TrimSpace(command.ArgumentHint), claudeCommandDiscoveryMaxHintBytes)
		for _, alias := range command.Aliases {
			alias = truncateClaudeCommandField(normalizeClaudeCommandName(alias), claudeCommandDiscoveryMaxNameBytes)
			if alias == "" {
				continue
			}
			if _, isPrimary := primary[alias]; isPrimary {
				continue
			}
			if _, exists := seen[alias]; exists {
				continue
			}
			aliasDescription := "Alias for /" + name
			if description != "" {
				aliasDescription += ": " + description
			}
			aliasDescription = truncateClaudeCommandField(aliasDescription, claudeCommandDiscoveryMaxDescriptionBytes)
			before := len(out)
			out, catalogBytes = appendClaudeAvailableCommand(out, catalogBytes, alias, aliasDescription, hint)
			if len(out) > before {
				seen[alias] = struct{}{}
			}
		}
	}
	if len(out) == 0 {
		return []commandbridge.AvailableCommand{}
	}
	return out
}

func appendClaudeAvailableCommand(out []commandbridge.AvailableCommand, catalogBytes int, name, description, hint string) ([]commandbridge.AvailableCommand, int) {
	if len(out) >= claudeCommandDiscoveryMaxCommands {
		return out, catalogBytes
	}
	command := commandbridge.AvailableCommand{Name: name, Description: description, InputHint: hint}
	size := claudeAvailableCommandWireSize(command)
	if size <= 0 || catalogBytes+size > claudeCommandDiscoveryMaxCatalogBytes {
		return out, catalogBytes
	}
	return append(out, command), catalogBytes + size
}

func claudeAvailableCommandWireSize(command commandbridge.AvailableCommand) int {
	item := map[string]any{
		"name":        strings.TrimSpace(command.Name),
		"description": strings.TrimSpace(command.Description),
	}
	if hint := strings.TrimSpace(command.InputHint); hint != "" {
		item["input"] = map[string]string{"hint": hint}
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return 0
	}
	return len(raw) + 1 // a comma or closing-array byte in the enclosing list
}

func truncateClaudeCommandField(value string, maxBytes int) string {
	value = strings.TrimSpace(value)
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	limit := maxBytes
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return strings.TrimSpace(value[:limit])
}

func normalizeClaudeCommandName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimLeft(name, "/")
	if name == "" || strings.ContainsAny(name, "\x00\r\n\t ") {
		return ""
	}
	return name
}

type claudeCommandDiscoveryCountingReader struct {
	io.Reader
	BytesRead int64
}

func (r *claudeCommandDiscoveryCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.BytesRead += int64(n)
	return n, err
}
