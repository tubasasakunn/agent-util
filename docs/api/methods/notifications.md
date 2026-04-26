# Notifications

Notifications are JSON-RPC messages with no `id`. The wrapper must not respond. All three notifications below flow Core → Wrapper.

| Method | Fires when | Toggle |
|---|---|---|
| [`stream.delta`](#streamdelta) | Each text delta from the streaming LLM | `streaming.enabled` |
| [`stream.end`](#streamend) | Once per `agent.run`, after the loop terminates | always when streaming is on |
| [`context.status`](#contextstatus) | Each turn boundary and after compaction | `streaming.context_status` |

See [streaming](../concepts/streaming.md) for the firing rules and ADR-014 for the design.

## stream.delta

Incremental text chunk produced by the model. Concatenating every `text` for a given `turn` reconstructs that turn's full assistant message.

### Notification

```json
{
  "jsonrpc": "2.0",
  "method": "stream.delta",
  "params": { "text": "Hello", "turn": 1 }
}
```

### Params

| field | type | required | description |
|---|---|---|---|
| `text` | string | yes | The delta. May be a single character. |
| `turn` | integer | no | Turn number (1-indexed). `0` = unknown. |

### Examples

**Python SDK** — pass an `on_delta` callback:

```python
def on_delta(text: str, turn: int) -> None:
    print(text, end="", flush=True)

await agent.run("Stream me", stream=on_delta)
```

**TypeScript SDK** — use `runStream`:

```typescript
for await (const ev of agent.runStream('Stream me')) {
  if (ev.kind === 'delta') process.stdout.write(ev.text);
}
```

## stream.end

Sent once after the loop terminates (only when streaming is enabled).

### Notification

```json
{
  "jsonrpc": "2.0",
  "method": "stream.end",
  "params": { "reason": "completed", "turns": 3 }
}
```

### Params

| field | type | required | description |
|---|---|---|---|
| `reason` | string | yes | Same vocabulary as `agent.run` result `reason`. |
| `turns` | integer | yes | Total turns consumed. |

## context.status

Reports current context window usage. Fires:

- Right after `agent.run` accepts the input,
- Before each turn,
- After compaction completes.

### Notification

```json
{
  "jsonrpc": "2.0",
  "method": "context.status",
  "params": { "usage_ratio": 0.5, "token_count": 2048, "token_limit": 4096 }
}
```

### Params

| field | type | required | description |
|---|---|---|---|
| `usage_ratio` | number | yes | `token_count / token_limit`. |
| `token_count` | integer | yes | Estimated tokens currently in the context. |
| `token_limit` | integer | yes | Configured ceiling. |

### Examples

**Python SDK**:

```python
def on_status(ratio: float, count: int, limit: int) -> None:
    print(f"[{count}/{limit} = {ratio:.0%}]")

await agent.run("...", on_status=on_status)
```

**TypeScript SDK**:

```typescript
for await (const ev of agent.runStream('...')) {
  if (ev.kind === 'status') {
    console.log(`[${ev.tokenCount}/${ev.tokenLimit} = ${(ev.usageRatio * 100).toFixed(0)}%]`);
  }
}
```

## Related

- [streaming](../concepts/streaming.md) — when each notification fires and the streaming model.
- [compaction](../concepts/compaction.md) — `context.status` is also fired after compaction.
- [agent.configure](./agent.configure.md) — the `streaming` block toggles these notifications.
- [ADR-014: Streaming notification config](../../../.claude/skills/decisions/014-streaming-notification-config.md)
