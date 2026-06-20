# Roadmap

## Phase 0: Scaffold

- stdio JSON-RPC harness
- source-review notes
- CI for tests

## Phase 1: Protocol Conformance Harness

- typed ACP request/response structs for the methods this adapter supports
- golden transcript tests
- fake ACP client test harness
- fake runtime method/notification dispatch through the real stdio path

## Phase 2: Fake Claude Runtime

- fake SDK events for assistant chunks, tool calls, permission requests,
  elicitations, cancellation, orphan results, settings, model options, and
  terminal output
- tests proving ACP output shape before any real Claude Code process is used
- session lifecycle coverage for create, prompt, cancel, and close

## Phase 3: Claude Code Runtime Bridge

- use `acp-adapter-kit/commandbridge` for the first native direct-CLI path:
  lightweight ACP sessions keyed by Claude UUID session ids, one
  `claude --print --session-id` process per prompt, stdout forwarded as
  assistant text, and ACP cancel mapped to process cancellation
- use `acp-adapter-kit/process` for every subprocess boundary
- use `acp-adapter-kit/runtimeproc` as the only process-backed runtime launcher
- use `acp-adapter-kit/runtimejsonrpc` for newline-delimited JSON-RPC over
  runtime stdio
- use `runtimejsonrpc.Client.Respond` for child runtime requests that need a
  JSON-RPC response instead of treating every child request as terminal
- use `acp.MethodContext.Request` when a runtime child request must be
  forwarded to the ACP client and answered before the prompt can finish
- use `acp-adapter-kit/runtimeacp` for subprocess ACP lifecycle negotiation
- in runtime-backed mode, pass the child runtime initialize result through to
  the ACP client instead of advertising scaffold-only capabilities
- forward auth methods (`authenticate`, `logout`) through the same typed
  runtime ACP and bridge seams
- build real session bridges on the typed `runtimeacp` initialize/session
  calls before adding vendor-specific tool mappings
- use `acp-adapter-kit/runtimebridge` as the ACP server handler seam for
  subprocess-backed sessions
- keep session load/resume/fork/list/delete protocol forwarding in
  `runtimeacp`/`runtimebridge`; vendor-specific persistence belongs above it
- forward unstable MCP-over-ACP `mcp/message` payloads as raw protocol data
  until the adapter owns real vendor MCP connection lifecycle semantics
- preserve extra `session/new` result fields such as `configOptions` and
  `modes`; never narrow runtime session setup responses down to `sessionId`
- forward session configuration changes (`session/set_config_option`) and the
  legacy `session/set_mode` API without rewriting returned config state
- use `acp-adapter-kit/runtimehost` to compose process launch, runtime
  initialize, and ACP server bridge options
- defer subprocess-backed runtime startup until the outer ACP client's
  `initialize` params are available, then pass those client capabilities into
  the child runtime handshake
- expose the subprocess-backed runtime path only through explicit root runtime
  flags until the Claude Code boundary is stable
- keep `doctor` green against the target Claude Code binary before enabling
  real runtime sessions
- use `github.com/coder/acp-go-sdk` as the upstream source for generated ACP
  protocol primitives where its JSON shape matches the adapter contract
- keep the kit `acp` stdio transport until an SDK-backed transport can preserve
  the adapter's ordering and cancellation invariants: ordinary inbound methods
  are processed in order, notifications and explicitly concurrent methods can
  cut through a running method, server-to-client request IDs stay visually
  distinct from inbound IDs, malformed/version-invalid messages produce JSON-RPC
  errors, and the 1 MiB line cap remains enforced
- prefer small DTO/error aliases and parity tests before replacing larger
  runtime session payloads; generated SDK unions can be stricter than the
  adapter's current pass-through shapes
- keep `runtimeacp.InitializeParams` hand-written for now because the generated
  SDK request emits `clientCapabilities.auth` when client capabilities are set,
  which would change the adapter's current initialize wire shape
- expand the native Claude Code / Claude Agent integration boundary beyond
  `claude --print`
- keep adapter-owned slash commands explicit: `/init`, `/review`,
  `/code-review`, and `/security-review` stay on the normal `claude --print`
  prompt path, and additional commands need parity tests before being exposed
- continue hardening auth/session/prompt/cancel/config/mcp/tool mappings with
  source-shaped parser fixtures and real Hecate release-binary smoke tests;
  deeper elicitation and provider-native permission edge cases remain parity
  work
- port the edge cases recorded in `SOURCE_REVIEW.md`

## Phase 4: Release and Hecate Integration

- signed/provenance-backed release hardening
- keep Hecate registry/Dockerfile pins pointed at the released
  `claude-code-acp-adapter` binary, and run Hecate's ACP adapter release smoke
  after every adapter release bump
- no Hecate runtime launch path depends on a package-manager adapter wrapper
