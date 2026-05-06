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
* ``-32005`` Guard denied          -> ``GuardDenied``
* ``-32006`` Tripwire fired        -> ``TripwireTriggered`` (subclass of ``GuardDenied``)
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
    """Raised when an input/output guard denies a prompt.

    Attributes:
        decision: ``"deny"`` or ``"tripwire"``.
        reason:   Human-readable explanation from the guard implementation.

    For tripwire events use :class:`TripwireTriggered` (a subclass) so you
    can distinguish them with ``except TripwireTriggered``.

    Example::

        try:
            result = await agent.input(user_prompt)
        except TripwireTriggered as e:
            alert_security_team(e.reason)
        except GuardDenied as e:
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


class TripwireTriggered(GuardDenied):
    """Raised when a tripwire guard fires (security alert).

    ``TripwireTriggered`` is a subclass of :class:`GuardDenied`, so existing
    ``except GuardDenied`` blocks continue to catch it.  Handlers that need to
    distinguish security events from ordinary denials can use::

        except TripwireTriggered as e:
            alert_security_team(e.reason)
        except GuardDenied:
            ...

    Maps to JSON-RPC error code ``-32006``.
    """

    def __init__(
        self,
        message: str,
        *,
        reason: str = "",
        code: int | None = None,
        data: Any | None = None,
    ) -> None:
        super().__init__(message, decision="tripwire", reason=reason, code=code, data=data)


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

    if code == _ERR_TRIPWIRE:
        reason = message
        if isinstance(data, dict):
            reason = data.get("reason", reason)
        return TripwireTriggered(message, reason=reason, code=code, data=data)

    if code == _ERR_GUARD_DENIED:
        decision = "deny"
        reason = message
        if isinstance(data, dict):
            reason = data.get("reason", reason)
            decision = data.get("decision", decision)
        return GuardDenied(message, decision=decision, reason=reason, code=code, data=data)

    cls = _CODE_TO_CLASS.get(code, AgentError)
    return cls(message, code=code, data=data)
