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
- scaffold `session/prompt` not-implemented errors
- 1 MiB inbound message cap

## Not Covered Yet

These must be tested before Hecate switches from the current Claude Agent ACP
adapter to this one:

- session create/load/resume/list/fork/close/delete
- prompt streaming with assistant chunks and terminal prompt results
- normal cancellation, wedged-runtime forced cancellation, and no double-settle
  behavior
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
- environment allowlisting and process hardening
- deterministic release binaries

## Test Strategy

Use `internal/acptest` for all protocol-level tests. It drives the real stdio
JSON-RPC path, so fake runtime tests exercise the same transport Hecate and
other ACP hosts will use.
