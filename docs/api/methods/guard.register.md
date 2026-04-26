# guard.register

Register one or more wrapper-implemented guards. After registration the names can appear in [`agent.configure`](./agent.configure.md) `guards.input` / `guards.tool_call` / `guards.output`. The core invokes them via [`guard.execute`](./guard.execute.md).

## Direction

Wrapper → Core (request / response). Implements ADR-015.

## Request

```json
{
  "jsonrpc": "2.0",
  "method": "guard.register",
  "params": {
    "guards": [
      { "name": "no_secrets", "stage": "input" },
      { "name": "verify_command", "stage": "tool_call" }
    ]
  },
  "id": 6
}
```

## Params

| field | type | required | description |
|---|---|---|---|
| `guards` | GuardDefinition[] | yes | Guards to register. |

### `GuardDefinition`

| field | type | required | description |
|---|---|---|---|
| `name` | string | yes | Unique guard name. |
| `stage` | string | yes | One of `"input"`, `"tool_call"`, `"output"`. |

## Result

```json
{
  "jsonrpc": "2.0",
  "result": { "registered": 2 },
  "id": 6
}
```

| field | type | description |
|---|---|---|
| `registered` | integer | Number of guards accepted. |

## Errors

- `-32602 InvalidParams` — missing fields, unknown `stage`, or duplicate name within the same stage.

## Examples

### Raw JSON-RPC

```bash
echo '{"jsonrpc":"2.0","method":"guard.register","params":{"guards":[{"name":"no_secrets","stage":"input"}]},"id":6}' \
  | ./agent
```

### Python SDK

```python
from ai_agent import Agent, AgentConfig, GuardsConfig, input_guard

@input_guard(name="internal_keyword")
def reject_internal(input: str) -> tuple[str, str]:
    if "internal-only" in input.lower():
        return ("deny", "input contains the 'internal-only' marker")
    return ("allow", "")

async with Agent() as agent:
    await agent.register_guards(reject_internal)
    await agent.configure(AgentConfig(guards=GuardsConfig(input=["internal_keyword"])))
```

### TypeScript SDK

```typescript
import { Agent, inputGuard } from '@ai-agent/sdk';

const noSecrets = inputGuard({
  name: 'no_secrets',
  check: (input) =>
    /password|secret/i.test(input)
      ? { decision: 'deny', reason: 'looks like a secret' }
      : { decision: 'allow' },
});

await using agent = await Agent.open();
await agent.registerGuards(noSecrets);
await agent.configure({ guards: { input: ['no_secrets'] } });
```

## Related

- [guard.execute](./guard.execute.md) — the callback the wrapper handles.
- [guards](../concepts/guards.md) — three-stage pipeline and tripwire semantics.
- [builtins.md](../builtins.md) — built-in guard names (registration not required).
- [ADR-015: Remote guard / verifier pattern](../../../.claude/skills/decisions/015-remote-guard-verifier-pattern.md)
