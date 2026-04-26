# mcp.register

Spawn or connect to an external MCP (Model Context Protocol) server and expose its tools to the agent. Tool dispatch is handled inside the core; the wrapper does not see `tool.execute` calls for MCP-sourced tools.

## Direction

Wrapper → Core (request / response).

## Request — stdio transport

```json
{
  "jsonrpc": "2.0",
  "method": "mcp.register",
  "params": {
    "transport": "stdio",
    "command": "mcp-fs",
    "args": ["--root", "."],
    "env": { "MCP_LOG": "info" }
  },
  "id": 5
}
```

## Request — SSE transport

```json
{
  "jsonrpc": "2.0",
  "method": "mcp.register",
  "params": {
    "transport": "sse",
    "url": "http://localhost:9090/mcp"
  },
  "id": 5
}
```

## Params

| field | type | required | description |
|---|---|---|---|
| `transport` | string (`"stdio"` \| `"sse"`) | no | Default `"stdio"`. |
| `command` | string | stdio only | Executable to spawn. |
| `args` | string[] | no | CLI arguments for `command`. |
| `env` | map[string]string | no | Extra env vars for the spawned process. |
| `url` | string | sse only | SSE endpoint URL. |

## Result

```json
{
  "jsonrpc": "2.0",
  "result": { "tools": ["fs.read", "fs.write"] },
  "id": 5
}
```

| field | type | description |
|---|---|---|
| `tools` | string[] | Names registered into the core's tool registry. |

## Errors

- `-32602 InvalidParams` — missing `command` for stdio / `url` for sse.
- `-32603 InternalError` — server failed to start, handshake failed, or initial tool listing failed.

## Examples

### Raw JSON-RPC

```bash
echo '{"jsonrpc":"2.0","method":"mcp.register","params":{"transport":"stdio","command":"mcp-fs","args":["--root","."]},"id":5}' \
  | ./agent
```

### Python SDK

```python
async with Agent() as agent:
    tools = await agent.register_mcp(
        transport="stdio",
        command="mcp-fs",
        args=["--root", "."],
    )
    print("registered:", tools)
```

### TypeScript SDK

```typescript
await using agent = await Agent.open();
const tools = await agent.registerMcp({
  transport: 'stdio',
  command: 'mcp-fs',
  args: ['--root', '.'],
});
console.log('registered:', tools);
```

## Related

- [tool.register](./tool.register.md) — alternative way to expose tools (wrapper-implemented).
- [permission](../concepts/permission.md) — applies to MCP tools too. They report `read_only` based on the MCP server's metadata.
