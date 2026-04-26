"""Stream stream.delta tokens to stdout in real time."""

from __future__ import annotations

import asyncio
import os
import sys
from pathlib import Path

from ai_agent import Agent, AgentConfig, StreamingConfig

REPO_ROOT = Path(__file__).resolve().parents[3]
AGENT_BINARY = str(REPO_ROOT / "agent")

ENV = {
    "SLLM_ENDPOINT": os.getenv("SLLM_ENDPOINT", "http://localhost:8080/v1/chat/completions"),
    "SLLM_API_KEY": os.getenv("SLLM_API_KEY", "sk-gemma4"),
}


def on_delta(text: str, turn: int) -> None:
    # Print deltas inline — turn number is useful for multi-turn flows.
    sys.stdout.write(text)
    sys.stdout.flush()


def on_status(ratio: float, count: int, limit: int) -> None:
    # Render context usage to stderr so it doesn't mix with the streamed answer.
    print(f"\n[ctx {count}/{limit} = {ratio:.0%}]", file=sys.stderr)


async def main() -> None:
    async with Agent(binary_path=AGENT_BINARY, env=ENV) as agent:
        # Streaming must be enabled before deltas are emitted.
        await agent.configure(AgentConfig(
            max_turns=4,
            streaming=StreamingConfig(enabled=True, context_status=True),
        ))
        result = await agent.run(
            "100文字くらいで自己紹介して。",
            stream=on_delta,
            on_status=on_status,
        )
        print("\n---")
        print("final reason:", result.reason, "| turns:", result.turns)


if __name__ == "__main__":
    asyncio.run(main())
