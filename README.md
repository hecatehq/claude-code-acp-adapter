# claude-code-acp-adapter

Neutral Go ACP adapter for Claude Code.

This repository is an alpha Go ACP adapter for Claude Code. It is intended to
replace runtime npm bridge launchers with a small, auditable binary that speaks
ACP over stdio. The adapter can run Claude Code prompts through its native
command bridge, but full parity with the previous Claude Agent ACP adapter is
still in progress.

## Goals

- Speak standard Agent Client Protocol over stdio.
- Keep the adapter independent from Hecate internals.
- Avoid runtime `npx`, shell wrappers, and broad environment inheritance.
- Preserve the important behavior exposed by the current Claude Agent ACP
  adapter: sessions, settings, auth, model/config options, permission requests,
  MCP servers, elicitation, tool updates, terminal output, cancellation, and
  resume/load behavior.
- Ship deterministic, signed Go release binaries.

## Current Status

Implemented:

- stdlib-only JSON-RPC/NDJSON ACP transport scaffold
- `initialize` response with adapter metadata
- structured errors for unimplemented methods
- source-review notes for the current npm/TypeScript adapter stack
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
- ACP model and effort config options for the command-backed path
- CI and tag-driven release packaging for unsigned alpha binaries

Not implemented yet:

- deeper Claude Code / Claude Agent SDK integration beyond `claude --print`
- vendor-specific persistent session semantics
- vendor-specific prompt/tool/permission/elicitation mapping
- runtime config/auth/model discovery and orphan-result handling
- production signing/provenance for release artifacts

## Development

Shared ACP transport, runtime JSON-RPC, bridge, host, process, doctor runner,
and fake-runtime test code lives in
[acp-adapter-kit](https://github.com/hecatehq/acp-adapter-kit). Keep this repo
focused on the Claude Code-specific CLI boundary, doctor defaults, docs, release
workflow, and vendor behavior.

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
be covered before this adapter can replace the current Claude Agent ACP bridge.
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
path exposes ACP config options for model and effort, passes only
provider-specific environment variables through the shared process runner, and
converts command stdout into ACP assistant text.

The root ACP server can also launch an explicit subprocess-backed ACP runtime
with `--runtime-binary`, `--runtime-workdir`, and repeated `--runtime-arg`
flags. That runtime process receives only the Claude Code adapter's explicit
environment allowlist (`PATH`, `HOME`, `XDG_CONFIG_HOME`, `TMPDIR`,
`ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL`, and `CLAUDE_CONFIG_DIR`); the parent
environment is not inherited wholesale. Runtime flags override the native
command-backed path and are mostly useful for protocol parity testing.

## Source Review

Before implementing the real bridge, read [docs/SOURCE_REVIEW.md](docs/SOURCE_REVIEW.md).
It records the behavior found in the current npm package and upstream adapter
source that this project needs to preserve or deliberately replace.
ACP adapter for running Claude Code over stdio
