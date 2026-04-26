"""Tests for the JSON-RPC client / Agent surface using a fake in-memory peer.

Rather than mocking subprocess, we drive the SDK against an in-memory stdio
peer that speaks the same newline-delimited JSON-RPC the real Go server uses.
This exercises the full client-side state machine (pending requests, request
handlers, notifications) without needing the agent binary built.
"""

from __future__ import annotations

import asyncio
import json
from typing import Any

import pytest

from ai_agent.client import Agent
from ai_agent.config import AgentConfig, StreamingConfig
from ai_agent.errors import AgentBusy, GuardDenied, ToolError
from ai_agent.guard import input_guard
from ai_agent.jsonrpc import JsonRpcClient
from ai_agent.tool import tool
from ai_agent.verifier import verifier


class FakePeer:
    """Bi-directional in-memory pipe representing the Go core peer.

    ``client_reader/writer`` are passed to ``JsonRpcClient.attach``; the test
    drives the peer side via ``send_to_client`` and ``read_from_client``.
    """

    def __init__(self) -> None:
        # Two unidirectional pipes.
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


# ---------------------------------------------------------------------------
# Low-level JsonRpcClient round-trip
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_call_round_trip() -> None:
    peer = FakePeer()
    client = JsonRpcClient()
    client.attach(peer.client_reader, peer.client_writer)

    async def serve_once() -> None:
        msg = await peer.read_from_client()
        assert msg["jsonrpc"] == "2.0"
        assert msg["method"] == "agent.run"
        await peer.send_to_client(
            {
                "jsonrpc": "2.0",
                "id": msg["id"],
                "result": {"response": "ok", "reason": "completed", "turns": 1,
                           "usage": {"prompt_tokens": 1, "completion_tokens": 2,
                                     "total_tokens": 3}},
            }
        )

    serve_task = asyncio.create_task(serve_once())
    result = await client.call("agent.run", {"prompt": "hi"})
    await serve_task

    assert result["response"] == "ok"
    await client.close()


@pytest.mark.asyncio
async def test_call_propagates_rpc_errors() -> None:
    peer = FakePeer()
    client = JsonRpcClient()
    client.attach(peer.client_reader, peer.client_writer)

    async def serve_once() -> None:
        msg = await peer.read_from_client()
        await peer.send_to_client(
            {
                "jsonrpc": "2.0",
                "id": msg["id"],
                "error": {"code": -32002, "message": "agent already running"},
            }
        )

    serve_task = asyncio.create_task(serve_once())
    with pytest.raises(AgentBusy) as excinfo:
        await client.call("agent.run", {"prompt": "hi"})
    await serve_task

    assert excinfo.value.code == -32002
    await client.close()


@pytest.mark.asyncio
async def test_request_handler_responds() -> None:
    """Core -> wrapper request reaches the registered handler."""

    peer = FakePeer()
    client = JsonRpcClient()
    client.attach(peer.client_reader, peer.client_writer)

    async def handle(params: dict[str, Any]) -> dict[str, Any]:
        return {"content": f"echo:{params.get('args', {}).get('x')}"}

    client.set_request_handler("tool.execute", handle)

    await peer.send_to_client(
        {
            "jsonrpc": "2.0",
            "id": 99,
            "method": "tool.execute",
            "params": {"name": "echo", "args": {"x": 1}},
        }
    )

    response = await peer.read_from_client()
    assert response == {
        "jsonrpc": "2.0",
        "id": 99,
        "result": {"content": "echo:1"},
    }

    await client.close()


@pytest.mark.asyncio
async def test_notification_handler_dispatches() -> None:
    peer = FakePeer()
    client = JsonRpcClient()
    client.attach(peer.client_reader, peer.client_writer)

    received: list[dict[str, Any]] = []

    def handler(params: dict[str, Any]) -> None:
        received.append(params)

    client.set_notification_handler("stream.delta", handler)

    await peer.send_to_client(
        {"jsonrpc": "2.0", "method": "stream.delta",
         "params": {"text": "hello", "turn": 1}}
    )

    # Give the reader loop a chance to dispatch.
    for _ in range(20):
        if received:
            break
        await asyncio.sleep(0)

    assert received == [{"text": "hello", "turn": 1}]
    await client.close()


# ---------------------------------------------------------------------------
# Agent (high-level) using the same fake peer
# ---------------------------------------------------------------------------


async def _make_attached_agent() -> tuple[Agent, FakePeer]:
    peer = FakePeer()
    agent = Agent(binary_path="unused")
    agent._rpc.attach(peer.client_reader, peer.client_writer)  # type: ignore[attr-defined]
    agent._wire_handlers()  # type: ignore[attr-defined]
    return agent, peer


@pytest.mark.asyncio
async def test_agent_run_returns_typed_result() -> None:
    agent, peer = await _make_attached_agent()

    async def serve() -> None:
        msg = await peer.read_from_client()
        assert msg["method"] == "agent.run"
        assert msg["params"] == {"prompt": "hi"}
        await peer.send_to_client(
            {
                "jsonrpc": "2.0",
                "id": msg["id"],
                "result": {
                    "response": "Hello!",
                    "reason": "completed",
                    "turns": 2,
                    "usage": {
                        "prompt_tokens": 10,
                        "completion_tokens": 5,
                        "total_tokens": 15,
                    },
                },
            }
        )

    serve_task = asyncio.create_task(serve())
    result = await agent.run("hi")
    await serve_task

    assert result.response == "Hello!"
    assert result.reason == "completed"
    assert result.turns == 2
    assert result.usage.total_tokens == 15
    await agent.close()


@pytest.mark.asyncio
async def test_agent_configure_sends_omitempty_params() -> None:
    agent, peer = await _make_attached_agent()

    async def serve() -> None:
        msg = await peer.read_from_client()
        assert msg["method"] == "agent.configure"
        # Only set fields should be present.
        assert msg["params"] == {
            "max_turns": 5,
            "streaming": {"enabled": True},
        }
        await peer.send_to_client(
            {
                "jsonrpc": "2.0",
                "id": msg["id"],
                "result": {"applied": ["max_turns", "streaming"]},
            }
        )

    serve_task = asyncio.create_task(serve())
    applied = await agent.configure(
        AgentConfig(max_turns=5, streaming=StreamingConfig(enabled=True))
    )
    await serve_task

    assert applied == ["max_turns", "streaming"]
    await agent.close()


@pytest.mark.asyncio
async def test_agent_register_tool_then_handle_execute() -> None:
    agent, peer = await _make_attached_agent()

    @tool(description="add", read_only=True)
    def add(a: int, b: int) -> str:
        return str(a + b)

    async def serve_register() -> None:
        msg = await peer.read_from_client()
        assert msg["method"] == "tool.register"
        assert msg["params"]["tools"][0]["name"] == "add"
        await peer.send_to_client(
            {"jsonrpc": "2.0", "id": msg["id"], "result": {"registered": 1}}
        )

    register_task = asyncio.create_task(serve_register())
    n = await agent.register_tools(add)
    await register_task
    assert n == 1

    # Now simulate a core -> wrapper tool.execute request.
    await peer.send_to_client(
        {
            "jsonrpc": "2.0",
            "id": 7,
            "method": "tool.execute",
            "params": {"name": "add", "args": {"a": 2, "b": 3}},
        }
    )

    reply = await peer.read_from_client()
    assert reply["id"] == 7
    assert reply["result"]["content"] == "5"
    assert reply["result"].get("is_error", False) is False

    await agent.close()


@pytest.mark.asyncio
async def test_agent_guard_register_and_execute_deny() -> None:
    agent, peer = await _make_attached_agent()

    @input_guard(name="banned")
    def check(input: str) -> tuple[str, str]:
        if "evil" in input:
            return ("deny", "evil keyword")
        return ("allow", "")

    async def serve_register() -> None:
        msg = await peer.read_from_client()
        assert msg["method"] == "guard.register"
        await peer.send_to_client(
            {"jsonrpc": "2.0", "id": msg["id"], "result": {"registered": 1}}
        )

    register_task = asyncio.create_task(serve_register())
    await agent.register_guards(check)
    await register_task

    await peer.send_to_client(
        {
            "jsonrpc": "2.0",
            "id": 11,
            "method": "guard.execute",
            "params": {"name": "banned", "stage": "input",
                       "input": "this is evil"},
        }
    )
    reply = await peer.read_from_client()
    assert reply["id"] == 11
    assert reply["result"]["decision"] == "deny"

    await agent.close()


@pytest.mark.asyncio
async def test_agent_verifier_execute_pass_then_fail() -> None:
    agent, peer = await _make_attached_agent()

    @verifier(name="non_empty")
    def check(tool_name: str, args: dict, result: str) -> tuple[bool, str]:
        if not result.strip():
            return (False, "empty")
        return (True, "ok")

    async def serve_register() -> None:
        msg = await peer.read_from_client()
        assert msg["method"] == "verifier.register"
        await peer.send_to_client(
            {"jsonrpc": "2.0", "id": msg["id"], "result": {"registered": 1}}
        )

    register_task = asyncio.create_task(serve_register())
    await agent.register_verifiers(check)
    await register_task

    await peer.send_to_client(
        {
            "jsonrpc": "2.0",
            "id": 21,
            "method": "verifier.execute",
            "params": {"name": "non_empty", "tool_name": "x", "result": "hello"},
        }
    )
    assert (await peer.read_from_client())["result"]["passed"] is True

    await peer.send_to_client(
        {
            "jsonrpc": "2.0",
            "id": 22,
            "method": "verifier.execute",
            "params": {"name": "non_empty", "tool_name": "x", "result": " "},
        }
    )
    assert (await peer.read_from_client())["result"]["passed"] is False

    await agent.close()


@pytest.mark.asyncio
async def test_agent_streaming_callback_invoked() -> None:
    agent, peer = await _make_attached_agent()

    received: list[tuple[str, int]] = []

    def cb(text: str, turn: int) -> None:
        received.append((text, turn))

    async def serve() -> None:
        msg = await peer.read_from_client()
        assert msg["method"] == "agent.run"
        # Send some stream.delta notifications first.
        await peer.send_to_client(
            {"jsonrpc": "2.0", "method": "stream.delta",
             "params": {"text": "hello ", "turn": 1}}
        )
        await peer.send_to_client(
            {"jsonrpc": "2.0", "method": "stream.delta",
             "params": {"text": "world", "turn": 1}}
        )
        # Give the reader loop a tick before sending the result.
        for _ in range(20):
            if len(received) == 2:
                break
            await asyncio.sleep(0)
        await peer.send_to_client(
            {
                "jsonrpc": "2.0",
                "id": msg["id"],
                "result": {"response": "hello world", "reason": "completed",
                           "turns": 1,
                           "usage": {"prompt_tokens": 0, "completion_tokens": 0,
                                     "total_tokens": 0}},
            }
        )

    serve_task = asyncio.create_task(serve())
    result = await agent.run("hi", stream=cb)
    await serve_task

    assert result.response == "hello world"
    assert received == [("hello ", 1), ("world", 1)]
    await agent.close()


@pytest.mark.asyncio
async def test_tool_error_surfaces_as_toolerror() -> None:
    agent, peer = await _make_attached_agent()

    async def serve() -> None:
        msg = await peer.read_from_client()
        await peer.send_to_client(
            {
                "jsonrpc": "2.0",
                "id": msg["id"],
                "error": {"code": -32001, "message": "tool execution failed: x"},
            }
        )

    serve_task = asyncio.create_task(serve())
    with pytest.raises(ToolError):
        await agent.run("hi")
    await serve_task
    await agent.close()
