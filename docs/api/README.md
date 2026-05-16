# ai-agent API Reference

Human-readable reference for the `ai-agent` JSON-RPC API. Wrapper authors and SDK users can read this without consulting the OpenRPC document.

For machine-readable specs see [`../openrpc.json`](../openrpc.json) and [`../schemas/`](../schemas/).

> **Looking for a browser-only setup?** The pure-TypeScript browser SDK at [`../../sdk/js-browser/`](../../sdk/js-browser/) runs the same agent loop client-side via WebLLM — no Go core, no JSON-RPC, no subprocess. Most of the concepts below (guards, verifiers, permission, router, streaming) carry over identically.

## Contents

- [overview.md](./overview.md) — Architecture, communication model, lifecycle, versioning.
- [errors.md](./errors.md) — Every error code and recommended handling.
- [builtins.md](./builtins.md) — Built-in guard / verifier / summarizer names.

### Methods

Wrapper-to-core (request/response):

- [agent.run](./methods/agent.run.md) — run the loop on a prompt
- [agent.abort](./methods/agent.abort.md) — cancel the in-flight run
- [agent.configure](./methods/agent.configure.md) — configure the harness
- [tool.register](./methods/tool.register.md) — register wrapper-implemented tools
- [mcp.register](./methods/mcp.register.md) — register an external MCP server
- [guard.register](./methods/guard.register.md) — register wrapper-implemented guards
- [verifier.register](./methods/verifier.register.md) — register wrapper-implemented verifiers

Core-to-wrapper (request/response):

- [tool.execute](./methods/tool.execute.md) — invoke a wrapper-side tool
- [guard.execute](./methods/guard.execute.md) — invoke a wrapper-side guard
- [verifier.execute](./methods/verifier.execute.md) — invoke a wrapper-side verifier
- [llm.execute](./methods/llm.execute.md) — delegate a `ChatCompletion` to the wrapper (when `llm.mode="remote"`)

Core-to-wrapper (notifications):

- [notifications.md](./methods/notifications.md) — `stream.delta` / `stream.end` / `context.status`

### Concepts

- [permission.md](./concepts/permission.md) — deny → allow → readOnly → ask → fail-closed pipeline
- [guards.md](./concepts/guards.md) — three-stage guardrails and tripwires
- [verify.md](./concepts/verify.md) — Plan-Execute-Verify cycle and error classes
- [compaction.md](./concepts/compaction.md) — four-stage context cascade
- [streaming.md](./concepts/streaming.md) — `stream.delta` and `context.status` semantics
- [delegation.md](./concepts/delegation.md) — `delegate_task` / `coordinate_tasks` / Worktree / Ralph Wiggum
- [lifecycle.md](./concepts/lifecycle.md) — subprocess spawn → configure → run → abort → close

## How to read this reference

1. Start with [overview.md](./overview.md) to learn how wrappers talk to the core.
2. Read [lifecycle.md](./concepts/lifecycle.md) to understand the call ordering.
3. Open the method pages on demand. Each page is self-contained.
4. When configuring a feature, jump to its concept page (e.g. `concepts/guards.md`) for semantics.

## Source of truth

`pkg/protocol/methods.go` and `pkg/protocol/errors.go` are the authoritative Go type definitions. All numbers, names, and JSON shapes in this reference are mirrored from those files.
