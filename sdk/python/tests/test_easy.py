"""Tests for the high-level easy.Agent API.

Exercises the pure-Python parts (AgentConfig, Tool, _MessageIndex)
and the network-facing parts via an in-memory fake peer.
"""

from __future__ import annotations

import asyncio
import json
from typing import Any

import pytest

from ai_agent.config import (
    CompactionConfig,
    CoordinatorConfig,
    DelegateConfig,
    GuardsConfig,
    JudgeConfig,
    LoopConfig,
    RouterConfig,
    StreamingConfig,
)
from ai_agent.easy import Agent, AgentConfig, Tool, _MessageIndex
from ai_agent.errors import AgentError, GuardDenied, TripwireTriggered, from_rpc_error
from ai_agent.guard import input_guard
from ai_agent.tool import tool
from ai_agent.verifier import verifier

# ---------------------------------------------------------------------------
# Helpers: minimal in-memory peer (mirrors test_jsonrpc.py FakePeer)
# ---------------------------------------------------------------------------


class _MemoryTransport(asyncio.Transport):
    def __init__(self, reader: asyncio.StreamReader) -> None:
        super().__init__()
        self._reader = reader
        self._closed = False

    def write(self, data: bytes) -> None:
        if not self._closed:
            self._reader.feed_data(data)

    def close(self) -> None:
        self._closed = True
        self._reader.feed_eof()

    def is_closing(self) -> bool:
        return self._closed

    def can_write_eof(self) -> bool:
        return True

    def write_eof(self) -> None:
        self._closed = True
        self._reader.feed_eof()

    def get_extra_info(self, name: str, default: Any = None) -> Any:
        return default


class FakePeer:
    def __init__(self) -> None:
        self.client_reader = asyncio.StreamReader()
        client_protocol = asyncio.StreamReaderProtocol(self.client_reader)
        self._peer_to_client_transport = _MemoryTransport(self.client_reader)
        self.client_writer_recv = asyncio.StreamReader()
        client_to_peer_protocol = asyncio.StreamReaderProtocol(self.client_writer_recv)
        self._client_to_peer_transport = _MemoryTransport(self.client_writer_recv)
        self.client_writer = asyncio.StreamWriter(
            self._client_to_peer_transport,
            client_to_peer_protocol,
            None,
            asyncio.get_event_loop(),
        )

    async def send_to_client(self, message: dict[str, Any]) -> None:
        data = (json.dumps(message) + "\n").encode()
        self.client_reader.feed_data(data)

    async def read_from_client(self) -> dict[str, Any]:
        line = await self.client_writer_recv.readline()
        if not line:
            raise EOFError("client closed")
        return json.loads(line.decode())

    def close_to_client(self) -> None:
        self.client_reader.feed_eof()


def _ok_run_response(msg: dict[str, Any]) -> dict[str, Any]:
    return {
        "jsonrpc": "2.0",
        "id": msg["id"],
        "result": {
            "response": "ok",
            "reason": "completed",
            "turns": 1,
            "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
        },
    }


async def _make_easy_agent(peer: FakePeer) -> Agent:
    """Return an easy.Agent with its core attached to *peer* (no subprocess)."""
    from ai_agent.client import Agent as CoreAgent
    from ai_agent.jsonrpc import JsonRpcClient

    config = AgentConfig(binary="./agent", system_prompt="test")
    agent = Agent(config, name="test-easy")

    core = CoreAgent.__new__(CoreAgent)
    rpc = JsonRpcClient()
    rpc.attach(peer.client_reader, peer.client_writer)
    core._rpc = rpc
    core._tools = {}
    core._guards = {}
    core._verifiers = {}
    core._judges = {}
    core._stream_cb = None
    core._on_status = None
    core._stream_lock = asyncio.Lock()
    core._wire_handlers()

    agent._core = core
    return agent


# ---------------------------------------------------------------------------
# AgentConfig._to_core_config
# ---------------------------------------------------------------------------


def test_agent_config_defaults() -> None:
    cfg = AgentConfig(binary="./agent")
    core = cfg._to_core_config()
    assert core.max_turns is None
    assert core.system_prompt is None


def test_agent_config_validation_empty_binary() -> None:
    import pytest as _pytest
    with _pytest.raises(ValueError, match="binary"):
        AgentConfig(binary="")


def test_agent_config_validation_zero_max_turns() -> None:
    import pytest as _pytest
    with _pytest.raises(ValueError, match="max_turns"):
        AgentConfig(binary="./agent", max_turns=0)


def test_agent_config_repr() -> None:
    cfg = AgentConfig(binary="./agent", system_prompt="hello", max_turns=5)
    r = repr(cfg)
    assert "AgentConfig" in r
    assert "./agent" in r
    assert "5" in r


def test_agent_repr() -> None:
    cfg = AgentConfig(binary="./agent")
    agent = Agent(cfg, name="my-agent")
    assert "my-agent" in repr(agent)
    assert "not_started" in repr(agent)


def test_agent_config_all_fields() -> None:
    cfg = AgentConfig(
        binary="./agent",
        system_prompt="hello",
        max_turns=10,
        token_limit=4096,
        work_dir="/tmp",
        delegate=DelegateConfig(enabled=True),
        coordinator=CoordinatorConfig(max_chars=500),
        compaction=CompactionConfig(enabled=True, budget_max_chars=1000),
        streaming=StreamingConfig(enabled=True),
        loop=LoopConfig(type="reaf"),
        router=RouterConfig(endpoint="http://localhost:8081/v1"),
        judge=JudgeConfig(name="my-judge"),
    )
    core = cfg._to_core_config()
    assert core.system_prompt == "hello"
    assert core.max_turns == 10
    assert core.loop is not None
    assert core.loop.type == "reaf"
    assert core.judge is not None
    assert core.judge.name == "my-judge"


# ---------------------------------------------------------------------------
# Tool class
# ---------------------------------------------------------------------------


def test_tool_from_function() -> None:
    def read_file(path: str, encoding: str = "utf-8") -> str:
        """Read a file."""
        return ""

    t = Tool(read_file, description="reads a file", read_only=True)
    assert t.name == "read_file"
    assert t.definition.read_only is True
    assert "path" in t.definition.parameters["properties"]


def test_tool_from_decorated() -> None:
    @tool(description="add two numbers", read_only=True)
    def add(a: int, b: int) -> int:
        return a + b

    t = Tool(add.func if hasattr(add, "func") else add, name="add", description="add")
    assert t.name == "add"


def test_tool_name_override() -> None:
    def fn(x: str) -> str:
        return x

    t = Tool(fn, name="my_custom_tool")
    assert t.name == "my_custom_tool"


# ---------------------------------------------------------------------------
# _MessageIndex
# ---------------------------------------------------------------------------


def test_message_index_empty_search() -> None:
    idx = _MessageIndex()
    assert idx.search("hello") == []


def test_message_index_basic_search() -> None:
    idx = _MessageIndex()
    idx.add("user", "what is the capital of Japan")
    idx.add("assistant", "Tokyo is the capital of Japan")
    idx.add("user", "what about France")
    idx.add("assistant", "Paris is the capital of France")

    results = idx.search("Japan capital", top_k=2)
    assert len(results) > 0
    # Tokyo / Japan message should rank higher
    assert any("Japan" in r["content"] or "Tokyo" in r["content"] for r in results)


def test_message_index_all_messages() -> None:
    idx = _MessageIndex()
    idx.add("user", "hello")
    idx.add("assistant", "hi")
    msgs = idx.all_messages()
    assert len(msgs) == 2
    assert msgs[0]["role"] == "user"
    assert msgs[1]["role"] == "assistant"


def test_message_index_copy_isolation() -> None:
    """copy() 後に子への追加が親に影響しないことを確認。"""
    parent = _MessageIndex()
    parent.add("user", "parent message")

    child = parent.copy()
    child.add("user", "child only message")

    assert len(parent.all_messages()) == 1, "親のインデックスは変化してはいけない"
    assert len(child.all_messages()) == 2, "子には両方のメッセージが入っている"


def test_message_index_copy_inherits_content() -> None:
    """copy() が既存ドキュメントを引き継いでいることを確認。"""
    idx = _MessageIndex()
    idx.add("user", "hello world")
    copied = idx.copy()
    results = copied.search("hello")
    assert len(results) > 0


# ---------------------------------------------------------------------------
# from_rpc_error — GuardDenied codes
# ---------------------------------------------------------------------------


def test_from_rpc_error_guard_denied() -> None:
    err = from_rpc_error(-32005, "Input rejected: bad prompt", {"decision": "deny", "reason": "bad prompt"})
    assert isinstance(err, GuardDenied)
    assert err.decision == "deny"
    assert err.reason == "bad prompt"
    assert err.code == -32005


def test_from_rpc_error_tripwire() -> None:
    err = from_rpc_error(-32006, "tripwire [input]: injection", {"decision": "tripwire", "source": "input", "reason": "injection"})
    assert isinstance(err, TripwireTriggered)
    assert isinstance(err, GuardDenied)  # TripwireTriggered は GuardDenied のサブクラス
    assert err.decision == "tripwire"
    assert err.reason == "injection"


def test_tripwire_catchable_as_guard_denied() -> None:
    err = from_rpc_error(-32006, "tripwire", {"reason": "injection attempt"})
    caught_as_guard = False
    caught_as_tripwire = False
    try:
        raise err
    except TripwireTriggered:
        caught_as_tripwire = True
    except GuardDenied:
        caught_as_guard = True
    assert caught_as_tripwire
    assert not caught_as_guard


def test_from_rpc_error_fallback_without_data() -> None:
    err = from_rpc_error(-32005, "Input rejected: reason", None)
    assert isinstance(err, GuardDenied)
    assert err.decision == "deny"
    # reason falls back to message when no data
    assert "Input rejected" in err.reason


# ---------------------------------------------------------------------------
# easy.Agent network-facing tests
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_easy_agent_input() -> None:
    peer = FakePeer()
    agent = await _make_easy_agent(peer)

    async def serve() -> None:
        # agent.run
        msg = await peer.read_from_client()
        assert msg["method"] == "agent.run"
        await peer.send_to_client(_ok_run_response(msg))

    task = asyncio.create_task(serve())
    result = await agent.input("hello")
    await task

    assert result == "ok"


@pytest.mark.asyncio
async def test_easy_agent_input_verbose() -> None:
    peer = FakePeer()
    agent = await _make_easy_agent(peer)

    async def serve() -> None:
        msg = await peer.read_from_client()
        await peer.send_to_client(_ok_run_response(msg))

    task = asyncio.create_task(serve())
    result = await agent.input_verbose("hello")
    await task

    assert result.response == "ok"
    assert result.turns == 1
    assert result.reason == "completed"


@pytest.mark.asyncio
async def test_easy_agent_export() -> None:
    peer = FakePeer()
    agent = await _make_easy_agent(peer)

    history = [{"role": "user", "content": "hi"}, {"role": "assistant", "content": "hello"}]

    async def serve() -> None:
        msg = await peer.read_from_client()
        assert msg["method"] == "session.history"
        await peer.send_to_client({"jsonrpc": "2.0", "id": msg["id"], "result": {"messages": history}})

    task = asyncio.create_task(serve())
    data = await agent.export()
    await task

    assert data["messages"] == history
    assert data["version"] == 1
    assert "rag_index" in data


@pytest.mark.asyncio
async def test_easy_agent_import_history() -> None:
    peer = FakePeer()
    agent = await _make_easy_agent(peer)

    history = [{"role": "user", "content": "hi"}, {"role": "assistant", "content": "hello"}]

    async def serve() -> None:
        msg = await peer.read_from_client()
        assert msg["method"] == "session.inject"
        assert msg["params"]["position"] == "replace"
        assert msg["params"]["messages"] == history
        await peer.send_to_client({"jsonrpc": "2.0", "id": msg["id"], "result": {}})

    task = asyncio.create_task(serve())
    await agent.import_history({"messages": history, "rag_index": []})
    await task


@pytest.mark.asyncio
async def test_easy_agent_register_guard() -> None:
    peer = FakePeer()
    agent = await _make_easy_agent(peer)

    @input_guard(name="block_bad")
    def check(input: str) -> tuple[str, str]:
        if "bad" in input:
            return "deny", "bad input"
        return "allow", ""

    async def serve() -> None:
        msg = await peer.read_from_client()
        assert msg["method"] == "guard.register"
        assert msg["params"]["guards"][0]["name"] == "block_bad"
        await peer.send_to_client({"jsonrpc": "2.0", "id": msg["id"], "result": {"registered": 1}})

    task = asyncio.create_task(serve())
    names = await agent.register_guards(check)
    await task
    assert names == ["block_bad"]


@pytest.mark.asyncio
async def test_easy_agent_guard_denied_raises() -> None:
    """GuardDenied is raised when the core returns ErrCodeGuardDenied(-32005)."""
    peer = FakePeer()
    agent = await _make_easy_agent(peer)

    async def serve() -> None:
        msg = await peer.read_from_client()
        assert msg["method"] == "agent.run"
        await peer.send_to_client({
            "jsonrpc": "2.0",
            "id": msg["id"],
            "error": {
                "code": -32005,
                "message": "Input rejected: bad prompt",
                "data": {"decision": "deny", "reason": "bad prompt"},
            },
        })

    task = asyncio.create_task(serve())
    with pytest.raises(GuardDenied) as exc_info:
        await agent.input("bad prompt")
    await task

    assert exc_info.value.decision == "deny"
    assert exc_info.value.reason == "bad prompt"
