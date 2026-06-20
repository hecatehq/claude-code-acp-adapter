# Testing

This repository tests the adapter scaffold, the subprocess-backed ACP runtime
bridge, and the first native Claude Code command bridge. It does not yet cover
a complete Claude Agent replacement with persistent vendor sessions and
tool/permission/elicitation parity.

## Covered Today

- CLI version output
- ACP `initialize` response shape
- custom ACP `initialize` handlers receiving raw client params before returning
  dynamic initialize results
- runtime-backed initialize passthrough so child agent info, capabilities, and
  auth methods reach the ACP client
- runtime-backed initialize result wrappers preserve runtime-provided extension
  fields through the runtime host
- runtime initialize forwarding for client terminal-auth capability
- runtime ACP auth calls: `authenticate` method-id forwarding and `logout`
  request forwarding
- request ID preservation
- server-to-client JSON-RPC requests from handlers, including successful
  client responses and client RPC errors
- in-flight client notification and concurrent cancel-request dispatch while a
  request handler is running, including prompt cancellation through the runtime
  bridge
- protocol-level `$/cancel_request` cancellation for in-flight method handlers
  waiting on server-to-client requests
- runtime bridge request context propagation, so protocol cancellation reaches
  context-aware runtime calls
- ordered ACP method execution without blocking notification dispatch behind a
  burst of queued method requests
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
- scaffold known ACP methods return structured not-implemented errors instead
  of method-not-found when no runtime binary is configured, including unstable
  SDK-known document, provider, and NES methods
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
- runtime JSON-RPC request cancellation sends `$/cancel_request` to the child
  runtime while ignoring the later abandoned response
- runtime JSON-RPC child request responses, including successful result replies
  and error replies written back to the child runtime
- ACP initialize negotiation over the runtime JSON-RPC client, including client
  info/capabilities, agent capability parsing, protocol-version mismatch, and
  runtime RPC error propagation
- session capability preservation in runtime initialize results
- MCP `acp`, `http`, and `sse` capability preservation in runtime initialize
  results
- ACP session lifecycle calls over the runtime JSON-RPC client: `session/new`,
  `session/fork`, `session/prompt`, `session/cancel`, `session/close`, session
  updates, stop reasons, MCP server payloads, and RPC error propagation
- `session/new` result preservation for runtime-provided `configOptions` and
  legacy `modes`, so model/reasoning/mode selectors survive the bridge
- runtime ACP config/mode setters: `session/set_config_option` and
  `session/set_mode` raw result forwarding, including session updates emitted
  before setter responses
- runtime ACP raw lifecycle/config helpers preserve validated-but-unknown
  extension fields when the bridge forwards params to subprocess runtimes
- runtime ACP prompt and session-list result wrappers preserve runtime-provided
  extension fields when the bridge returns responses to ACP clients
- ACP session load/resume/fork/list/delete protocol calls, including replay
  updates, raw resume/fork result preservation, listed session metadata, cursor
  parsing, and delete request forwarding
- ACP bridge forwarding for resume-time replay updates emitted before
  `session/resume` responds
- ACP-transport MCP server request payloads preserve their opaque `id` and
  `_meta` data when forwarded to subprocess runtimes
- unstable MCP-over-ACP `mcp/message` pass-through, including raw inner MCP
  response preservation
- unstable MCP-over-ACP `mcp/message` notification forwarding from ACP clients
  to subprocess runtimes
- ACP server-to-runtime bridge behavior: handler param validation, session
  method proxying, prompt update forwarding, cancel notifications, close
  requests, and runtime RPC error mapping
- ACP bridge forwarding for setup-time `session/update` notifications emitted
  before `session/new` responds
- ACP bridge forwarding for close-time `session/update` notifications emitted
  before `session/close` responds
- ACP server-to-runtime bridge coverage for session load, resume, list, and
  delete methods
- ACP server-to-runtime bridge coverage for session fork and MCP message
  forwarding
- ACP bridge forwarding for dynamic `session/update` payloads such as
  `available_commands_update`, `config_option_update`, and
  `session_info_update`
- ACP bridge forwarding for runtime child requests that require client
  responses, including returning the client result to the child runtime
- ACP bridge forwarding for runtime MCP child requests, including
  `mcp/message` params and client response delivery back to the child runtime
- cancellation while awaiting a runtime child request, so a prompt can settle
  as cancelled even when the client never answers the child request
- ACP bridge forwarding for `authenticate` and `logout`
- ACP bridge forwarding for `session/set_config_option` and legacy
  `session/set_mode`
- runtime host composition: subprocess launch, ACP initialize handshake,
  initialize result retention, bridge option exposure, prompt update forwarding,
  and protocol-version mismatch cleanup
- root ACP scaffold initialize metadata and capabilities for this adapter
- root `doctor` command Claude binary default, JSON report shape, and Claude
  environment status list
- root ACP runtime flags: opt-in subprocess-backed serving, deferred runtime
  startup with forwarded client initialize capabilities, required absolute
  runtime workdir, runtime argv passthrough, Claude-specific environment
  allowlist inheritance, and runtime-flag precedence over the native command
  bridge
- root ACP native command bridge: session creation with Claude model/effort
  config options, permission-mode config option including `bypassPermissions`,
  config updates,
  `claude --print --output-format stream-json --session-id` argv construction,
  Claude UUID session creation, host-known session id adoption on
  `session/load`, `/init`/`/review`/`/code-review`/`/security-review`
  advertisement through the normal `claude --print` prompt path, additional
  workspace directories, ACP stdio/HTTP MCP server config propagation into
  Claude `--mcp-config`, streamed JSONL parsing into ACP assistant text,
  thinking, tool-call, and usage updates, Claude tool kind classification for
  shell/file/web/task/memory/todo/plan tools, generic command `tool_call`
  activity for the outer Claude process, in-memory fork capability, bounded
  transcript replay for later command prompts, and prompt completion
- Coder ACP SDK compatibility guardrails for the adopted protocol primitives:
  JSON-RPC error shape, default initialize protocol version, and selected
  runtime ACP request JSON shapes
- release packaging gate through `make release-check`: unit tests, race tests,
  vet, and a version-stamped local binary build

## Not Covered Yet

These must be tested before Hecate switches from the current Claude Agent ACP
adapter to this one:

- deeper vendor-specific durable persistent session storage and restore
  semantics beyond Claude `--session-id`
- Claude-specific session fork persistence/history semantics beyond in-memory
  command-backed state
- terminal prompt results beyond parsed command stream updates
- real vendor-runtime normal cancellation, wedged-runtime forced cancellation, and no
  double-settle behavior
- auth methods and terminal auth behavior in local/remote environments
- gateway auth metadata
- settings resolution, settings trust filtering, and settings reloads
- model allowlists, model alias discovery, and permission-mode availability by
  model beyond the initial static command-backed selectors
- provider-native permission request/event mapping beyond the selected Claude
  Code permission mode
- AskUserQuestion and MCP elicitation forms
- deeper shell, file, edit, grep, glob, web, TODO, task, plan, memory, and
  terminal-output tool mappings beyond kind classification
- orphan result skipping after cancelled queued prompts
- query-closed errors for prompts/cancels after stream termination
- local slash-command metadata stripping beyond the adapter-owned command set
- vendor MCP connection lifecycle semantics and MCP tool approval elicitations
- production release signing/provenance

## Test Strategy

Use [acp-adapter-kit](https://github.com/hecatehq/acp-adapter-kit) for
provider-neutral protocol/runtime/process tests. The kit owns ACP transport
conformance, subprocess safety, JSON-RPC request/cancel behavior, runtime ACP
DTO parity, runtime bridge forwarding, runtime host composition, fake-runtime
fixtures, and generic doctor-runner behavior.

Keep this repository's tests focused on Claude Code-specific adapter behavior:

- CLI version and no-argument ACP stdio behavior;
- scaffold `initialize` metadata and Claude capability flags;
- `doctor` command defaults for the Claude binary and Claude environment list;
- runtime flag wiring from Cobra into the shared runtime host;
- command-backed `claude --print` argv construction and config-option mapping;
- Claude-specific prompt, tool, permission, config, model, MCP, auth, and
  session mapping as those features land.

Do not recreate kit packages under `internal/`. Add reusable protocol/runtime
coverage to `acp-adapter-kit`, then update this adapter to the new kit version
and add only Claude-specific integration assertions here.
