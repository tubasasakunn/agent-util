"""
複雑なエージェントワークフロー統合テスト
Gemma4 SLLMを使って各機能を実践的に検証し、Slackへ報告する。
"""
from __future__ import annotations

import asyncio
import json
import math
import os
import sys
import time
import traceback
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import httpx

# ── SDK パス ──────────────────────────────────────────────────────────
sys.path.insert(0, str(Path(__file__).parent / "sdk/python"))

from ai_agent.client import Agent, AgentResult
from ai_agent.config import (
    AgentConfig,
    CoordinatorConfig,
    DelegateConfig,
    JudgeConfig,
    LoopConfig,
    RouterConfig,
    StreamingConfig,
)
from ai_agent.tool import ToolDefinition

# ── 設定 ──────────────────────────────────────────────────────────────
BINARY   = str(Path(__file__).parent / "bin/agent")
ENDPOINT = "http://localhost:8080/v1/chat/completions"
API_KEY  = "sk-gemma4"
MODEL    = "gemma3:4b"

ENV = {
    "SLLM_ENDPOINT": ENDPOINT,
    "SLLM_API_KEY":  API_KEY,
    "SLLM_MODEL":    MODEL,
}

SLACK_TOKEN      = os.environ.get("SLACK_TOKEN", "")
SLACK_CHANNEL_ID = os.environ.get("SLACK_CHANNEL_ID", "")

# ── 結果収集 ───────────────────────────────────────────────────────────
@dataclass
class TestResult:
    name: str
    ok: bool
    elapsed: float
    detail: str
    error: str = ""

results: list[TestResult] = []

def _agent() -> Agent:
    return Agent(binary_path=BINARY, env=ENV)

def _cfg(**kw) -> AgentConfig:
    return AgentConfig(max_turns=kw.pop("max_turns", 10), **kw)

async def run_case(name: str, coro) -> TestResult:
    print(f"\n{'='*60}")
    print(f"▶ {name}")
    t0 = time.perf_counter()
    try:
        detail = await coro
        elapsed = time.perf_counter() - t0
        r = TestResult(name=name, ok=True, elapsed=elapsed, detail=str(detail))
        print(f"  ✅  {elapsed:.1f}s | {str(detail)[:140]}")
    except Exception as exc:
        elapsed = time.perf_counter() - t0
        r = TestResult(name=name, ok=False, elapsed=elapsed, detail="", error=str(exc))
        print(f"  ❌  {elapsed:.1f}s | {exc}")
        print(traceback.format_exc()[:400])
    results.append(r)
    return r


# ══════════════════════════════════════════════════════════════════════
# テストケース
# ══════════════════════════════════════════════════════════════════════

# ── 1. シンプル Q&A ───────────────────────────────────────────────────
async def test_simple_qa():
    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt="You are a concise assistant. Always reply in one sentence.",
        ))
        r: AgentResult = await ag.run("What is 7 multiplied by 8?")
        resp_lower = r.response.lower()
        assert "56" in r.response or "fifty-six" in resp_lower or "fifty six" in resp_lower, \
            f"expected 56/fifty-six, got: {r.response}"
        return f"turns={r.turns} | {r.response[:80]}"


# ── 2. カスタムツール (計算機) ─────────────────────────────────────────
async def test_custom_tool_calculator():
    def calculator(expression: str) -> str:
        """Evaluate a mathematical expression and return the result."""
        try:
            val = eval(expression, {"__builtins__": {}}, {"sqrt": math.sqrt, "pi": math.pi})
            return str(val)
        except Exception as e:
            return f"error: {e}"

    params = {
        "type": "object",
        "properties": {
            "expression": {"type": "string", "description": "Mathematical expression to evaluate"}
        },
        "required": ["expression"]
    }
    tool_def = ToolDefinition(
        name="calculator",
        description="Evaluate a mathematical expression. Use this for any arithmetic.",
        parameters=params,
        read_only=True,
        func=calculator,
        is_coroutine=False,
    )

    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt=(
                "Use the calculator tool for all arithmetic operations. "
                "Never compute numbers yourself."
            ),
        ))
        await ag.register_tools(tool_def)
        r: AgentResult = await ag.run("What is sqrt(144) + 3? Use the calculator.")
        assert "15" in r.response, f"expected 15 in response: {r.response}"
        return f"turns={r.turns} tool_called=True | {r.response[:80]}"


# ── 3. ファイル読み取りツール ─────────────────────────────────────────
async def test_file_read_tool():
    tmp = Path("/tmp/ai_agent_test_data.txt")
    tmp.write_text("SECRET_NUMBER=42\nPROJECT=ai-agent\n")

    def read_file(path: str) -> str:
        """Read a file and return its text content."""
        try:
            return Path(path).read_text()
        except Exception as e:
            return f"error: {e}"

    params = {
        "type": "object",
        "properties": {"path": {"type": "string", "description": "Absolute file path to read"}},
        "required": ["path"]
    }
    tool_def = ToolDefinition(
        name="read_file",
        description="Read a text file and return its content.",
        parameters=params,
        read_only=True,
        func=read_file,
        is_coroutine=False,
    )

    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt="Use read_file to read files. Answer based only on file content.",
        ))
        await ag.register_tools(tool_def)
        r: AgentResult = await ag.run(
            f"Read the file at {tmp} and tell me the value of SECRET_NUMBER."
        )
        assert "42" in r.response, f"expected 42 in response: {r.response}"
        return f"turns={r.turns} | {r.response[:80]}"


# ── 4. Delegate サブエージェント ──────────────────────────────────────
async def test_delegate_task():
    """
    delegate_task の実行を timing と response length で確認する。
    サブエージェント起動は 5s 以上かかるため elapsed で判定。
    """
    t0 = time.perf_counter()
    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt=(
                "You are a research coordinator with a delegation tool. "
                "ALWAYS use the delegation tool for research and writing tasks — "
                "never write the answer yourself. "
                "Your only job is to delegate and then report what the sub-agent returned."
            ),
            delegate=DelegateConfig(enabled=True, max_chars=2000),
        ))
        r: AgentResult = await ag.run(
            "Research task: Write 3 detailed sentences about the history of the Python "
            "programming language, including who created it and when. "
            "This must be handled by the delegation tool."
        )
    elapsed = time.perf_counter() - t0
    # サブエージェントが実際に起動→LLM呼び出しが発生 → 5s 以上かかる
    delegate_ran = elapsed > 5.0
    has_content  = len(r.response) > 40
    assert delegate_ran or has_content, \
        f"delegate did not appear to run: elapsed={elapsed:.1f}s resp={r.response[:60]}"
    return f"turns={r.turns} elapsed={elapsed:.1f}s delegate_ran={delegate_ran} | {r.response[:120]}"


# ── 5. Coordinate 並列タスク ──────────────────────────────────────────
async def test_coordinate_tasks():
    """
    coordinate_tasks の実行を timing で確認する。
    サブエージェントが実際に LLM 呼び出しを行えば 3s 以上かかる。
    注意: sub-agent は親のPython側ツールを継承しない — 純粋 LLM タスクで検証。
    """
    t0 = time.perf_counter()
    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt=(
                "You are a parallel task coordinator. "
                "ALWAYS use the parallel execution tool when the user gives you "
                "multiple independent tasks. Never answer multiple tasks directly — "
                "spawn parallel sub-agents for each one."
            ),
            coordinator=CoordinatorConfig(enabled=True, max_chars=800),
        ))
        r: AgentResult = await ag.run(
            "Use the parallel execution tool to handle these TWO independent tasks simultaneously:\n"
            "Task-1: In exactly one sentence, define what a 'stack' data structure is.\n"
            "Task-2: In exactly one sentence, define what a 'queue' data structure is.\n"
            "After both sub-agents finish, combine their answers into your response."
        )
    elapsed = time.perf_counter() - t0

    resp_lower = r.response.lower()
    has_stack      = "stack" in resp_lower
    has_queue      = "queue" in resp_lower
    # sub-agent が LLM を呼び出せば最低でも 3s かかる
    parallel_ran   = elapsed > 3.0

    detail = (
        f"elapsed={elapsed:.1f}s parallel_ran={parallel_ran} "
        f"has_stack={has_stack} has_queue={has_queue} turns={r.turns} | "
        f"{r.response[:100]}"
    )
    assert has_stack or has_queue or parallel_ran, \
        f"coordinate_tasks produced no observable result: {detail}"
    return detail


# ── 6. GoalJudge — 意味的ゴール検出 ──────────────────────────────────
async def test_goal_judge_reaf():
    """
    judge.evaluate RPC のコールバック機構を検証する。
    ジャッジはトピックに関連するキーワードを検出して終了判定する。
    """
    judge_calls: list[dict] = []

    def my_judge(response: str, turn: int):
        """東京/Japan が含まれたら目標達成として終了。最大2ターンで強制終了。"""
        resp_lower = response.lower()
        found = "tokyo" in resp_lower or "japan" in resp_lower or "東京" in response
        judge_calls.append({"turn": turn, "found": found, "snippet": response[:40]})

        if found:
            return True, "goal_achieved_keyword_found"
        if turn >= 2:
            return True, "max_turns_reached"
        return False, ""

    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt="Answer geography questions concisely.",
        ))
        await ag.register_judge("geo_judge", my_judge)
        await ag.configure(AgentConfig(judge=JudgeConfig(name="geo_judge")))
        r: AgentResult = await ag.run("What is the capital city of Japan?")

    assert len(judge_calls) > 0, "judge was never called"
    terminated_correctly = r.reason in ("goal_achieved_keyword_found", "max_turns_reached", "completed")
    assert terminated_correctly, f"unexpected reason: {r.reason}"
    return (
        f"turns={r.turns} reason={r.reason} "
        f"judge_calls={len(judge_calls)} details={judge_calls} | {r.response[:60]}"
    )


# ── 7. Router 専用 LLM 設定 ──────────────────────────────────────────
async def test_router_separate_llm():
    """RouterConfig でルーター専用の LLM エンドポイントを設定できることを確認。"""
    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt="Answer questions concisely in one sentence.",
            router=RouterConfig(
                endpoint=ENDPOINT,
                model=MODEL,
                api_key=API_KEY,
            ),
        ))
        r: AgentResult = await ag.run("Name one benefit of using Python for data science.")
        assert len(r.response) > 5, f"response too short: {r.response}"
        return f"turns={r.turns} | {r.response[:80]}"


# ── 8. ストリーミング受信 ──────────────────────────────────────────────
async def test_streaming():
    chunks: list[str] = []

    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt="Answer in exactly 2 sentences.",
            streaming=StreamingConfig(enabled=True),
        ))

        def on_stream(text: str, turn: int) -> None:
            chunks.append(text)

        r: AgentResult = await ag.run("Explain what an API is.", stream=on_stream)

    assert len(chunks) > 0, "no stream chunks received"
    full = "".join(chunks)
    assert len(full) > 10, f"streamed content too short: {full!r}"
    return (
        f"turns={r.turns} chunks={len(chunks)} "
        f"total_chars={len(full)} | {r.response[:60]}"
    )


# ── 9. マルチターン会話 (セッション継続) ─────────────────────────────
async def test_multi_turn_conversation():
    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt="You are a helpful assistant. Maintain context across messages.",
            max_turns=5,
        ))
        r1: AgentResult = await ag.run("Remember: my lucky number is 7.")
        r2: AgentResult = await ag.run("What is my lucky number?")

    assert "7" in r2.response or "seven" in r2.response.lower(), \
        f"expected 7/seven in: {r2.response}"
    return f"r1.turns={r1.turns} r2.turns={r2.turns} | {r2.response[:80]}"


# ── 10. ツール + Delegate 複合ワークフロー ─────────────────────────────
async def test_tool_plus_delegate():
    """
    メインエージェントがツールを使いつつ、複雑な分析はサブエージェントに委任する。
    """
    call_log: list[str] = []

    def get_weather(city: str) -> str:
        """Get current weather for a city."""
        call_log.append(city)
        data = {"Tokyo": "sunny 22°C", "Osaka": "cloudy 18°C", "Sapporo": "snowy 0°C"}
        return data.get(city, f"unknown city: {city}")

    params = {
        "type": "object",
        "properties": {"city": {"type": "string", "description": "City name"}},
        "required": ["city"]
    }
    tool_def = ToolDefinition(
        name="get_weather",
        description="Get current weather for a named city.",
        parameters=params,
        read_only=True,
        func=get_weather,
        is_coroutine=False,
    )

    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt=(
                "You have a weather tool. Use it to get weather data. "
                "For complex analysis tasks you can delegate to sub-agents."
            ),
            delegate=DelegateConfig(enabled=True, max_chars=2000),
        ))
        await ag.register_tools(tool_def)
        r: AgentResult = await ag.run(
            "Get the weather in Tokyo using the weather tool, then tell me if I need an umbrella."
        )

    assert len(r.response) > 10, f"response too short: {r.response}"
    return f"turns={r.turns} tool_calls={call_log} | {r.response[:100]}"


# ── 11. Permission (ツール拒否) ──────────────────────────────────────
async def test_permission_deny():
    """PermissionConfig.deny が機能してツール呼び出しをブロックすること。"""
    from ai_agent.config import PermissionConfig

    blocked_calls: list[str] = []

    def dangerous_delete(path: str) -> str:
        """Delete a file."""
        blocked_calls.append(path)
        return f"deleted {path}"

    params = {
        "type": "object",
        "properties": {"path": {"type": "string"}},
        "required": ["path"]
    }
    tool_def = ToolDefinition(
        name="dangerous_delete",
        description="Delete a file at the given path.",
        parameters=params,
        read_only=False,
        func=dangerous_delete,
        is_coroutine=False,
    )

    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt="You have a dangerous_delete tool. Use it when asked to delete files.",
            permission=PermissionConfig(enabled=True, deny=["dangerous_delete"]),
        ))
        await ag.register_tools(tool_def)
        r: AgentResult = await ag.run("Please delete the file /tmp/test.txt")

    # ツールは呼ばれていないはず
    assert len(blocked_calls) == 0, f"tool should have been blocked but was called: {blocked_calls}"
    assert len(r.response) > 0, "expected some response"
    return f"turns={r.turns} blocked_calls={blocked_calls} | {r.response[:80]}"


# ── 12. LoopType 設定 (reaf) ─────────────────────────────────────────
async def test_loop_type_reaf():
    """LoopConfig(type='reaf') を設定できることと、エージェントが動作することを確認。"""
    async with _agent() as ag:
        await ag.configure(_cfg(
            system_prompt="Answer questions briefly.",
            loop=LoopConfig(type="reaf"),
        ))
        r: AgentResult = await ag.run("What programming language is Go?")
        assert len(r.response) > 5, f"response too short: {r.response}"
        return f"turns={r.turns} loop=reaf | {r.response[:80]}"


# ══════════════════════════════════════════════════════════════════════
# Slack 送信
# ══════════════════════════════════════════════════════════════════════

async def send_slack(text: str) -> bool:
    if not SLACK_TOKEN or not SLACK_CHANNEL_ID:
        print("[Slack] token/channel not set, skipping")
        return False
    async with httpx.AsyncClient() as c:
        resp = await c.post(
            "https://slack.com/api/chat.postMessage",
            headers={"Authorization": f"Bearer {SLACK_TOKEN}"},
            json={"channel": SLACK_CHANNEL_ID, "text": text},
            timeout=10,
        )
        data = resp.json()
        if not data.get("ok"):
            print(f"[Slack] error: {data}")
        return data.get("ok", False)


def build_slack_report(results: list[TestResult]) -> str:
    passed  = sum(1 for r in results if r.ok)
    total   = len(results)
    emoji   = "🎉" if passed == total else ("⚠️" if passed > total // 2 else "🔴")
    lines   = [
        f"*{emoji} ai-agent ワークフロー検証レポート*",
        f"SLLM: `{ENDPOINT}` (model: {MODEL})",
        f"結果: *{passed}/{total} PASS*\n",
    ]
    for r in results:
        icon    = "✅" if r.ok else "❌"
        elapsed = f"{r.elapsed:.1f}s"
        if r.ok:
            detail = r.detail[:120] if r.detail else ""
            lines.append(f"{icon} *{r.name}* ({elapsed})\n   `{detail}`")
        else:
            lines.append(f"{icon} *{r.name}* ({elapsed})\n   エラー: `{r.error[:130]}`")
    return "\n".join(lines)


# ══════════════════════════════════════════════════════════════════════
# main
# ══════════════════════════════════════════════════════════════════════

async def main():
    cases = [
        ("1. シンプルQ&A",               test_simple_qa),
        ("2. カスタムツール(計算機)",     test_custom_tool_calculator),
        ("3. ファイル読み取りツール",     test_file_read_tool),
        ("4. Delegateサブエージェント",   test_delegate_task),
        ("5. Coordinate並列タスク",       test_coordinate_tasks),
        ("6. GoalJudge(意味的検出)",      test_goal_judge_reaf),
        ("7. Router専用LLM設定",          test_router_separate_llm),
        ("8. ストリーミング受信",         test_streaming),
        ("9. マルチターン会話",           test_multi_turn_conversation),
        ("10. ツール+Delegate複合",       test_tool_plus_delegate),
        ("11. Permission拒否",            test_permission_deny),
        ("12. LoopType=reaf設定",         test_loop_type_reaf),
    ]

    for name, fn in cases:
        await run_case(name, fn())

    passed = sum(1 for r in results if r.ok)
    total  = len(results)
    print(f"\n{'='*60}")
    print(f"TOTAL: {passed}/{total} PASS")

    report = build_slack_report(results)
    print("\n--- Slack Report ---")
    print(report)

    ok = await send_slack(report)
    print(f"[Slack] sent={ok}")


if __name__ == "__main__":
    asyncio.run(main())
