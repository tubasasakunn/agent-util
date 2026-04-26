# guard.execute

Sent by the core to invoke a wrapper-registered guard. The shape of params depends on the stage.

## Direction

Core → Wrapper (request / response).

## Request

### Stage `input`

```json
{
  "jsonrpc": "2.0",
  "method": "guard.execute",
  "params": { "name": "no_secrets", "stage": "input", "input": "Hello" },
  "id": 21
}
```

### Stage `tool_call`

```json
{
  "jsonrpc": "2.0",
  "method": "guard.execute",
  "params": {
    "name": "verify_command",
    "stage": "tool_call",
    "tool_name": "bash",
    "args": { "command": "ls -la" }
  },
  "id": 22
}
```

### Stage `output`

```json
{
  "jsonrpc": "2.0",
  "method": "guard.execute",
  "params": {
    "name": "secret_filter",
    "stage": "output",
    "output": "The user's password is 12345"
  },
  "id": 23
}
```

## Params

| field | type | required | description |
|---|---|---|---|
| `name` | string | yes | Guard name (from `guard.register`). |
| `stage` | string | yes | One of `"input"`, `"tool_call"`, `"output"`. |
| `input` | string | stage=input | User input being checked. |
| `tool_name` | string | stage=tool_call | Tool the model wants to call. |
| `args` | object | stage=tool_call | Args the model proposed. |
| `output` | string | stage=output | Final assistant text being checked. |

## Result

```json
{
  "jsonrpc": "2.0",
  "result": {
    "decision": "deny",
    "reason": "matched secret pattern",
    "details": ["pattern: sk-..."]
  },
  "id": 21
}
```

| field | type | required | description |
|---|---|---|---|
| `decision` | string | yes | One of `"allow"`, `"deny"`, `"tripwire"`. |
| `reason` | string | no | Human-readable explanation. |
| `details` | string[] | no | Per-check breakdown. |

### Decision semantics

| decision | effect |
|---|---|
| `allow` | Stage continues normally. |
| `deny` | Stage soft-fails. Input → `reason: "input_denied"`. Tool call → result replaced with deny message, loop continues. Output → `reason: "output_blocked"`. |
| `tripwire` | Loop stops immediately with a `TripwireError` propagated to `agent.run`. Use sparingly. |

## Errors

- `-32603 InternalError` — wrapper handler raised an unhandled exception.

A wrapper that does not recognise `name` should still return a `decision: "allow"` to fail open, or surface `-32603` to fail closed.

## Examples

### Python SDK (handler shape)

```python
@input_guard(name="no_secrets")
def no_secrets(input: str) -> tuple[str, str]:
    if "password" in input.lower():
        return ("deny", "looks like a secret")
    return ("allow", "")
```

### TypeScript SDK (handler shape)

```typescript
const noSecrets = inputGuard({
  name: 'no_secrets',
  check: (input) =>
    /password/i.test(input)
      ? { decision: 'deny', reason: 'looks like a secret' }
      : { decision: 'allow' },
});
```

## Related

- [guard.register](./guard.register.md) — declares the names.
- [guards](../concepts/guards.md) — when each stage runs.
- [errors.md](../errors.md) — relationship between `tripwire` and the `Aborted` error.
