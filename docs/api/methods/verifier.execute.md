# verifier.execute

Sent by the core after a tool call completes successfully (no execution error). The wrapper inspects the tool's output and decides whether the result is acceptable.

## Direction

Core → Wrapper (request / response).

## Request

```json
{
  "jsonrpc": "2.0",
  "method": "verifier.execute",
  "params": {
    "name": "json_valid",
    "tool_name": "fetch_json",
    "args": { "url": "https://api.example.com/v1/data" },
    "result": "{\"ok\":true}"
  },
  "id": 31
}
```

## Params

| field | type | required | description |
|---|---|---|---|
| `name` | string | yes | Verifier name (from `verifier.register`). |
| `tool_name` | string | yes | Tool that produced `result`. |
| `args` | object | no | Original tool args. |
| `result` | string | yes | Tool's `content` field. |

## Result

```json
{
  "jsonrpc": "2.0",
  "result": {
    "passed": false,
    "summary": "JSON parse error at line 1",
    "details": ["expected '}' got EOF"]
  },
  "id": 31
}
```

| field | type | required | description |
|---|---|---|---|
| `passed` | boolean | yes | `true` = acceptable, `false` = retry the model. |
| `summary` | string | no | One-line reason. |
| `details` | string[] | no | Per-check breakdown. |

When `passed: false`, the core injects a `[Verification Failed] {summary}` user message into the loop so the model can correct itself. See [verify](../concepts/verify.md).

## Errors

- `-32603 InternalError` — wrapper handler raised an unhandled exception. Such errors are logged and the verifier is treated as if it had passed (skip-on-error policy in the registry).

## Examples

### Python SDK (handler shape)

```python
@verifier(name="json_valid")
def check_json(tool_name: str, args: dict, result: str) -> tuple[bool, str]:
    import json
    try:
        json.loads(result)
        return (True, "valid JSON")
    except Exception as exc:
        return (False, f"invalid JSON: {exc}")
```

### TypeScript SDK (handler shape)

```typescript
const jsonValid = verifier({
  name: 'json_valid',
  check: ({ result }) => {
    try {
      JSON.parse(result);
      return { passed: true, summary: 'valid JSON' };
    } catch (err) {
      return { passed: false, summary: String(err) };
    }
  },
});
```

## Related

- [verifier.register](./verifier.register.md) — declares the names.
- [verify](../concepts/verify.md) — when verification runs and how retries work.
