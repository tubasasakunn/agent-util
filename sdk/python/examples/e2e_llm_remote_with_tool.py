"""E2E: llm.execute through router + tool call.

Confirms the remote LLM handler also gets invoked from the router step
(which uses JSON mode) and the response-generation step.
"""
from __future__ import annotations

import asyncio
import json
import os

from ai_agent import Agent, AgentConfig, LLMConfig, Tool


CALL_LOG: list[dict] = []


def meaning_of_life() -> str:
    """Returns the meaning of life."""
    return "42"


meaning_tool = Tool(meaning_of_life, description="Returns the meaning of life.")


def fake_llm(request: dict) -> dict:
    CALL_LOG.append(request)
    response_format = request.get("response_format") or {}

    # ルーター呼び出しは JSON mode。1 回目だけ meaning_of_life を呼ばせる
    if response_format.get("type") == "json_object":
        # ツール一覧が tools に含まれているか確認
        msgs = request.get("messages", [])
        sys_prompt = msgs[0]["content"] if msgs else ""
        if "meaning_of_life" in sys_prompt and not any(
            "42" in (m.get("content") or "") for m in msgs
        ):
            payload = {
                "tool": "meaning_of_life",
                "arguments": {},
                "reasoning": "user wants the meaning of life",
            }
        else:
            payload = {"tool": "none", "arguments": {}, "reasoning": "done"}
        content = json.dumps(payload)
    else:
        # チャットステップでは普通の応答
        msgs = request.get("messages", [])
        tool_outputs = [m.get("content") for m in msgs if m.get("role") == "tool"]
        if tool_outputs:
            content = f"The meaning of life is {tool_outputs[-1]}."
        else:
            content = "I don't know yet."

    return {
        "id": "fake-2",
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
    env = {"SLLM_ENDPOINT": "http://127.0.0.1:1/nonexistent"}

    config = AgentConfig(
        binary=binary,
        env=env,
        system_prompt="You answer factual questions using the tools provided.",
        max_turns=4,
        llm=LLMConfig(mode="remote"),
        llm_handler=fake_llm,
    )

    async with Agent(config) as agent:
        await agent.register_tools(meaning_tool)
        result = await agent.input("What is the meaning of life?")

    print("---- result ----")
    print(f"response: {result!r}")
    print(f"llm.execute call count: {len(CALL_LOG)}")
    for i, req in enumerate(CALL_LOG):
        mode = (req.get("response_format") or {}).get("type", "chat")
        print(f"  [{i}] mode={mode} msgs={len(req.get('messages', []))}")

    if "42" not in result:
        print("FAIL: tool result did not propagate into final response")
        return 1
    print("PASS: router + tool + chat all routed through llm.execute")
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
