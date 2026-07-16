package claudecodeadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	adapterprocess "github.com/hecatehq/acp-adapter-kit/process"
)

func classifyClaudePromptFailure(session commandbridge.Session, command adapterprocess.Spec, result adapterprocess.Result, runErr error) commandbridge.PromptFailureKind {
	if runErr == nil || command.Command != "claude" || session.ID == "" || result.StdoutTruncated || result.StderrTruncated {
		return commandbridge.PromptFailureUnknown
	}
	var exitErr *adapterprocess.ExitError
	if !errors.As(runErr, &exitErr) {
		return commandbridge.PromptFailureUnknown
	}

	sessionID, ok := claudeResumeSessionID(command.Args)
	if !ok || sessionID != session.ID || !missingClaudeSessionError(result.Stderr, sessionID) || !missingClaudeSessionStdout(result.Stdout, sessionID) {
		return commandbridge.PromptFailureUnknown
	}
	return commandbridge.PromptFailureNativeSessionMissing
}

func missingClaudeSessionStdout(stdout []byte, sessionID string) bool {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return true
	}
	if bytes.Contains(trimmed, []byte{'\n'}) {
		return false
	}
	if !hasUniqueJSONKeys(trimmed) {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil ||
		!hasExactJSONFields(fields,
			"type", "subtype", "duration_ms", "duration_api_ms", "is_error", "num_turns", "stop_reason",
			"session_id", "total_cost_usd", "usage", "modelUsage", "permission_denials", "uuid", "errors") {
		return false
	}
	var usageFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["usage"], &usageFields); err != nil ||
		!hasExactJSONFields(usageFields,
			"input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens", "output_tokens",
			"server_tool_use", "service_tier", "cache_creation", "inference_geo", "iterations", "speed") {
		return false
	}
	var serverToolFields map[string]json.RawMessage
	if err := json.Unmarshal(usageFields["server_tool_use"], &serverToolFields); err != nil ||
		!hasExactJSONFields(serverToolFields, "web_search_requests", "web_fetch_requests") {
		return false
	}
	var cacheCreationFields map[string]json.RawMessage
	if err := json.Unmarshal(usageFields["cache_creation"], &cacheCreationFields); err != nil ||
		!hasExactJSONFields(cacheCreationFields, "ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens") {
		return false
	}
	durationMS, durationOK := rawJSONInt64(fields["duration_ms"])
	typeName, typeOK := rawJSONString(fields["type"])
	subtype, subtypeOK := rawJSONString(fields["subtype"])
	resultSessionID, resultSessionOK := rawJSONString(fields["session_id"])
	resultUUID, resultUUIDOK := rawJSONString(fields["uuid"])
	serviceTier, serviceTierOK := rawJSONString(usageFields["service_tier"])
	_, inferenceGeoOK := rawJSONString(usageFields["inference_geo"])
	speed, speedOK := rawJSONString(usageFields["speed"])
	var reportedErrors []string
	errorsOK := rawJSONArray(fields["errors"], &reportedErrors)
	wantError := "No conversation found with session ID: " + sessionID
	return typeOK && typeName == "result" &&
		subtypeOK && subtype == "error_during_execution" &&
		durationOK && durationMS >= 0 &&
		rawJSONNumberIsZero(fields["duration_api_ms"]) &&
		bytes.Equal(bytes.TrimSpace(fields["is_error"]), []byte("true")) &&
		rawJSONNumberIsZero(fields["num_turns"]) &&
		bytes.Equal(bytes.TrimSpace(fields["stop_reason"]), []byte("null")) &&
		resultSessionOK && validClaudeSessionID(resultSessionID) &&
		rawJSONNumberIsZero(fields["total_cost_usd"]) &&
		rawJSONNumberIsZero(usageFields["input_tokens"]) &&
		rawJSONNumberIsZero(usageFields["cache_creation_input_tokens"]) &&
		rawJSONNumberIsZero(usageFields["cache_read_input_tokens"]) &&
		rawJSONNumberIsZero(usageFields["output_tokens"]) &&
		rawJSONNumberIsZero(serverToolFields["web_search_requests"]) &&
		rawJSONNumberIsZero(serverToolFields["web_fetch_requests"]) &&
		rawJSONNumberIsZero(cacheCreationFields["ephemeral_1h_input_tokens"]) &&
		rawJSONNumberIsZero(cacheCreationFields["ephemeral_5m_input_tokens"]) &&
		rawJSONArrayIsEmpty(usageFields["iterations"]) &&
		serviceTierOK && serviceTier != "" &&
		inferenceGeoOK &&
		speedOK && speed != "" &&
		rawJSONObjectIsEmpty(fields["modelUsage"]) &&
		rawJSONArrayIsEmpty(fields["permission_denials"]) &&
		resultUUIDOK && validClaudeSessionID(resultUUID) &&
		errorsOK && len(reportedErrors) == 1 &&
		reportedErrors[0] == wantError
}

func hasExactJSONFields(fields map[string]json.RawMessage, names ...string) bool {
	if len(fields) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	return true
}

func hasUniqueJSONKeys(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if !consumeUniqueJSONValue(decoder) {
		return false
	}
	var trailing any
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}

func consumeUniqueJSONValue(decoder *json.Decoder) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return true
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return false
			}
			key, ok := keyToken.(string)
			if !ok {
				return false
			}
			if _, duplicate := seen[key]; duplicate {
				return false
			}
			seen[key] = struct{}{}
			if !consumeUniqueJSONValue(decoder) {
				return false
			}
		}
		closing, err := decoder.Token()
		return err == nil && closing == json.Delim('}')
	case '[':
		for decoder.More() {
			if !consumeUniqueJSONValue(decoder) {
				return false
			}
		}
		closing, err := decoder.Token()
		return err == nil && closing == json.Delim(']')
	default:
		return false
	}
}

func rawJSONString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func rawJSONInt64(raw json.RawMessage) (int64, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return 0, false
	}
	number, ok := value.(json.Number)
	if !ok || strings.ContainsAny(number.String(), ".eE") {
		return 0, false
	}
	parsed, err := strconv.ParseInt(number.String(), 10, 64)
	return parsed, err == nil
}

func rawJSONNumberIsZero(raw json.RawMessage) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	mantissa := strings.SplitN(strings.SplitN(number.String(), "e", 2)[0], "E", 2)[0]
	sawDigit := false
	for _, character := range mantissa {
		switch {
		case character == '0':
			sawDigit = true
		case character == '-' || character == '.':
		case character >= '1' && character <= '9':
			return false
		default:
			return false
		}
	}
	return sawDigit
}

func rawJSONArray(raw json.RawMessage, target any) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '[' && json.Unmarshal(trimmed, target) == nil
}

func rawJSONArrayIsEmpty(raw json.RawMessage) bool {
	var values []json.RawMessage
	return rawJSONArray(raw, &values) && len(values) == 0
}

func rawJSONObjectIsEmpty(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var values map[string]json.RawMessage
	return json.Unmarshal(trimmed, &values) == nil && len(values) == 0
}

func claudeResumeSessionID(args []string) (string, bool) {
	// PromptCommand always appends the user prompt as the final argument. Keep
	// prompt text such as "--resume" or "--session-id" outside option parsing.
	optionEnd := len(args) - 1
	if optionEnd <= 0 {
		return "", false
	}
	resumeID := ""
	for index := 0; index < optionEnd; index++ {
		switch args[index] {
		case "--":
			return resumeID, resumeID != ""
		case "--resume":
			if resumeID != "" || index+1 >= optionEnd || !validClaudeSessionID(args[index+1]) {
				return "", false
			}
			resumeID = args[index+1]
			index++
		case "--session-id":
			return "", false
		}
	}
	return resumeID, resumeID != ""
}

func missingClaudeSessionError(stderr []byte, sessionID string) bool {
	want := "No conversation found with session ID: " + sessionID
	for _, line := range bytes.Split(stderr, []byte{'\n'}) {
		if strings.TrimSpace(string(line)) == want {
			return true
		}
	}
	return false
}
