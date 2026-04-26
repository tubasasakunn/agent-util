# verifier.register

Register one or more wrapper-implemented verifiers. Names registered here can appear in [`agent.configure`](./agent.configure.md) `verify.verifiers`. The core invokes them via [`verifier.execute`](./verifier.execute.md) after every successful tool call.

## Direction

Wrapper → Core (request / response). Implements ADR-015.

## Request

```json
{
  "jsonrpc": "2.0",
  "method": "verifier.register",
  "params": {
    "verifiers": [
      { "name": "json_valid" },
      { "name": "non_empty" }
    ]
  },
  "id": 7
}
```

## Params

| field | type | required | description |
|---|---|---|---|
| `verifiers` | VerifierDefinition[] | yes | Verifiers to register. |

### `VerifierDefinition`

| field | type | required | description |
|---|---|---|---|
| `name` | string | yes | Unique verifier name. |

## Result

```json
{
  "jsonrpc": "2.0",
  "result": { "registered": 2 },
  "id": 7
}
```

| field | type | description |
|---|---|---|
| `registered` | integer | Number of verifiers accepted. |

## Errors

- `-32602 InvalidParams` — missing `name`, or duplicate name.

## Examples

### Raw JSON-RPC

```bash
echo '{"jsonrpc":"2.0","method":"verifier.register","params":{"verifiers":[{"name":"json_valid"}]},"id":7}' \
  | ./agent
```

### Python SDK

```python
from ai_agent import Agent, AgentConfig, VerifyConfig, verifier

@verifier(name="json_valid")
def check_json(tool_name: str, args: dict, result: str) -> tuple[bool, str]:
    import json
    try:
        json.loads(result)
        return (True, "ok")
    except Exception as exc:
        return (False, f"invalid JSON: {exc}")

async with Agent() as agent:
    await agent.register_verifiers(check_json)
    await agent.configure(AgentConfig(verify=VerifyConfig(verifiers=["json_valid"])))
```

### TypeScript SDK

```typescript
import { Agent, verifier } from '@ai-agent/sdk';

const jsonValid = verifier({
  name: 'json_valid',
  check: ({ result }) => {
    try {
      JSON.parse(result);
      return { passed: true };
    } catch (err) {
      return { passed: false, summary: String(err) };
    }
  },
});

await using agent = await Agent.open();
await agent.registerVerifiers(jsonValid);
await agent.configure({ verify: { verifiers: ['json_valid'] } });
```

## Related

- [verifier.execute](./verifier.execute.md) — the callback the wrapper handles.
- [verify](../concepts/verify.md) — Plan-Execute-Verify cycle and retry semantics.
- [builtins.md](../builtins.md) — built-in verifier names (registration not required).
- [ADR-015: Remote guard / verifier pattern](../../../.claude/skills/decisions/015-remote-guard-verifier-pattern.md)
