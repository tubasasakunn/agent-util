"""High-level :class:`Agent` client.

Spawns the Go ``agent --rpc`` binary and exposes ``configure``, ``run``,
``abort`` and registration helpers (``register_tools``, ``register_guards``,
``register_verifiers``, ``register_mcp``) as async coroutines.

Use it as an async context manager:

    async with Agent(binary_path="./agent") as agent:
        await agent.configure(AgentConfig(max_turns=5))
        await agent.register_tools(my_tool)
        result = await agent.run("hello")
        print(result.response)
"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass
from typing import Any, Awaitable, Callable

from ai_agent.config import AgentConfig
from ai_agent.errors import AgentError, GuardDenied
from ai_agent.guard import GuardDefinition, get_guard_definition
from ai_agent.jsonrpc import JsonRpcClient
from ai_agent.tool import ToolDefinition, get_tool_definition
from ai_agent.verifier import VerifierDefinition, get_verifier_definition

# JSON-RPC method names (mirrors pkg/protocol/methods.go).
_M_AGENT_RUN = "agent.run"
_M_AGENT_ABORT = "agent.abort"
_M_AGENT_CONFIGURE = "agent.configure"
_M_TOOL_REGISTER = "tool.register"
_M_TOOL_EXECUTE = "tool.execute"
_M_MCP_REGISTER = "mcp.register"
_M_GUARD_REGISTER = "guard.register"
_M_GUARD_EXECUTE = "guard.execute"
_M_VERIFIER_REGISTER = "verifier.register"
_M_VERIFIER_EXECUTE = "verifier.execute"
_N_STREAM_DELTA = "stream.delta"
_N_STREAM_END = "stream.end"
_N_CONTEXT_STATUS = "context.status"


@dataclass
class UsageInfo:
    """Token usage returned alongside ``agent.run``."""

    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0


@dataclass
class AgentResult:
    """Result of a successful ``agent.run``."""

    response: str
    reason: str
    turns: int
    usage: UsageInfo


StreamCallback = Callable[[str, int], None] | Callable[[str, int], Awaitable[None]]
"""Type for the ``stream=`` callback. Sync or async; receives ``(text, turn)``."""

StatusCallback = Callable[[float, int, int], None] | Callable[
    [float, int, int], Awaitable[None]
]
"""Type for the optional context-status callback. ``(usage_ratio, count, limit)``."""


class Agent:
    """Async Python client for the ai-agent JSON-RPC server.

    Args:
        binary_path: Path to the compiled ``agent`` binary. Defaults to
            ``"agent"`` (looked up via PATH). Build it with::

                go build -o agent ./cmd/agent/

        env: Extra environment variables for the subprocess (merged with
            the parent environment). Useful for ``SLLM_ENDPOINT``,
            ``SLLM_MODEL``, ``SLLM_API_KEY``, ``SLLM_CONTEXT_SIZE``.
        cwd: Working directory for the subprocess.
    """

    def __init__(
        self,
        binary_path: str = "agent",
        *,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
    ) -> None:
        self._binary_path = binary_path
        self._env = env
        self._cwd = cwd
        self._rpc = JsonRpcClient()

        self._tools: dict[str, ToolDefinition] = {}
        self._guards: dict[str, GuardDefinition] = {}
        self._verifiers: dict[str, VerifierDefinition] = {}

        self._on_status: StatusCallback | None = None

        # Per-call streaming callback. Set by ``run`` and reset on completion
        # so concurrent calls (which the core forbids anyway) don't leak.
        self._stream_cb: StreamCallback | None = None
        self._stream_lock = asyncio.Lock()

    # -- lifecycle -------------------------------------------------------

    async def __aenter__(self) -> "Agent":
        await self.start()
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        await self.close()

    async def start(self) -> None:
        """Spawn the agent subprocess and wire up callbacks."""

        await self._rpc.connect_subprocess(
            self._binary_path,
            args=["--rpc"],
            env=self._env,
            cwd=self._cwd,
        )
        self._wire_handlers()

    async def close(self) -> None:
        """Terminate the subprocess and release resources."""

        await self._rpc.close()

    # -- configuration ---------------------------------------------------

    async def configure(self, config: AgentConfig) -> list[str]:
        """Apply ``agent.configure``.

        Returns the ``applied`` list reported by the core (the names of
        config fields that took effect).
        """

        params = config.to_params()
        result = await self._rpc.call(_M_AGENT_CONFIGURE, params)
        return list(result.get("applied", []))

    # -- run / abort -----------------------------------------------------

    async def run(
        self,
        prompt: str,
        *,
        max_turns: int | None = None,
        stream: StreamCallback | None = None,
        on_status: StatusCallback | None = None,
        timeout: float | None = None,
    ) -> AgentResult:
        """Execute ``agent.run`` and return the final result.

        Args:
            prompt: User prompt for this turn.
            max_turns: Per-call override for the configured max turns.
            stream: Optional callback ``(text_chunk, turn)`` invoked for each
                ``stream.delta`` notification while the run is in progress.
                Streaming must also be enabled via
                ``agent.configure(streaming=...)`` for deltas to be sent.
            on_status: Optional callback ``(usage_ratio, count, limit)`` for
                ``context.status`` notifications.
            timeout: Optional overall timeout (seconds) for the request.
        """

        # Only one stream callback at a time (the core forbids concurrent runs anyway).
        async with self._stream_lock:
            self._stream_cb = stream
            previous_status = self._on_status
            if on_status is not None:
                self._on_status = on_status
            try:
                params: dict[str, Any] = {"prompt": prompt}
                if max_turns is not None:
                    params["max_turns"] = max_turns
                raw = await self._rpc.call(_M_AGENT_RUN, params, timeout=timeout)
            finally:
                self._stream_cb = None
                self._on_status = previous_status

        usage_dict = raw.get("usage") or {}
        return AgentResult(
            response=raw.get("response", ""),
            reason=raw.get("reason", ""),
            turns=int(raw.get("turns", 0)),
            usage=UsageInfo(
                prompt_tokens=int(usage_dict.get("prompt_tokens", 0)),
                completion_tokens=int(usage_dict.get("completion_tokens", 0)),
                total_tokens=int(usage_dict.get("total_tokens", 0)),
            ),
        )

    async def abort(self, reason: str = "") -> bool:
        """Cancel the in-flight ``agent.run``.

        Returns the ``aborted`` flag from the core (``False`` if no run was
        actually in progress).
        """

        params: dict[str, Any] = {}
        if reason:
            params["reason"] = reason
        raw = await self._rpc.call(_M_AGENT_ABORT, params)
        return bool(raw.get("aborted", False))

    # -- registration ----------------------------------------------------

    async def register_tools(self, *tools: Any) -> int:
        """Register ``@tool``-decorated callables with the core.

        Pass either the decorated functions (any object with the
        ``__ai_agent_tool__`` attribute) or :class:`ToolDefinition` instances
        directly.
        """

        defs: list[ToolDefinition] = []
        for t in tools:
            if isinstance(t, ToolDefinition):
                defn = t
            else:
                defn = get_tool_definition(t)
                if defn is None:
                    raise AgentError(
                        f"object {t!r} is not decorated with @tool"
                    )
            defs.append(defn)

        for defn in defs:
            self._tools[defn.name] = defn

        params = {"tools": [d.to_protocol_dict() for d in defs]}
        raw = await self._rpc.call(_M_TOOL_REGISTER, params)
        return int(raw.get("registered", 0))

    async def register_guards(self, *guards: Any) -> int:
        """Register guard callables decorated with ``@input_guard`` etc."""

        defs: list[GuardDefinition] = []
        for g in guards:
            if isinstance(g, GuardDefinition):
                defn = g
            else:
                defn = get_guard_definition(g)
                if defn is None:
                    raise AgentError(
                        f"object {g!r} is not decorated with an @*_guard"
                    )
            defs.append(defn)

        for defn in defs:
            self._guards[defn.name] = defn

        params = {"guards": [d.to_protocol_dict() for d in defs]}
        raw = await self._rpc.call(_M_GUARD_REGISTER, params)
        return int(raw.get("registered", 0))

    async def register_verifiers(self, *verifiers: Any) -> int:
        """Register verifier callables decorated with ``@verifier``."""

        defs: list[VerifierDefinition] = []
        for v in verifiers:
            if isinstance(v, VerifierDefinition):
                defn = v
            else:
                defn = get_verifier_definition(v)
                if defn is None:
                    raise AgentError(
                        f"object {v!r} is not decorated with @verifier"
                    )
            defs.append(defn)

        for defn in defs:
            self._verifiers[defn.name] = defn

        params = {"verifiers": [d.to_protocol_dict() for d in defs]}
        raw = await self._rpc.call(_M_VERIFIER_REGISTER, params)
        return int(raw.get("registered", 0))

    async def register_mcp(
        self,
        command: str | None = None,
        args: list[str] | tuple[str, ...] = (),
        *,
        env: dict[str, str] | None = None,
        transport: str = "stdio",
        url: str | None = None,
    ) -> list[str]:
        """Register an external MCP server as a tool source.

        For ``transport="stdio"`` (default), supply ``command`` and optional
        ``args`` / ``env``. For ``transport="sse"``, supply ``url``.

        Returns the list of tool names exposed by the MCP server.
        """

        params: dict[str, Any] = {"transport": transport}
        if command is not None:
            params["command"] = command
        if args:
            params["args"] = list(args)
        if env:
            params["env"] = dict(env)
        if url is not None:
            params["url"] = url
        raw = await self._rpc.call(_M_MCP_REGISTER, params)
        return list(raw.get("tools", []))

    # -- internal: handler wiring ---------------------------------------

    def _wire_handlers(self) -> None:
        self._rpc.set_request_handler(_M_TOOL_EXECUTE, self._handle_tool_execute)
        self._rpc.set_request_handler(_M_GUARD_EXECUTE, self._handle_guard_execute)
        self._rpc.set_request_handler(
            _M_VERIFIER_EXECUTE, self._handle_verifier_execute
        )

        self._rpc.set_notification_handler(_N_STREAM_DELTA, self._handle_stream_delta)
        self._rpc.set_notification_handler(_N_STREAM_END, self._handle_stream_end)
        self._rpc.set_notification_handler(
            _N_CONTEXT_STATUS, self._handle_context_status
        )

    async def _handle_tool_execute(self, params: dict[str, Any]) -> dict[str, Any]:
        name = params.get("name", "")
        args = params.get("args") or {}
        defn = self._tools.get(name)
        if defn is None:
            return {
                "content": f"tool not found: {name}",
                "is_error": True,
            }
        try:
            result = await defn.call(args)
        except Exception as exc:  # noqa: BLE001
            return {
                "content": f"tool execution failed: {exc}",
                "is_error": True,
            }
        return _coerce_tool_result(result)

    async def _handle_guard_execute(self, params: dict[str, Any]) -> dict[str, Any]:
        name = params.get("name", "")
        stage = params.get("stage", "")
        defn = self._guards.get(name)
        if defn is None or defn.stage != stage:
            return {
                "decision": "deny",
                "reason": f"guard not found: {name}/{stage}",
            }
        try:
            decision, reason = await defn.call(
                input=params.get("input", ""),
                tool_name=params.get("tool_name", ""),
                args=params.get("args") or {},
                output=params.get("output", ""),
            )
        except Exception as exc:  # noqa: BLE001
            return {"decision": "deny", "reason": f"guard error: {exc}"}
        return {"decision": decision, "reason": reason}

    async def _handle_verifier_execute(self, params: dict[str, Any]) -> dict[str, Any]:
        name = params.get("name", "")
        defn = self._verifiers.get(name)
        if defn is None:
            return {"passed": False, "summary": f"verifier not found: {name}"}
        try:
            passed, summary = await defn.call(
                tool_name=params.get("tool_name", ""),
                args=params.get("args") or {},
                result=params.get("result", ""),
            )
        except Exception as exc:  # noqa: BLE001
            return {"passed": False, "summary": f"verifier error: {exc}"}
        return {"passed": passed, "summary": summary}

    async def _handle_stream_delta(self, params: dict[str, Any]) -> None:
        cb = self._stream_cb
        if cb is None:
            return
        text = params.get("text", "")
        turn = int(params.get("turn", 0))
        ret = cb(text, turn)
        if asyncio.iscoroutine(ret):
            await ret

    async def _handle_stream_end(self, params: dict[str, Any]) -> None:
        # Currently unused at the SDK surface; the core also returns these
        # fields in the ``agent.run`` result. Hook left in case applications
        # want to listen via subclassing.
        return

    async def _handle_context_status(self, params: dict[str, Any]) -> None:
        cb = self._on_status
        if cb is None:
            return
        usage_ratio = float(params.get("usage_ratio", 0.0))
        count = int(params.get("token_count", 0))
        limit = int(params.get("token_limit", 0))
        ret = cb(usage_ratio, count, limit)
        if asyncio.iscoroutine(ret):
            await ret

    # -- introspection --------------------------------------------------

    @property
    def stderr_output(self) -> str:
        """Captured stderr from the subprocess (handy for debugging)."""

        return self._rpc.stderr_output


def _coerce_tool_result(raw: Any) -> dict[str, Any]:
    """Normalise tool return values into ``ToolExecuteResult`` shape.

    Acceptable user return values:

    * ``str``                   -> ``{"content": value}``
    * ``dict`` with ``content`` -> passed through (with type coercion)
    * anything else             -> ``{"content": str(value)}``
    """

    if isinstance(raw, str):
        return {"content": raw, "is_error": False}
    if isinstance(raw, dict) and "content" in raw:
        out: dict[str, Any] = {"content": str(raw["content"])}
        if "is_error" in raw:
            out["is_error"] = bool(raw["is_error"])
        if "metadata" in raw and raw["metadata"] is not None:
            out["metadata"] = dict(raw["metadata"])
        return out
    return {"content": "" if raw is None else str(raw), "is_error": False}


# Re-export GuardDenied for users who want to raise it from their guards.
__all__ = [
    "Agent",
    "AgentResult",
    "UsageInfo",
    "GuardDenied",
]
