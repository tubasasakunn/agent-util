# ai-agent API Specification

This directory contains the public API specification for the `ai-agent` JSON-RPC interface. Wrapper authors (Python / JS / Rust / etc.) should generate clients and types from these files rather than hand-write them.

## Files

- `openrpc.json` — OpenRPC 1.2.6 document covering every JSON-RPC method exposed by the core. Methods, params, results, and errors are all defined. This is the source of truth for what wrappers can call and what callbacks they may receive.
- `schemas/*.json` — Standalone JSON Schema (Draft 2020-12) files for every named type used by the API. Each file has its own `$id` and `$schema`, so they can be fed directly into code generators.

## Methods at a glance

Wrapper-to-core (request/response):

- `agent.run` — run the agent loop on a prompt
- `agent.abort` — cancel the in-flight agent loop
- `agent.configure` — configure the harness (must be called before `agent.run`)
- `tool.register` — register wrapper-implemented tools
- `mcp.register` — register an external MCP server as a tool source
- `guard.register` — register wrapper-implemented guards
- `verifier.register` — register wrapper-implemented verifiers

Core-to-wrapper (request/response):

- `tool.execute` — invoke a registered wrapper tool
- `guard.execute` — invoke a registered wrapper guard
- `verifier.execute` — invoke a registered wrapper verifier
- `llm.execute` — delegate a ChatCompletion to the wrapper. Activated by
  `agent.configure({ llm: { mode: "remote" } })`. The wrapper receives an
  OpenAI-compatible `ChatRequest` and must return an OpenAI-compatible
  `ChatResponse`; use it to plug in any backend (Anthropic, Bedrock, ollama,
  mock, ...). See [ADR-016](../.claude/skills/decisions/016-llm-execute-reverse-rpc.md).

Core-to-wrapper (notifications, no `id`, no response):

- `stream.delta` — incremental model output text
- `stream.end` — agent loop finished
- `context.status` — current context window usage

## Generating wrapper clients

### quicktype (TypeScript / Python / Go / Rust / etc.)

```sh
# Generate a TypeScript client from one of the schemas.
quicktype docs/schemas/AgentRunParams.json -o agent_run_params.ts --src-lang schema

# Or feed the entire OpenRPC document and emit all named types.
quicktype docs/openrpc.json -o api_types.py --lang python --src-lang schema
```

### Python (datamodel-code-generator → pydantic)

```sh
datamodel-codegen \
  --input docs/schemas/ \
  --input-file-type jsonschema \
  --output ai_agent_types.py
```

### TypeScript (json-schema-to-typescript)

```sh
json2ts -i 'docs/schemas/*.json' -o ./types/
```

### Rust (typify / schemars)

```sh
typify docs/schemas/AgentRunParams.json --output src/types.rs
```

## Versioning

The API version is tracked in `openrpc.json` `info.version` (currently `0.1.0`). Per `pkg/protocol/` policy, breaking changes to existing methods are introduced under a new method name (e.g. `agent.run.v2`) so existing wrappers continue to work. Additive changes (new optional fields, new methods) are made in place and bump the minor version.

See `CLAUDE.md` and `.claude/rules/protocol.md` for the full policy.

## Source of truth

The Go type definitions in `pkg/protocol/methods.go` are the authoritative source. `openrpc.json` is hand-mirrored from those types and verified by `pkg/protocol/spec_test.go`, which checks that the method list and required-field sets match.
