"""Register a wrapper-side input guard that denies inputs containing 'internal-only'."""

from __future__ import annotations

import asyncio
import os
from pathlib import Path

from ai_agent import Agent, AgentConfig, GuardDenied, GuardsConfig, input_guard

REPO_ROOT = Path(__file__).resolve().parents[3]
AGENT_BINARY = str(REPO_ROOT / "agent")

ENV = {
    "SLLM_ENDPOINT": os.getenv("SLLM_ENDPOINT", "http://localhost:8080/v1/chat/completions"),
    "SLLM_API_KEY": os.getenv("SLLM_API_KEY", "sk-gemma4"),
}


@input_guard(name="internal_keyword")
def reject_internal(input: str) -> tuple[str, str]:
    # Returning ('deny', reason) blocks the run before the model is called.
    if "internal-only" in input.lower():
        return ("deny", "input contains the 'internal-only' marker")
    return ("allow", "")


async def main() -> None:
    async with Agent(binary_path=AGENT_BINARY, env=ENV) as agent:
        await agent.register_guards(reject_internal)
        await agent.configure(AgentConfig(
            max_turns=3,
            guards=GuardsConfig(input=["internal_keyword"]),
        ))

        # Allowed: regular prompt.
        ok = await agent.run("Tell me a fun fact about octopuses in one sentence.")
        print("[1] allowed:", ok.response.strip())

        # Denied: prompt contains the forbidden marker.
        try:
            await agent.run("This is internal-only material — summarise it.")
            print("[2] denied: NOT blocked (unexpected)")
        except GuardDenied as exc:
            print("[2] denied:", exc)


if __name__ == "__main__":
    asyncio.run(main())
