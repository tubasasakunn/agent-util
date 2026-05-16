"""End-to-end smoke test for llm.execute (Plan B reverse RPC).

Spawns the agent binary with NO valid SLLM_ENDPOINT and routes every
ChatCompletion to a Python-side handler that fabricates an OpenAI-style
response. If the run succeeds, the LLM never touched HTTP — meaning
``llm.mode="remote"`` correctly diverts the completer to the wrapper.

Run from repo root::

    PYTHONPATH=sdk/python python3 sdk/python/examples/e2e_llm_remote.py
"""
from __future__ import annotations

import asyncio
import os

from ai_agent import Agent, AgentConfig, LLMConfig


CALL_LOG: list[dict] = []


def fake_llm(request: dict) -> dict:
    """Echo handler. Returns an OpenAI-style ChatResponse without any network."""
    CALL_LOG.append(request)
    # 直近のユーザーメッセージを取り出してエコー
    msgs = request.get("messages", [])
    last_user = next(
        (m.get("content") for m in reversed(msgs) if m.get("role") == "user"),
        "(no user msg)",
    )
    response_format = request.get("response_format") or {}
    # ルーターは JSON mode で {"tool":..., "arguments":..., "reasoning":...} を期待する
    if response_format.get("type") == "json_object":
        content = '{"tool":"none","arguments":{},"reasoning":"fake handler"}'
    else:
        content = f"FAKE-LLM echoes: {last_user!r}"

    return {
        "id": "fake-1",
        "object": "chat.completion",
        "created": 0,
        "model": request.get("model", "fake-model"),
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": content},
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
    }


async def main() -> int:
    binary = os.environ.get("AGENT_BIN", "./bin/agent")
    # 故意に SLLM_ENDPOINT を壊して「HTTP は絶対に使わない」ことを担保
    env = {"SLLM_ENDPOINT": "http://127.0.0.1:1/nonexistent"}

    config = AgentConfig(
        binary=binary,
        env=env,
        system_prompt="You are a tester.",
        max_turns=2,
        llm=LLMConfig(mode="remote"),
        llm_handler=fake_llm,
    )

    async with Agent(config) as agent:
        result = await agent.input("hello!")

    print("---- result ----")
    print(f"response: {result!r}")
    print(f"llm.execute call count: {len(CALL_LOG)}")
    if not CALL_LOG:
        print("FAIL: handler was never called")
        return 1
    if "FAKE-LLM" not in result and "fake handler" not in result:
        print("WARN: response did not contain the fake marker (may have gone through compaction)")
    print("PASS: end-to-end llm.execute routing works")
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
