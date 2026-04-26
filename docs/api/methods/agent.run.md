# agent.run

Run the agent loop on a single prompt. The loop calls the model, optionally invokes tools, and continues until it produces a final answer or hits a stop condition. History accumulates inside the core, so calling `agent.run` again continues a multi-turn conversation.

## Direction

Wrapper → Core (request / response).

## Request

```json
{
  "jsonrpc": "2.0",
  "method": "agent.run",
  "params": { "prompt": "Summarize README.md", "max_turns": 8 },
  "id": 1
}
```

## Params

| field | type | required | description |
|---|---|---|---|
| `prompt` | string | yes | The user message that triggers the loop. |
| `max_turns` | integer | no | Per-call override of the configured loop limit. `0` / omitted means use the value from `agent.configure`. |

## Result

```json
{
  "jsonrpc": "2.0",
  "result": {
    "response": "README.md describes the project.",
    "reason": "completed",
    "turns": 3,
    "usage": {
      "prompt_tokens": 1234,
      "completion_tokens": 256,
      "total_tokens": 1490
    }
  },
  "id": 1
}
```

| field | type | description |
|---|---|---|
| `response` | string | Final assistant text. Empty when the loop stopped before producing one. |
| `reason` | string | Why the loop stopped — see the table below. |
| `turns` | integer | How many turns the loop consumed. |
| `usage.prompt_tokens` | integer | Cumulative prompt tokens across all model calls in this run. |
| `usage.completion_tokens` | integer | Cumulative completion tokens. |
| `usage.total_tokens` | integer | Sum of the above. |

### `reason` values

| reason | meaning |
|---|---|
| `completed` | Model produced a terminal answer. |
| `max_turns` | Loop hit `max_turns` without terminating. |
| `context_overflow` | Compaction could not bring the context below the limit. |
| `model_error` | LLM returned an unrecoverable error. |
| `aborted` | `agent.abort` was called. |
| `input_denied` | An input guard returned `deny`. |
| `output_blocked` | An output guard returned `deny`. |
| `user_fixable` | A user-fixable error was raised (auth failure etc). |
| `max_consecutive_failures` | Too many failed turns in a row (tool errors, verify failures, denials). |

## Errors

- `-32602 InvalidParams` — `prompt` missing or wrong type.
- `-32002 AgentBusy` — another `agent.run` is already in flight.
- `-32003 Aborted` — the run was aborted via `agent.abort` before producing a result.
- `-32603 InternalError` — unexpected core error.

See [errors.md](../errors.md) for the full table.

## Examples

### Raw JSON-RPC

```bash
echo '{"jsonrpc":"2.0","method":"agent.run","params":{"prompt":"Hello","max_turns":4},"id":1}' \
  | ./agent
```

### Python SDK

```python
from ai_agent import Agent

async with Agent() as agent:
    result = await agent.run("Summarize README.md", max_turns=8)
    print(result.response, result.reason, result.turns)
```

### TypeScript SDK

```typescript
import { Agent } from '@ai-agent/sdk';

await using agent = await Agent.open();
const result = await agent.run('Summarize README.md', { max_turns: 8 });
console.log(result.response, result.reason, result.turns);
```

## Related

- [agent.configure](./agent.configure.md) — set defaults for `max_turns`, system prompt, etc.
- [agent.abort](./agent.abort.md) — cancel an in-flight run.
- [notifications.md](./notifications.md) — `stream.delta` / `stream.end` / `context.status` fired during a run.
- [verify](../concepts/verify.md) — error classification and the four `reason` failure modes.
