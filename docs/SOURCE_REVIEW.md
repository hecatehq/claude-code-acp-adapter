# Source Review

This scaffold was created after inspecting the current npm package and upstream
adapter source. The goal is to replace npm-managed runtime launchers without
losing important protocol behavior.

## Sources Inspected

- `@agentclientprotocol/claude-agent-acp@0.47.0` npm tarball
- `agentclientprotocol/claude-agent-acp` source repository, shallow clone

## What the npm Package Does

The published package is a TypeScript/Node ACP adapter around the official
Claude Agent SDK. Unlike the Codex npm package, this package owns substantial
runtime behavior, not just platform dispatch.

## Behavior the Adapter Handles

The upstream adapter currently handles:

- ACP initialization, auth, session create/load/resume/list/fork/close/delete,
  prompt, cancel, modes, and config options
- terminal auth flows for local and remote environments
- gateway authentication metadata
- Claude Code executable resolution, including optional platform packages and
  musl/glibc selection
- managed/user/project/local settings resolution using the SDK merge engine
- trust filtering for escalating `permissions.defaultMode` values
- settings file watching and debounced reload
- a long-lived per-session query consumer
- prompt queueing with FIFO turn tracking
- cancellation, including a forced wake-up timer for wedged SDK streams
- orphan result skipping after cancelled queued prompts
- clear errors after the SDK query stream has ended
- local slash-command metadata stripping
- context-window tracking from model usage and model heuristics
- model selection, model allowlists, model aliases, and effort config options
- permission-mode availability by model, including `auto` gating
- permission request rendering and "Always Allow" labels with scoped rules
- AskUserQuestion and MCP elicitation mapping to ACP forms
- tool rendering for Bash, Read, Write, Edit, Grep, Glob, WebFetch, WebSearch,
  TodoWrite, TaskCreate/Update/List/Get, ExitPlanMode, memory recall, and MCP
  tool calls
- terminal output metadata for Bash
- raw SDK message forwarding under `_claude/sdkMessage`
- session history replay and message-id to SDK-UUID bookkeeping for future
  rewind/fork behavior

## Go Adapter Requirements

The Go replacement must preserve these semantics deliberately. A thin `claude`
process wrapper would be simpler, but it would not replace what the npm adapter
currently does.

Minimum safety requirements:

- no runtime `npx`
- no shell command construction
- fixed argv arrays
- explicit environment allowlist
- bounded JSON message size
- newline-delimited JSON-RPC only on stdout for subprocess protocol bridges
- bounded stdout/stderr capture for subprocess-backed paths
- deterministic platform release artifacts
- fake-Claude protocol tests for sessions, settings, permissions,
  elicitations, tools, cancellation, orphan results, and model/config behavior
  before Hecate switches to this adapter by default
