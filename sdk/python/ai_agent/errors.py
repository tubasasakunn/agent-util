"""Exception types for the ai-agent SDK.

The SDK wraps low-level transport / RPC errors into a small hierarchy so that
user code can use ``except AgentError`` for "anything from the SDK" and still
match more precise subclasses for known JSON-RPC error codes.

Mapping to the JSON-RPC errors defined in ``pkg/protocol/errors.go``:

* ``-32700`` Parse error           -> ``AgentError``
* ``-32600`` Invalid request       -> ``AgentError``
* ``-32601`` Method not found      -> ``AgentError``
* ``-32602`` Invalid params        -> ``AgentError``
* ``-32603`` Internal error        -> ``AgentError``
* ``-32000`` Tool not found        -> ``ToolError``
* ``-32001`` Tool exec failed      -> ``ToolError``
* ``-32002`` Agent already running -> ``AgentBusy``
* ``-32003`` Aborted               -> ``AgentAborted``
* ``-32004`` Message too large     -> ``AgentError``
* ``-32005`` Guard denied          -> ``GuardDenied(decision="deny")``
* ``-32006`` Tripwire fired        -> ``GuardDenied(decision="tripwire")``
"""

from __future__ import annotations

from typing import Any


class AgentError(Exception):
    """Base error for the ai-agent SDK.

    Attributes:
        code: JSON-RPC error code, or ``None`` for SDK-internal errors.
        data: Optional structured ``data`` field from the JSON-RPC error.
    """

    code: int | None = None

    def __init__(
        self,
        message: str,
        *,
        code: int | None = None,
        data: Any | None = None,
    ) -> None:
        super().__init__(message)
        self.code = code
        self.data = data


class AgentBusy(AgentError):
    """Raised when ``agent.run`` is invoked while another run is in progress.

    Maps to JSON-RPC error code ``-32002``.
    """


class AgentAborted(AgentError):
    """Raised when an in-flight ``agent.run`` was cancelled via ``agent.abort``.

    Maps to JSON-RPC error code ``-32003``.
    """


class ToolError(AgentError):
    """Raised on tool-related failures.

    Maps to JSON-RPC error codes ``-32000`` (tool not found) and
    ``-32001`` (tool execution failed).
    """


class GuardDenied(AgentError):
    """Raised when an input guard denies a prompt or a tripwire fires.

    Attributes:
        decision: ``"deny"`` (prompt blocked) or ``"tripwire"`` (alert but
            the run may have already started; treat as a security event).
        reason:   Human-readable explanation from the guard implementation.

    Example::

        try:
            result = await agent.input(user_prompt)
        except GuardDenied as e:
            if e.decision == "tripwire":
                alert_security_team(e.reason)
            else:
                return "申し訳ありませんが、そのリクエストはお受けできません。"
    """

    def __init__(
        self,
        message: str,
        *,
        decision: str = "deny",
        reason: str = "",
        code: int | None = None,
        data: Any | None = None,
    ) -> None:
        super().__init__(message, code=code, data=data)
        self.decision = decision
        self.reason = reason

    def __str__(self) -> str:
        base = super().__str__()
        parts = [f"[{self.decision}] {base}"]
        if self.reason:
            parts.append(f"reason: {self.reason}")
        return " — ".join(parts)


# Code -> class mapping used by jsonrpc.py to translate error responses.
_CODE_TO_CLASS: dict[int, type[AgentError]] = {
    -32000: ToolError,
    -32001: ToolError,
    -32002: AgentBusy,
    -32003: AgentAborted,
}

# Guard/tripwire error codes (mirrors pkg/protocol/errors.go).
_ERR_GUARD_DENIED = -32005
_ERR_TRIPWIRE = -32006


def from_rpc_error(code: int, message: str, data: Any | None = None) -> AgentError:
    """Convert a JSON-RPC error tuple into the most specific SDK exception."""

    if code in (_ERR_GUARD_DENIED, _ERR_TRIPWIRE):
        decision = "tripwire" if code == _ERR_TRIPWIRE else "deny"
        reason = message
        if isinstance(data, dict):
            reason = data.get("reason", reason)
            decision = data.get("decision", decision)
        return GuardDenied(message, decision=decision, reason=reason, code=code, data=data)

    cls = _CODE_TO_CLASS.get(code, AgentError)
    return cls(message, code=code, data=data)
