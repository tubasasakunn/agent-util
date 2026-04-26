"""Register a single read-only tool and let the agent invoke it."""

from __future__ import annotations

import asyncio
import os
from pathlib import Path

from ai_agent import Agent, AgentConfig, PermissionConfig, tool

REPO_ROOT = Path(__file__).resolve().parents[3]
AGENT_BINARY = str(REPO_ROOT / "agent")

ENV = {
    "SLLM_ENDPOINT": os.getenv("SLLM_ENDPOINT", "http://localhost:8080/v1/chat/completions"),
    "SLLM_API_KEY": os.getenv("SLLM_API_KEY", "sk-gemma4"),
}


@tool(description="Read a UTF-8 text file from the workspace.", read_only=True)
def read_file(path: str) -> str:
    # Restrict to the repo root to stay inside the workspace.
    full = (REPO_ROOT / path).resolve()
    if not str(full).startswith(str(REPO_ROOT)):
        raise ValueError(f"path escapes workspace: {path}")
    return full.read_text(encoding="utf-8")


async def main() -> None:
    async with Agent(binary_path=AGENT_BINARY, env=ENV, cwd=str(REPO_ROOT)) as agent:
        # Allow only `read_file` so we can see permission filtering working.
        await agent.configure(AgentConfig(
            max_turns=8,
            permission=PermissionConfig(enabled=True, allow=["read_file"]),
        ))
        await agent.register_tools(read_file)

        result = await agent.run("README.md を3行で要約して。")
        print("response:\n" + result.response)
        print("---")
        print("reason:", result.reason, "| turns:", result.turns)


if __name__ == "__main__":
    asyncio.run(main())
