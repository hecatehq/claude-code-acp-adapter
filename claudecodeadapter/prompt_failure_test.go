package claudecodeadapter

import (
	"bytes"
	"errors"
	"testing"

	"github.com/hecatehq/acp-adapter-kit/commandbridge"
	adapterprocess "github.com/hecatehq/acp-adapter-kit/process"
)

func TestClassifyClaudePromptFailure(t *testing.T) {
	const sessionID = "550e8400-e29b-41d4-a716-446655440000"
	baseSession := commandbridge.Session{ID: sessionID, Adopted: true}
	baseCommand := adapterprocess.Spec{
		Command: "claude",
		Args:    []string{"--print", "--resume", sessionID, "hello"},
	}
	missing := []byte("No conversation found with session ID: " + sessionID + "\n")
	realMissingResult := []byte(`{"type":"result","subtype":"error_during_execution","duration_ms":0,"duration_api_ms":0,"is_error":true,"num_turns":0,"stop_reason":null,"session_id":"550e8400-e29b-41d4-a716-446655440002","total_cost_usd":0,"usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0,"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0},"service_tier":"standard","cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":0},"inference_geo":"","iterations":[],"speed":"standard"},"modelUsage":{},"permission_denials":[],"uuid":"550e8400-e29b-41d4-a716-446655440003","errors":["No conversation found with session ID: ` + sessionID + `"]}`)
	exitErr := &adapterprocess.ExitError{Command: "claude", Code: 1}
	tests := []struct {
		name    string
		session commandbridge.Session
		command adapterprocess.Spec
		result  adapterprocess.Result
		err     error
		want    commandbridge.PromptFailureKind
	}{
		{name: "missing native conversation", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stderr: missing}, err: exitErr, want: commandbridge.PromptFailureNativeSessionMissing},
		{name: "real missing native result", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: realMissingResult, Stderr: missing}, err: exitErr, want: commandbridge.PromptFailureNativeSessionMissing},
		{name: "prompt named resume flag", session: baseSession, command: adapterprocess.Spec{Command: "claude", Args: []string{"--resume", sessionID, "--resume"}}, result: adapterprocess.Result{Stderr: missing}, err: exitErr, want: commandbridge.PromptFailureNativeSessionMissing},
		{name: "prompt named session id flag", session: baseSession, command: adapterprocess.Spec{Command: "claude", Args: []string{"--resume", sessionID, "--session-id"}}, result: adapterprocess.Result{Stderr: missing}, err: exitErr, want: commandbridge.PromptFailureNativeSessionMissing},
		{name: "option delimiter", session: baseSession, command: adapterprocess.Spec{Command: "claude", Args: []string{"--resume", sessionID, "--", "--session-id"}}, result: adapterprocess.Result{Stderr: missing}, err: exitErr, want: commandbridge.PromptFailureNativeSessionMissing},
		{name: "stdout observed", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: []byte("partial"), Stderr: missing}, err: exitErr},
		{name: "result after provider turn", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"num_turns":0`), []byte(`"num_turns":1`), 1), Stderr: missing}, err: exitErr},
		{name: "result after token usage", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"input_tokens":0`), []byte(`"input_tokens":1`), 1), Stderr: missing}, err: exitErr},
		{name: "result after cost", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"total_cost_usd":0`), []byte(`"total_cost_usd":0.01`), 1), Stderr: missing}, err: exitErr},
		{name: "result with underflow cost", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"total_cost_usd":0`), []byte(`"total_cost_usd":1e-999999`), 1), Stderr: missing}, err: exitErr},
		{name: "result with null turns", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"num_turns":0`), []byte(`"num_turns":null`), 1), Stderr: missing}, err: exitErr},
		{name: "result with null token count", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"input_tokens":0`), []byte(`"input_tokens":null`), 1), Stderr: missing}, err: exitErr},
		{name: "result with string token count", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"input_tokens":0`), []byte(`"input_tokens":"0"`), 1), Stderr: missing}, err: exitErr},
		{name: "result with null cost", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"total_cost_usd":0`), []byte(`"total_cost_usd":null`), 1), Stderr: missing}, err: exitErr},
		{name: "result with null model usage", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"modelUsage":{}`), []byte(`"modelUsage":null`), 1), Stderr: missing}, err: exitErr},
		{name: "result with null permission denials", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"permission_denials":[]`), []byte(`"permission_denials":null`), 1), Stderr: missing}, err: exitErr},
		{name: "result with null iterations", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"iterations":[]`), []byte(`"iterations":null`), 1), Stderr: missing}, err: exitErr},
		{name: "result with null inference geo", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"inference_geo":""`), []byte(`"inference_geo":null`), 1), Stderr: missing}, err: exitErr},
		{name: "result with unknown field", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`{"type"`), []byte(`{"unexpected":true,"type"`), 1), Stderr: missing}, err: exitErr},
		{name: "result with duplicate top-level field", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"num_turns":0`), []byte(`"num_turns":1,"num_turns":0`), 1), Stderr: missing}, err: exitErr},
		{name: "result with duplicate nested field", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: bytes.Replace(realMissingResult, []byte(`"input_tokens":0`), []byte(`"input_tokens":1,"input_tokens":0`), 1), Stderr: missing}, err: exitErr},
		{name: "result with second line", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stdout: append(append([]byte(nil), realMissingResult...), []byte("\n{}")...), Stderr: missing}, err: exitErr},
		{name: "stdout truncated", session: baseSession, command: baseCommand, result: adapterprocess.Result{StdoutTruncated: true, Stderr: missing}, err: exitErr},
		{name: "stderr truncated", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stderr: missing, StderrTruncated: true}, err: exitErr},
		{name: "different failure", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stderr: []byte("authentication failed")}, err: exitErr},
		{name: "different stderr session", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stderr: []byte("No conversation found with session ID: 550e8400-e29b-41d4-a716-446655440001\n")}, err: exitErr},
		{name: "case changed error", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stderr: []byte("no conversation found with session ID: " + sessionID + "\n")}, err: exitErr},
		{name: "non exit failure", session: baseSession, command: baseCommand, result: adapterprocess.Result{Stderr: missing}, err: errors.New("launch failed")},
		{name: "different command", session: baseSession, command: adapterprocess.Spec{Command: "other", Args: baseCommand.Args}, result: adapterprocess.Result{Stderr: missing}, err: exitErr},
		{name: "mismatched ACP session", session: commandbridge.Session{ID: "550e8400-e29b-41d4-a716-446655440001"}, command: baseCommand, result: adapterprocess.Result{Stderr: missing}, err: exitErr},
		{name: "fresh command", session: baseSession, command: adapterprocess.Spec{Command: "claude", Args: []string{"--session-id", sessionID, "hello"}}, result: adapterprocess.Result{Stderr: missing}, err: exitErr},
		{name: "competing session id", session: baseSession, command: adapterprocess.Spec{Command: "claude", Args: []string{"--resume", sessionID, "--session-id", sessionID, "hello"}}, result: adapterprocess.Result{Stderr: missing}, err: exitErr},
		{name: "invalid resume id", session: baseSession, command: adapterprocess.Spec{Command: "claude", Args: []string{"--resume", "not-a-uuid", "hello"}}, result: adapterprocess.Result{Stderr: missing}, err: exitErr},
		{name: "missing resume id", session: baseSession, command: adapterprocess.Spec{Command: "claude", Args: []string{"--resume", "hello"}}, result: adapterprocess.Result{Stderr: missing}, err: exitErr},
		{name: "duplicate resume", session: baseSession, command: adapterprocess.Spec{Command: "claude", Args: []string{"--resume", sessionID, "--resume", sessionID, "hello"}}, result: adapterprocess.Result{Stderr: missing}, err: exitErr},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyClaudePromptFailure(test.session, test.command, test.result, test.err); got != test.want {
				t.Fatalf("classifyClaudePromptFailure() = %v, want %v", got, test.want)
			}
		})
	}
}
