# tool.execute

Sent by the core when the agent loop selects a wrapper-registered tool. The wrapper executes the tool with the supplied args and returns the textual result.

## Direction

Core → Wrapper (request / response). Implements ADR-013 (RemoteTool + PendingRequests).

## Request

```json
{
  "jsonrpc": "2.0",
  "method": "tool.execute",
  "params": {
    "name": "read_file",
    "args": { "path": "README.md" }
  },
  "id": 17
}
```

## Params

| field | type | required | description |
|---|---|---|---|
| `name` | string | yes | Name from the original `tool.register`. |
| `args` | object | yes | Arguments matching the tool's parameter schema. |

## Result

The wrapper must reply with:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "content": "file contents...",
    "is_error": false,
    "metadata": { "size": 1234 }
  },
  "id": 17
}
```

| field | type | required | description |
|---|---|---|---|
| `content` | string | yes | Tool output the model will see. |
| `is_error` | boolean | no | When `true`, the loop treats the result as an error (counts toward `max_consecutive_failures`). |
| `metadata` | object | no | Free-form metadata; opaque to the core. |

## Errors

The wrapper may reply with:

- `-32000 ToolNotFound` — the wrapper has no implementation for `name`.
- `-32001 ToolExecFailed` — execution raised an exception.

Returning an error response is equivalent to returning `is_error: true` for the purpose of consecutive-failure counting.

## Examples

### Python SDK (handler)

The Python SDK dispatches `tool.execute` automatically based on functions decorated with `@tool`. Manual handling:

```python
async def handle_tool_execute(name: str, args: dict) -> dict:
    if name == "read_file":
        try:
            return {"content": open(args["path"]).read()}
        except Exception as exc:
            return {"content": str(exc), "is_error": True}
    raise ToolNotFound(name)
```

### TypeScript SDK (handler)

```typescript
const readFile = tool({
  name: 'read_file',
  description: 'Read a file',
  parameters: { /* ... */ },
  handler: async ({ path }) => {
    try {
      return { content: await fs.readFile(path, 'utf8') };
    } catch (err) {
      return { content: String(err), is_error: true };
    }
  },
});
```

## Related

- [tool.register](./tool.register.md) — declares the names the core may dispatch.
- [permission](../concepts/permission.md) — checked before `tool.execute` is sent.
- [guards](../concepts/guards.md) — the `tool_call` stage runs before `tool.execute`.
- [verify](../concepts/verify.md) — runs after the wrapper returns the result.
- [ADR-013: RemoteTool + PendingRequests](../../../.claude/skills/decisions/013-rpc-remote-tool-pending-requests.md)
