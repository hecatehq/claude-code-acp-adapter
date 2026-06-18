# Roadmap

## Phase 0: Scaffold

- stdio JSON-RPC harness
- source-review notes
- CI for tests

## Phase 1: Protocol Conformance Harness

- typed ACP request/response structs for the methods this adapter supports
- golden transcript tests
- fake ACP client test harness

## Phase 2: Fake Claude Runtime

- fake SDK events for assistant chunks, tool calls, permission requests,
  elicitations, cancellation, orphan results, settings, model options, and
  terminal output
- tests proving ACP output shape before any real Claude Code process is used

## Phase 3: Claude Code Runtime Bridge

- choose a stable Claude Code / Claude Agent SDK integration boundary
- implement auth/session/prompt/cancel/config/mcp/tool/elicitation mappings
- port the edge cases recorded in `SOURCE_REVIEW.md`

## Phase 4: Release and Hecate Integration

- signed multi-platform releases
- Hecate registry entry points at `claude-code-acp-adapter`
- legacy npm launcher becomes explicit opt-in only
