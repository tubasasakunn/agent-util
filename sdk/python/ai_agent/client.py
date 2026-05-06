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
_M_CONTEXT_SUMMARIZE = "context.summarize"
_M_JUDGE_REGISTER = "judge.register"
_M_JUDGE_EVALUATE = "judge.evaluate"
_N_STREAM_DELTA = "stream.delta"
_N_STREAM_END = "stream.end"
_N_CONTEXT_STATUS = "context.status"

# GoalJudge callable type: (response: str, turn: int) -> (terminate: bool, reason: str)
GoalJudgeCallable = Callable[[str, int], Any]


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
        self._judges: dict[str, GoalJudgeCallable] = {}

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

    async def summarize(self) -> str:
        """現在の会話履歴を LLM で要約して返す。"""
        raw = await self._rpc.call(_M_CONTEXT_SUMMARIZE, {})
        return str(raw.get("summary", ""))

    # -- registration ----------------------------------------------------

    async def _register_definitions(
        self,
        defs: list[Any],
        store: dict[str, Any],
        rpc_method: str,
        rpc_list_key: str,
    ) -> list[str]:
        """共通登録ヘルパー。defs を store に格納してコアへ RPC 送信する。

        Returns:
            登録されたアイテムの名前リスト。
        """
        for defn in defs:
            store[defn.name] = defn
        params = {rpc_list_key: [d.to_protocol_dict() for d in defs]}
        await self._rpc.call(rpc_method, params)
        return [d.name for d in defs]

    async def register_tools(self, *tools: Any) -> list[str]:
        """Register ``@tool``-decorated callables with the core.

        Pass either the decorated functions (any object with the
        ``__ai_agent_tool__`` attribute) or :class:`ToolDefinition` instances
        directly.

        Returns:
            Names of the registered tools.
        """

        defs: list[ToolDefinition] = []
        for t in tools:
            if isinstance(t, ToolDefinition):
                defn = t
            else:
                defn = get_tool_definition(t)
                if defn is None:
                    raise AgentError(
                        f"Cannot register {t!r} as a tool.\n"
                        "  Use @tool decorator:    @tool(description='...')\n"
                        "  Or pass ToolDefinition: ToolDefinition(name=..., func=...)\n"
                        "  Or use Tool class:      Tool(fn, description='...')"
                    )
            defs.append(defn)

        return await self._register_definitions(defs, self._tools, _M_TOOL_REGISTER, "tools")

    async def register_guards(self, *guards: Any) -> list[str]:
        """Register guard callables decorated with ``@input_guard`` etc.

        Returns:
            Names of the registered guards.
        """

        defs: list[GuardDefinition] = []
        for g in guards:
            if isinstance(g, GuardDefinition):
                defn = g
            else:
                defn = get_guard_definition(g)
                if defn is None:
                    raise AgentError(
                        f"Cannot register {g!r} as a guard.\n"
                        "  Use a guard decorator: @input_guard, @tool_call_guard, or @output_guard"
                    )
            defs.append(defn)

        return await self._register_definitions(defs, self._guards, _M_GUARD_REGISTER, "guards")

    async def register_verifiers(self, *verifiers: Any) -> list[str]:
        """Register verifier callables decorated with ``@verifier``.

        Returns:
            Names of the registered verifiers.
        """

        defs: list[VerifierDefinition] = []
        for v in verifiers:
            if isinstance(v, VerifierDefinition):
                defn = v
            else:
                defn = get_verifier_definition(v)
                if defn is None:
                    raise AgentError(
                        f"Cannot register {v!r} as a verifier.\n"
                        "  Use @verifier decorator: @verifier(description='...')"
                    )
            defs.append(defn)

        return await self._register_definitions(
            defs, self._verifiers, _M_VERIFIER_REGISTER, "verifiers"
        )

    async def register_judge(self, name: str, handler: GoalJudgeCallable) -> None:
        """Register a goal-judge callable under *name*.

        *handler* receives ``(response: str, turn: int)`` and must return
        ``(terminate: bool, reason: str)`` — sync or async.

        After registration call ``configure(AgentConfig(judge=JudgeConfig(name=name)))``
        to activate it.
        """
        self._judges[name] = handler
        await self._rpc.call(_M_JUDGE_REGISTER, {"name": name})

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
        self._rpc.set_request_handler(_M_JUDGE_EVALUATE, self._handle_judge_evaluate)

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
            registered = sorted(self._tools)
            return {
                "content": f"tool not found: {name!r} (registered: {registered})",
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
            registered = sorted(f"{g.name}/{g.stage}" for g in self._guards.values())
            return {
                "decision": "deny",
                "reason": f"guard not found: {name!r} stage={stage!r} (registered: {registered})",
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
            registered = sorted(self._verifiers)
            return {"passed": False, "summary": f"verifier not found: {name!r} (registered: {registered})"}
        try:
            passed, summary = await defn.call(
                tool_name=params.get("tool_name", ""),
                args=params.get("args") or {},
                result=params.get("result", ""),
            )
        except Exception as exc:  # noqa: BLE001
            return {"passed": False, "summary": f"verifier error: {exc}"}
        return {"passed": passed, "summary": summary}

    async def _handle_judge_evaluate(self, params: dict[str, Any]) -> dict[str, Any]:
        name = params.get("name", "")
        handler = self._judges.get(name)
        if handler is None:
            registered = sorted(self._judges)
            return {"terminate": False, "reason": f"judge not found: {name!r} (registered: {registered})"}
        try:
            response = params.get("response", "")
            turn = int(params.get("turn", 0))
            ret = handler(response, turn)
            if asyncio.iscoroutine(ret):
                ret = await ret
            terminate, reason = ret
        except Exception as exc:  # noqa: BLE001
            return {"terminate": False, "reason": f"judge error: {exc}"}
        return {"terminate": bool(terminate), "reason": str(reason)}

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
