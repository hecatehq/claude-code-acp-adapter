# claude-code-acp-adapter

Neutral Go ACP adapter for Claude Code.

This repository is an early scaffold. It is intended to replace runtime npm
bridge launchers with a small, auditable Go adapter that speaks ACP over stdio.
It is not ready to use as a production Claude Code bridge yet.

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

Not implemented yet:

- Claude Code / Claude Agent SDK integration
- session creation/load/resume/list
- prompt streaming
- tool/permission/elicitation mapping
- cancellation and orphan-result handling
- model discovery/config options
- release packaging

## Development

```sh
go test ./...
go run ./cmd/claude-code-acp-adapter --version
```

See [docs/TESTING.md](docs/TESTING.md) for what is covered today and what must
be covered before this adapter can replace the current Claude Agent ACP bridge.

## CLI Contract

The binary uses Cobra for human commands, but the root command with no arguments
is reserved for ACP stdio. Do not add default logging, banners, usage output, or
prompts to the no-argument path; stdout is the protocol stream.

## Source Review

Before implementing the real bridge, read [docs/SOURCE_REVIEW.md](docs/SOURCE_REVIEW.md).
It records the behavior found in the current npm package and upstream adapter
source that this project needs to preserve or deliberately replace.
ACP adapter for running Claude Code over stdio
