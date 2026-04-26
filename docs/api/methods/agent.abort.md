# agent.abort

Cancel the currently running `agent.run`. Returns immediately; the in-flight run completes with `reason: "aborted"` (or returns a `-32003 Aborted` error, depending on timing).

## Direction

Wrapper → Core (request / response).

## Request

```json
{
  "jsonrpc": "2.0",
  "method": "agent.abort",
  "params": { "reason": "user requested" },
  "id": 2
}
```

## Params

| field | type | required | description |
|---|---|---|---|
| `reason` | string | no | Human-readable cancellation cause. Stored in audit logs; not used for control flow. |

## Result

```json
{
  "jsonrpc": "2.0",
  "result": { "aborted": true },
  "id": 2
}
```

| field | type | description |
|---|---|---|
| `aborted` | boolean | `true` when the cancel signal was delivered. `false` when no run was active. |

## Errors

- `-32603 InternalError` — failed to deliver the cancel signal.

`agent.abort` does not return `-32602` — empty params are valid.

## Examples

### Raw JSON-RPC

```bash
echo '{"jsonrpc":"2.0","method":"agent.abort","params":{"reason":"timeout"},"id":99}' \
  | ./agent
```

### Python SDK

```python
async with Agent() as agent:
    task = asyncio.create_task(agent.run("long task..."))
    await asyncio.sleep(2)
    await agent.abort(reason="timeout")
    result = await task  # result.reason == "aborted"
```

### TypeScript SDK

```typescript
await using agent = await Agent.open();
const promise = agent.run('long task...');
setTimeout(() => agent.abort('timeout'), 2000);
const result = await promise; // result.reason === 'aborted'
```

## Related

- [agent.run](./agent.run.md) — the call being aborted.
- [errors.md](../errors.md) — recovery for `-32003 Aborted`.
