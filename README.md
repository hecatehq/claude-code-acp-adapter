# claude-code-acp-adapter

Neutral Go ACP adapter for Claude Code.

This repository is an alpha Go ACP adapter for Claude Code. It runs as a small,
auditable binary that speaks ACP over stdio. The adapter can run Claude Code
prompts through its native command bridge, but full parity with the previous
Claude Agent ACP adapter is still in progress.

## Goals

- Speak standard Agent Client Protocol over stdio.
- Keep the adapter independent from Hecate internals.
- Avoid package-manager launchers, shell wrappers, and broad environment
  inheritance.
- Preserve the important behavior exposed by the previous Claude Agent ACP
  adapter: sessions, settings, auth, model/config options, permission requests,
  MCP servers, elicitation, tool updates, terminal output, cancellation, and
  resume/load behavior.
- Ship deterministic, signed Go release binaries.

## Current Status

Implemented:

- stdlib-only JSON-RPC/NDJSON ACP transport scaffold
- `initialize` response with adapter metadata
- structured errors for unimplemented methods
- source-review notes for the previous adapter behavior
- unit tests for the protocol scaffold
- `doctor` command for probing the local Claude Code binary boundary
- process-backed runtime launcher seam
- subprocess JSON-RPC client for ACP-style stdio runtime bridges
- ACP initialize client for subprocess runtime negotiation
- typed ACP session lifecycle calls for subprocess runtimes
- ACP server-to-runtime bridge for session methods and streamed updates
- runtime host seam that launches, initializes, and exposes the bridged child
- protocol forwarding for session load, resume, fork, list, delete, and
  MCP-over-ACP message payloads
- command-backed native Claude Code path using `claude --print`
- ACP model, effort, and permission-mode config options for the command-backed
  path
- command-backed Claude UUID session ids passed to `claude --session-id`,
  including `session/load` / `session/resume` adoption of host-known ids after
  adapter process restart
- in-memory command-backed session fork plus bounded transcript replay for
  multi-turn continuity while the adapter process is alive
- command-backed `session/list` metadata, `config_option_update`
  notifications for config changes, and `session_info_update` notifications
  when transcript metadata changes
- command-backed `/init`, `/review`, `/code-review`, `/security-review`,
  `/compact`, `/debug`, `/run`, and `/verify` advertisement through the normal
  `claude --print` prompt path
- command-backed ACP stdio/HTTP MCP server config propagation into Claude
  `--mcp-config` with `--strict-mcp-config`
- Claude `--output-format stream-json` translation into ACP assistant text,
  thinking, tool-call, and usage updates, plus generic command `tool_call`
  activity for the native Claude process
- CI and tag-driven release packaging for unsigned alpha binaries

Not implemented yet:

- deeper Claude Code / Claude Agent SDK integration beyond `claude --print`
- deeper vendor-specific durable/native persistent session semantics beyond
  Claude `--session-id`
- complete vendor-specific permission/MCP lifecycle/auth/slash-command/elicitation mapping beyond the adapter-owned command set
- runtime config/auth/model discovery and orphan-result handling
- production signing/provenance for release artifacts

## Development

Shared ACP transport, runtime JSON-RPC, bridge, host, process, doctor runner,
and fake-runtime test code lives in
[acp-adapter-kit](https://github.com/hecatehq/acp-adapter-kit). Keep this repo
focused on the Claude Code-specific CLI boundary, doctor defaults, docs, release
workflow, and vendor behavior.

The binary remains the primary integration mode. Hosts that need an embedded
adapter can import
`github.com/hecatehq/claude-code-acp-adapter/claudecodeadapter` to build the
same ACP server, info/options, CLI spec, config options, environment allowlists,
and Claude Code prompt command without shelling out to
`claude-code-acp-adapter`. The embedded path still launches the underlying
`claude` CLI for prompts; it only removes the extra adapter process boundary.

```sh
make release-check
make snapshot
go test ./...
go test -race ./...
go vet ./...
go run ./cmd/claude-code-acp-adapter --version
go run ./cmd/claude-code-acp-adapter doctor
```

See [docs/TESTING.md](docs/TESTING.md) for what is covered today and what must
be covered before this adapter can be used as the default production Claude
Agent ACP bridge.
See [docs/RELEASE.md](docs/RELEASE.md) for the tag-driven release flow.

## CLI Contract

The binary uses Cobra for human commands, but the root command with no arguments
is reserved for ACP stdio. Do not add default logging, banners, usage output, or
prompts to the no-argument path; stdout is the protocol stream.

Use `doctor` before wiring this adapter into an ACP host. It resolves the Claude
Code binary, runs a fixed-argv version probe through the hardened process
runner, and reports selected environment variable presence without printing
secret values. Use `--binary` to point at a non-default Claude executable and
`--json` for machine-readable output.

By default, the root ACP server owns lightweight ACP sessions and runs each
prompt through `claude --print` in the session workspace. The command-backed
path exposes ACP config options for model, effort, and Claude Code permission
mode, including Claude Code's `bypassPermissions` full-access mode, passes only
provider-specific environment variables through the shared process runner, and
runs Claude with `--output-format stream-json`. Known
Claude JSONL events are translated into ACP assistant text, thinking,
tool-call, and usage updates. Tool updates preserve Claude Code categories such
as shell execution, file reads/edits, web fetch/search, task tools, memory
recall, todos, and plan/thinking tools; unknown JSONL events are ignored rather
than shown as raw chat text. A generic `tool_call` still wraps the native
Claude process execution so hosts can show the outer command boundary. The session
state is lightweight in the adapter, but session ids are Claude-native UUIDs
passed to `claude --session-id`. `session/load` and `session/resume` can adopt a
host-known Claude session id after an adapter process restart; `session/fork`
and transcript replay remain in-memory conveniences while the adapter process
is alive. `session/list` returns the adapter's currently loaded session
metadata, and later prompts receive a bounded transcript prelude so
command-backed turns keep conversational context while still using Claude's
native session id. Config changes return the current config option list and
publish `config_option_update` notifications. Completed command-backed prompts
publish `session_info_update` notifications with the in-memory title and
updated timestamp when transcript metadata changes.
The adapter advertises `/init`, `/review`, `/code-review`, `/security-review`,
`/compact`, `/debug`, `/run`, and `/verify` as ACP available commands and passes
them through the normal `claude --print` prompt path. Other Claude Code commands
remain unadvertised until their non-interactive behavior is explicitly tested.

The root ACP server can also launch an explicit subprocess-backed ACP runtime
with `--runtime-binary`, `--runtime-workdir`, and repeated `--runtime-arg`
flags. That runtime process receives only the Claude Code adapter's explicit
environment allowlist (`PATH`, `HOME`, `XDG_CONFIG_HOME`, `TMPDIR`,
`ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL`, and `CLAUDE_CONFIG_DIR`); the parent
environment is not inherited wholesale. Runtime flags override the native
command-backed path and are mostly useful for protocol parity testing.

## Source Review

Before implementing the real bridge, read [docs/SOURCE_REVIEW.md](docs/SOURCE_REVIEW.md).
It records historical package/source behavior that this project needs to
preserve or deliberately replace.
