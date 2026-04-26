"""End-to-end test against a real ``agent --rpc`` subprocess.

This test only runs when the ``AGENT_BINARY`` environment variable points to
a built ``agent`` binary; otherwise it is skipped. The default Quickstart
build command is::

    go build -o agent ./cmd/agent/

Then run::

    AGENT_BINARY=$(pwd)/agent python -m pytest sdk/python/tests/test_e2e.py

The test does not actually call out to a real SLLM (which would require a
running model server); it just exercises ``configure`` to confirm the binary
speaks JSON-RPC properly.
"""

from __future__ import annotations

import os
from pathlib import Path

import pytest

from ai_agent import Agent, AgentConfig, StreamingConfig

AGENT_BINARY = os.environ.get("AGENT_BINARY")

pytestmark = pytest.mark.skipif(
    not AGENT_BINARY or not Path(AGENT_BINARY).exists(),
    reason="AGENT_BINARY env var not set or path does not exist",
)


@pytest.mark.asyncio
async def test_agent_configure_against_real_binary() -> None:
    assert AGENT_BINARY is not None
    async with Agent(binary_path=AGENT_BINARY) as agent:
        applied = await agent.configure(
            AgentConfig(
                max_turns=3,
                streaming=StreamingConfig(enabled=True, context_status=True),
            )
        )
        assert "max_turns" in applied
        assert "streaming" in applied


@pytest.mark.asyncio
async def test_agent_abort_when_idle_returns_false() -> None:
    assert AGENT_BINARY is not None
    async with Agent(binary_path=AGENT_BINARY) as agent:
        # Nothing running; abort should report False.
        ok = await agent.abort("test")
        assert ok is False
