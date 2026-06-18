# Testing

This repository currently tests the adapter scaffold, not a complete Claude Code
runtime bridge.

## Covered Today

- CLI version output
- ACP `initialize` response shape
- request ID preservation
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
- ACP initialize negotiation over the runtime JSON-RPC client, including client
  info/capabilities, agent capability parsing, protocol-version mismatch, and
  runtime RPC error propagation
- ACP session lifecycle calls over the runtime JSON-RPC client: `session/new`,
  `session/prompt`, `session/cancel`, `session/close`, session updates, stop
  reasons, MCP server payloads, and RPC error propagation

## Not Covered Yet

These must be tested before Hecate switches from the current Claude Agent ACP
adapter to this one:

- session load/resume/list/fork/delete
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
