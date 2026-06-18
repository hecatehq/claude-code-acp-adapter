# Testing

This repository currently tests the adapter scaffold, not a complete Claude Code
runtime bridge.

## Covered Today

- CLI version output
- ACP `initialize` response shape
- custom ACP `initialize` handlers receiving raw client params before returning
  dynamic initialize results
- runtime-backed initialize passthrough so child agent info, capabilities, and
  auth methods reach the ACP client
- runtime ACP auth calls: `authenticate` method-id forwarding and `logout`
  request forwarding
- request ID preservation
- server-to-client JSON-RPC requests from handlers, including successful
  client responses and client RPC errors
- malformed JSON errors without stopping later requests
- invalid JSON-RPC version errors
- notification dispatch without responses
- fake runtime method dispatch through the stdio transport
- fake runtime error propagation
- fake runtime session creation
- fake prompt lifecycle with assistant chunk, tool call, and tool update
  notifications emitted before the prompt response
- cancel notification and cancel request behavior
- session close and post-close not-found behavior
- scaffold `session/prompt` not-implemented errors
- 1 MiB inbound message cap
- hardened process runner behavior: fixed argv, shell rejection, absolute cwd
  enforcement, env allowlists, redaction, output caps, missing-binary errors,
  non-zero exits, and context cancellation
- long-lived process start behavior: stdin/stdout pipes, bounded stderr capture,
  process IDs, exit errors, shell rejection, and cancellation
- `doctor` runtime-boundary probe success, missing-binary, shell rejection,
  failed version-probe, secret redaction, and invalid-workdir behavior
- runtime launcher defaults, env/argv merging, bounded stderr, shell rejection,
  missing-workdir validation, exit errors, and cancellation
- runtime JSON-RPC client request/response matching, notifications, child
  events, error responses, malformed stdout failure, and request timeout
  cleanup
- runtime JSON-RPC child request responses, including successful result replies
  and error replies written back to the child runtime
- ACP initialize negotiation over the runtime JSON-RPC client, including client
  info/capabilities, agent capability parsing, protocol-version mismatch, and
  runtime RPC error propagation
- session capability preservation in runtime initialize results
- ACP session lifecycle calls over the runtime JSON-RPC client: `session/new`,
  `session/prompt`, `session/cancel`, `session/close`, session updates, stop
  reasons, MCP server payloads, and RPC error propagation
- `session/new` result preservation for runtime-provided `configOptions` and
  legacy `modes`, so model/reasoning/mode selectors survive the bridge
- runtime ACP config/mode setters: `session/set_config_option` and
  `session/set_mode` raw result forwarding
- ACP session load/resume/list/delete protocol calls, including replay updates,
  raw resume result preservation, listed session metadata, cursor parsing, and
  delete request forwarding
- ACP server-to-runtime bridge behavior: handler param validation, session
  method proxying, prompt update forwarding, cancel notifications, close
  requests, and runtime RPC error mapping
- ACP server-to-runtime bridge coverage for session load, resume, list, and
  delete methods
- ACP bridge forwarding for runtime child requests that require client
  responses, including returning the client result to the child runtime
- ACP bridge forwarding for `authenticate` and `logout`
- ACP bridge forwarding for `session/set_config_option` and legacy
  `session/set_mode`
- runtime host composition: subprocess launch, ACP initialize handshake,
  initialize result retention, bridge option exposure, prompt update forwarding,
  and protocol-version mismatch cleanup
- root ACP runtime flags: opt-in subprocess-backed serving, required absolute
  runtime workdir, runtime argv passthrough, and default scaffold behavior when
  no runtime binary is configured

## Not Covered Yet

These must be tested before Hecate switches from the current Claude Agent ACP
adapter to this one:

- vendor-specific persistent session storage and restore semantics
- Claude-specific session fork behavior, if exposed by the chosen runtime
  boundary
- prompt streaming with assistant chunks and terminal prompt results
- real normal cancellation, wedged-runtime forced cancellation, and no
  double-settle behavior
- auth methods and terminal auth behavior in local/remote environments
- gateway auth metadata
- settings resolution, settings trust filtering, and settings reloads
- model allowlists, model aliases, effort options, and permission-mode
  availability by model
- AskUserQuestion and MCP elicitation forms
- shell, file, edit, grep, glob, web, MCP, TODO, task, plan, memory, and
  terminal-output tool mappings
- orphan result skipping after cancelled queued prompts
- query-closed errors for prompts/cancels after stream termination
- local slash-command metadata stripping
- deterministic release binaries

## Test Strategy

Use `internal/acptest` for all protocol-level tests. It drives the real stdio
JSON-RPC path, so fake runtime tests exercise the same transport Hecate and
other ACP hosts will use.

Use `internal/process` for every subprocess boundary. Its tests pin the
security contract before real Claude Code integration lands. Use `Run` for
bounded one-shot probes and `Start` for long-lived runtime sessions that need
stdio pipes.

Use `internal/doctor` for local runtime readiness checks. Its tests use the Go
test binary as a fake Claude Code executable so command probing stays
deterministic and does not require a real Claude Code install.

Use `internal/runtimeproc` for the process-backed runtime boundary. It is the
only place that should decide the default Claude Code executable, allowed
inherited environment, and launch-time stdio process wiring.

Use `internal/runtimejsonrpc` for newline-delimited JSON-RPC over a launched
runtime process. It owns request IDs, pending response matching, child
notification delivery, and protocol decode failures; higher layers should keep
Claude Code-specific ACP mapping outside this transport client.

Use `internal/runtimeacp` for ACP protocol lifecycle calls made to a
subprocess-backed runtime. It should stay protocol-shaped and vendor-neutral;
Claude Code-specific behavior belongs in the layer that maps Claude Code
runtime semantics onto ACP.

Use `internal/runtimebridge` to connect ACP server handlers to a
subprocess-backed runtime client. It owns handler-level param decoding,
runtime-error mapping, and forwarding runtime `session/update` notifications
while a prompt request is active.

Use `internal/runtimehost` as the composition boundary for real runtime
sessions. It owns launching the subprocess-backed client, sending the ACP
`initialize` handshake with adapter identity/capabilities, retaining the
runtime's initialize result, exposing the bridge's ACP handler options, and
force-closing the child during cleanup.
