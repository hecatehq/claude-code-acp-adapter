# Stable Readiness

The `v0.1.0` release established the command-backed ACP v1 contract that
operators can depend on directly. Every later stable release preserves that
baseline and may add compatible surfaces. Stable does not mean every draft ACP
RFD or provider-native edge case is implemented; those remain tracked future
work unless they are part of the contract below.

## Required Gate For Every Stable Release

Before tagging:

- `make release-check` passes on the release commit.
- `make real-cli-smoke` passes on a prepared machine with an authenticated
  Claude Code CLI.
- Required GitHub checks pass on the exact commit that will be tagged.
- Every `Required` row in the parity matrix is covered. `Future` rows are
  non-blocking.

Immediately after tagging:

- The tag workflow publishes the expected release archives and
  `checksums.txt`.
- Every release archive passes checksum and GitHub artifact-attestation
  verification.
- Hecate is pinned to the stable module tag and its built-in adapter integration
  checks pass before Hecate consumes the release.

Tags are immutable. If a post-tag gate fails, do not adopt the release; correct
the problem and publish a patch release.

## Stable Baseline

The `v0.1.0` contract covers stable ACP v1 session methods, prompt streaming,
config selectors, MCP server config handoff into Claude Code, stable
`session/request_permission` handling, cancellation, release provenance, and
Hecate's embedded-adapter integration.

Draft ACP RFDs, including MCP-over-ACP and Elicitation, are not stable blockers.
Provider-native MCP lifecycle hooks, AskUserQuestion/elicitation flows beyond
stable permission requests, runtime model discovery, and deeper Claude-native
SDK integration are future work unless promoted into this file as required
gates.

## Parity Matrix

| Surface                    | Current state                                                                                                                                             | Automated coverage                                                                                  | Stable-release decision                                                                      |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| ACP initialize             | Adapter metadata, auth/logout, MCP, prompt, and stable session capabilities are advertised.                                                               | Unit tests plus shared kit initialize conformance.                                                   | Required; covered.                                                                          |
| Auth and logout            | ACP `authenticate` maps to `claude /login`; ACP `logout` maps to `claude auth logout`.                                                                    | Unit tests, fake bridge tests, and opt-in real CLI smoke.                                            | Required shape covered; destructive auth-cycle smoke remains operator opt-in.                |
| Session lifecycle          | `session/new`, `session/list`, `session/load`, `session/resume`, `session/fork`, `session/close`, and `session/delete` work for command-backed sessions. | Unit tests, portable upstream parity checks, and opt-in real CLI list/load smoke.                     | Required; Claude `--session-id` creation and `--resume` adoption are covered.                |
| Prompt streaming           | Claude stream JSON maps to ACP assistant text, thinking, usage, stop reasons, and tool updates.                                                           | Source-shaped parser fixtures, command bridge tests, and opt-in real CLI prompt/tool smoke.           | Required; covered for real shell/file update flow.                                          |
| Config selectors           | Static model, effort, and permission-mode selectors are exposed and updateable.                                                                           | Unit tests and portable selector parity checks.                                                      | Required as static selectors; runtime discovery is future work.                              |
| MCP handoff                | ACP stdio/HTTP MCP server config maps to Claude `--mcp-config` with strict config enabled.                                                                | Unit tests, Hecate embedded-adapter tests, and opt-in real CLI stdio MCP echo-tool smoke.              | Required handoff covered; draft MCP-over-ACP/provider lifecycle hooks are future work.       |
| Permissions and eliciting  | Stable ACP permission requests plus denied/rejected/blocked provider tool results map to ACP permission/tool state.                                      | Source-shaped parser fixtures, grant/reject/cancel tests, Hecate embedded-adapter tests, and opt-in real CLI permission auto-approval. | Required stable permission flow covered; AskUserQuestion/elicitation-style prompts are future work. |
| Cancellation               | ACP cancel terminates active command-backed prompts and drops post-cancel stream chunks.                                                                  | Unit tests, command bridge tests, portable parity checks, Hecate embedded-adapter tests, and opt-in real CLI cancellation smoke. | Required; normal cancellation/no double-settle is covered, wedged-runtime force cases are future hardening. |
| Release artifacts          | GoReleaser archives are checksum-verified and provenance-attested by the tag workflow.                                                                    | Release workflow doc test and `make release-check`; release verification uses `gh attestation verify`. | Required for every stable release.                                                           |
| Hecate integration         | Hecate compiles the versioned Go adapter library into its binary.                                                                                         | Hecate agent-adapter tests and `just test-acp-real-embedded claude_code`.                             | Required for every Hecate adapter pin bump.                                                  |

## Release Decision

Cut a stable tag only after every pre-tag gate passes. Treat it as ready for
Hecate consumption only after every post-tag gate passes. Future rows should
stay documented, but they do not block a stable adapter release.
