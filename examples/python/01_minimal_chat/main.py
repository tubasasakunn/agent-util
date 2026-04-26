"""Smallest possible ai-agent run: spawn the binary, ask one question, exit."""

from __future__ import annotations

import asyncio
import os
from pathlib import Path

from ai_agent import Agent

# Resolve the agent binary built at the repo root: examples/python/<name>/main.py
AGENT_BINARY = str(Path(__file__).resolve().parents[3] / "agent")

# SLLM endpoint and API key — defaults match the repo's reference LM Studio setup.
ENV = {
    "SLLM_ENDPOINT": os.getenv("SLLM_ENDPOINT", "http://localhost:8080/v1/chat/completions"),
    "SLLM_API_KEY": os.getenv("SLLM_API_KEY", "sk-gemma4"),
}


async def main() -> None:
    # Async context manager handles start() / close() for us.
    async with Agent(binary_path=AGENT_BINARY, env=ENV) as agent:
        result = await agent.run("こんにちは。今日の天気はどう？一文で答えて。")
        print("response:", result.response)
        print("reason:  ", result.reason)
        print("turns:   ", result.turns)


if __name__ == "__main__":
    asyncio.run(main())
