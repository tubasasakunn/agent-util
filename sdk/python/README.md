# ai-agent Python SDK

Thin async Python client for the [ai-agent](../../) Go harness. Speaks
JSON-RPC 2.0 over stdio with a child `agent --rpc` process.

* No third-party runtime dependencies (stdlib `asyncio` + `subprocess` + `json`).
* Async-first; `@tool` decorator works for sync **and** async functions.
* Mirrors `pkg/protocol/methods.go` and `docs/openrpc.json` exactly.

## Install

```bash
# 1) Build the Go agent binary (from the repo root)
go build -o agent ./cmd/agent/

# 2) Install the SDK in editable mode (from sdk/python/)
pip install -e .

# 3) (optional) install test extras
pip install -e ".[test]"
```

The SDK supports Python 3.10+.

## Quickstart

### 1. Minimal run

```python
import asyncio
from ai_agent import Agent

async def main() -> None:
    async with Agent(binary_path="./agent") as agent:
        result = await agent.run("こんにちは")
        print(result.response)

asyncio.run(main())
```

`Agent.run` returns an `AgentResult` with `.response`, `.reason`, `.turns`
and `.usage` (an object with `prompt_tokens`, `completion_tokens`,
`total_tokens`).

### 2. Register a tool

```python
import asyncio
from pathlib import Path
from ai_agent import Agent, tool

@tool(description="Read a file from disk", read_only=True)
def read_file(path: str) -> str:
    return Path(path).read_text()

async def main() -> None:
    async with Agent(binary_path="./agent") as agent:
        await agent.register_tools(read_file)
        result = await agent.run("Read README.md and summarize it.")
        print(result.response)

asyncio.run(main())
```

The `@tool` decorator generates a JSON Schema from the function signature
(see [`ai_agent/tool.py`](./ai_agent/tool.py) for the type-mapping table).
`async def` tool functions are called natively; sync ones are dispatched on
the default executor so the JSON-RPC reader loop never blocks.

### 3. Configure guards / permissions / streaming

```python
import asyncio
from ai_agent import (
    Agent, AgentConfig, GuardsConfig, PermissionConfig, StreamingConfig,
    input_guard,
)

@input_guard(name="no_secrets")
def reject_secrets(input: str) -> tuple[str, str]:
    if "password" in input.lower():
        return ("deny", "looks like a secret")
    return ("allow", "")

async def main() -> None:
    async with Agent(binary_path="./agent") as agent:
        await agent.register_guards(reject_secrets)
        await agent.configure(AgentConfig(
            max_turns=8,
            permission=PermissionConfig(enabled=True, allow=["read_file"]),
            guards=GuardsConfig(input=["no_secrets"]),
            streaming=StreamingConfig(enabled=True),
        ))
        await agent.run(
            "Walk me through the README.",
            stream=lambda chunk, turn: print(chunk, end="", flush=True),
        )

asyncio.run(main())
```

## API surface

```python
class Agent:
    def __init__(self, binary_path: str = "agent",
                 *, env: dict[str, str] | None = None,
                 cwd: str | None = None) -> None: ...

    async def __aenter__(self) -> "Agent": ...
    async def __aexit__(self, *exc) -> None: ...

    async def configure(self, config: AgentConfig) -> list[str]: ...
    async def run(self, prompt: str, *,
                  max_turns: int | None = None,
                  stream: Callable[[str, int], None] | None = None,
                  on_status: Callable[[float, int, int], None] | None = None,
                  timeout: float | None = None) -> AgentResult: ...
    async def abort(self, reason: str = "") -> bool: ...
    async def register_tools(self, *tools) -> int: ...
    async def register_guards(self, *guards) -> int: ...
    async def register_verifiers(self, *verifiers) -> int: ...
    async def register_mcp(self, command: str | None = None,
                           args: list[str] | tuple[str, ...] = (),
                           *, env: dict[str, str] | None = None,
                           transport: str = "stdio",
                           url: str | None = None) -> list[str]: ...
```

Full configuration schema lives in [`ai_agent/config.py`](./ai_agent/config.py)
and matches `AgentConfigureParams` in `docs/openrpc.json`.

### Errors

| Class           | JSON-RPC code | When                                        |
| --------------- | ------------- | ------------------------------------------- |
| `AgentBusy`     | `-32002`      | `agent.run` while another run is in flight  |
| `AgentAborted`  | `-32003`      | A run was cancelled via `agent.abort`       |
| `ToolError`     | `-32000/-32001` | Tool not found / tool execution failed     |
| `GuardDenied`   | n/a           | An input guard returned `deny` / `tripwire` |
| `AgentError`    | other         | Base class for everything from the SDK      |

## Tests

```bash
# All unit tests (no agent binary required)
cd sdk/python
python -m pytest

# Add the e2e tests against a real binary
go build -o ../../agent ../../cmd/agent/
AGENT_BINARY=$(pwd)/../../agent python -m pytest
```

## References

* [`docs/openrpc.json`](../../docs/openrpc.json) — full OpenRPC 1.2.6 spec.
* [`docs/schemas/`](../../docs/schemas/) — JSON Schemas for every type.
* [`pkg/protocol/methods.go`](../../pkg/protocol/methods.go) — Go source of truth.
* ADR-001 (JSON-RPC over stdio) and ADR-013 (RemoteTool adapter pattern).
