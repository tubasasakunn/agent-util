# tool.register

Register one or more wrapper-implemented tools with the core. Registered tools become invokable from the agent loop and are dispatched back to the wrapper via [`tool.execute`](./tool.execute.md). Multiple calls are additive.

## Direction

Wrapper → Core (request / response).

## Request

```json
{
  "jsonrpc": "2.0",
  "method": "tool.register",
  "params": {
    "tools": [
      {
        "name": "read_file",
        "description": "Read a file from the workspace",
        "parameters": {
          "type": "object",
          "properties": { "path": { "type": "string" } },
          "required": ["path"]
        },
        "read_only": true
      }
    ]
  },
  "id": 4
}
```

## Params

| field | type | required | description |
|---|---|---|---|
| `tools` | ToolDefinition[] | yes | Tools to register. |

### `ToolDefinition`

| field | type | required | description |
|---|---|---|---|
| `name` | string | yes | Unique tool name. Used by the router and by `tool.execute`. |
| `description` | string | yes | One-line description shown to the model. |
| `parameters` | object (JSON Schema) | yes | JSON Schema for the args object. Used to format the router prompt. |
| `read_only` | boolean | no | When `true` the [permission pipeline](../concepts/permission.md) auto-approves. Default `false`. |

## Result

```json
{
  "jsonrpc": "2.0",
  "result": { "registered": 1 },
  "id": 4
}
```

| field | type | description |
|---|---|---|
| `registered` | integer | Number of tools accepted in this call. |

## Errors

- `-32602 InvalidParams` — missing `name` / `description` / `parameters`, or invalid JSON Schema.

Duplicate names are rejected at registration time and surface as `-32602`.

## Examples

### Raw JSON-RPC

```bash
echo '{"jsonrpc":"2.0","method":"tool.register","params":{"tools":[{"name":"echo","description":"Echo input","parameters":{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]},"read_only":true}]},"id":4}' \
  | ./agent
```

### Python SDK

```python
from ai_agent import Agent, tool

@tool(description="Read a file", read_only=True)
def read_file(path: str) -> str:
    with open(path) as f:
        return f.read()

async with Agent() as agent:
    await agent.register_tools(read_file)
```

### TypeScript SDK

```typescript
import { Agent, tool } from '@ai-agent/sdk';

const readFile = tool({
  name: 'read_file',
  description: 'Read a file',
  parameters: {
    type: 'object',
    properties: { path: { type: 'string' } },
    required: ['path'],
  },
  read_only: true,
  handler: async ({ path }) => (await fs.readFile(path)).toString(),
});

await using agent = await Agent.open();
await agent.registerTools(readFile);
```

## Related

- [tool.execute](./tool.execute.md) — the callback the wrapper handles after registration.
- [mcp.register](./mcp.register.md) — alternative tool source via an MCP server.
- [permission](../concepts/permission.md) — `read_only: true` auto-approves in the pipeline.
- [ADR-013: RemoteTool + PendingRequests](../../../.claude/skills/decisions/013-rpc-remote-tool-pending-requests.md)
