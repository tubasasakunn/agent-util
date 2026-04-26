# agent.configure

Apply harness configuration. Call once before `agent.run`. Omitted (`null`) fields keep their existing value; nested config blocks toggle their feature via `enabled`.

## Direction

Wrapper → Core (request / response).

## Request

```json
{
  "jsonrpc": "2.0",
  "method": "agent.configure",
  "params": {
    "max_turns": 10,
    "system_prompt": "You are a concise assistant.",
    "token_limit": 8192,
    "streaming": { "enabled": true, "context_status": true },
    "guards": { "input": ["prompt_injection"], "output": ["secret_leak"] },
    "permission": { "enabled": true, "deny": ["dangerous_tool"] }
  },
  "id": 3
}
```

## Params

Top-level scalars (all optional, `null` = keep current value):

| field | type | description |
|---|---|---|
| `max_turns` | integer | Default loop limit. Per-call override on `agent.run`. |
| `system_prompt` | string | Static system prompt (prepended to every model call). |
| `token_limit` | integer | Hard ceiling on the context window in tokens. |
| `work_dir` | string | Working directory injected into tools via `context.Context` (ADR-008). |

Feature blocks (each carries `enabled` to toggle):

| block | feature | concept |
|---|---|---|
| `delegate` | `delegate_task` virtual tool | [delegation.md](../concepts/delegation.md) |
| `coordinator` | `coordinate_tasks` virtual tool | [delegation.md](../concepts/delegation.md) |
| `compaction` | four-stage context cascade | [compaction.md](../concepts/compaction.md) |
| `permission` | tool permission pipeline | [permission.md](../concepts/permission.md) |
| `guards` | three-stage guardrails | [guards.md](../concepts/guards.md) |
| `verify` | tool result verification | [verify.md](../concepts/verify.md) |
| `tool_scope` | per-call tool subset selection | — |
| `reminder` | system reminder injection | — |
| `streaming` | stream.delta / context.status notifications | [streaming.md](../concepts/streaming.md) |

### `delegate` / `coordinator`

| field | type | description |
|---|---|---|
| `enabled` | boolean | Toggle the virtual tool. |
| `max_chars` | integer | Cap on subagent result size (truncated past this; ADR-007). |

### `compaction`

| field | type | description |
|---|---|---|
| `enabled` | boolean | Toggle the cascade. |
| `budget_max_chars` | integer | Per-tool-result cap before truncation (Stage 1). |
| `keep_last` | integer | Most-recent messages excluded from compaction (Stages 2–4). |
| `target_ratio` | number | Stop the cascade once usage drops below this fraction. |
| `summarizer` | string | Built-in summarizer name. Currently only `"llm"`. See [builtins.md](../builtins.md). |

### `permission`

| field | type | description |
|---|---|---|
| `enabled` | boolean | Toggle the pipeline. |
| `deny` | string[] | Tool names always denied. `"*"` denies all. |
| `allow` | string[] | Tool names always allowed. `"*"` allows all. |

### `guards`

| field | type | description |
|---|---|---|
| `input` | string[] | Built-in or wrapper-registered input-stage guard names. |
| `tool_call` | string[] | Tool-call-stage guard names. |
| `output` | string[] | Output-stage guard names. |

See [builtins.md](../builtins.md) for the built-in names.

### `verify`

| field | type | description |
|---|---|---|
| `verifiers` | string[] | Built-in or wrapper-registered verifier names. |
| `max_step_retries` | integer | Transient-error retries per turn. |
| `max_consecutive_failures` | integer | Successive failed turns before stop. |

### `tool_scope`

| field | type | description |
|---|---|---|
| `max_tools` | integer | Maximum tools surfaced to the router per turn. |
| `include_always` | string[] | Tools always included regardless of scoping. |

### `reminder`

| field | type | description |
|---|---|---|
| `threshold` | integer | Inject reminder once history reaches this length. |
| `content` | string | Reminder text inserted before the last user message. |

### `streaming`

| field | type | description |
|---|---|---|
| `enabled` | boolean | Emit `stream.delta` notifications. |
| `context_status` | boolean | Emit `context.status` notifications. |

## Result

```json
{
  "jsonrpc": "2.0",
  "result": { "applied": ["max_turns", "streaming"] },
  "id": 3
}
```

| field | type | description |
|---|---|---|
| `applied` | string[] | Names of top-level fields that took effect this call. |

## Errors

- `-32602 InvalidParams` — malformed JSON or wrong types.
- `-32603 InternalError` — unexpected core error (e.g. unknown built-in name).

## Examples

### Raw JSON-RPC

```bash
echo '{"jsonrpc":"2.0","method":"agent.configure","params":{"max_turns":10,"streaming":{"enabled":true}},"id":3}' \
  | ./agent
```

### Python SDK

```python
from ai_agent import Agent, AgentConfig, GuardsConfig, StreamingConfig

async with Agent() as agent:
    await agent.configure(AgentConfig(
        max_turns=10,
        guards=GuardsConfig(input=["prompt_injection"], output=["secret_leak"]),
        streaming=StreamingConfig(enabled=True, context_status=True),
    ))
```

### TypeScript SDK

```typescript
await using agent = await Agent.open();
await agent.configure({
  max_turns: 10,
  guards: { input: ['prompt_injection'], output: ['secret_leak'] },
  streaming: { enabled: true, context_status: true },
});
```

## Related

- [agent.run](./agent.run.md) — uses the configured defaults.
- [permission.md](../concepts/permission.md), [guards.md](../concepts/guards.md), [verify.md](../concepts/verify.md), [compaction.md](../concepts/compaction.md), [streaming.md](../concepts/streaming.md) — semantics of each feature block.
- [builtins.md](../builtins.md) — names available for `guards.*`, `verify.verifiers`, `compaction.summarizer`.
