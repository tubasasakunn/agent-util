"""Guard decorators: ``@input_guard``, ``@tool_call_guard``, ``@output_guard``.

A guard is a wrapper-side function the core invokes via ``guard.execute`` to
decide whether a prompt / tool call / output should be allowed, denied, or
should trip the safety wire (immediate stop).

Each guard returns a 2-tuple ``(decision, reason)`` where ``decision`` is one
of ``"allow"``, ``"deny"``, ``"tripwire"``. ``reason`` is a short human-
readable explanation that the core may surface to the user.

Stage-specific signatures (sync or async — both are supported):

    @input_guard(name="no_secrets")
    def check_input(input: str) -> tuple[str, str]: ...

    @tool_call_guard(name="fs_root_only")
    def check_tool_call(tool_name: str, args: dict) -> tuple[str, str]: ...

    @output_guard(name="pii_redactor")
    def check_output(output: str) -> tuple[str, str]: ...
"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass, field
from typing import Any, Awaitable, Callable

# Stage identifiers (must match pkg/protocol/methods.go).
STAGE_INPUT = "input"
STAGE_TOOL_CALL = "tool_call"
STAGE_OUTPUT = "output"

VALID_DECISIONS = frozenset({"allow", "deny", "tripwire"})


GuardCallable = Callable[..., "tuple[str, str] | Awaitable[tuple[str, str]]"]


@dataclass
class GuardDefinition:
    """SDK-side record of a registered guard."""

    name: str
    stage: str
    func: GuardCallable = field(repr=False)
    is_coroutine: bool = field(repr=False, default=False)

    async def call(self, *, input: str = "", tool_name: str = "",
                   args: Any | None = None, output: str = "") -> tuple[str, str]:
        """Dispatch the wrapped function with the right kwargs for the stage.

        Sync functions run in the default executor to avoid blocking the
        JSON-RPC reader loop.
        """

        if self.stage == STAGE_INPUT:
            kwargs = {"input": input}
        elif self.stage == STAGE_TOOL_CALL:
            kwargs = {"tool_name": tool_name, "args": args or {}}
        elif self.stage == STAGE_OUTPUT:
            kwargs = {"output": output}
        else:  # pragma: no cover — guarded at decoration time.
            raise ValueError(f"unknown guard stage: {self.stage}")

        if self.is_coroutine:
            decision, reason = await self.func(**kwargs)
        else:
            loop = asyncio.get_running_loop()
            decision, reason = await loop.run_in_executor(
                None, lambda: self.func(**kwargs)
            )

        if decision not in VALID_DECISIONS:
            # Fail-closed: unknown decisions are treated as deny.
            return "deny", f"invalid guard decision {decision!r}: {reason}"
        return decision, reason

    def to_protocol_dict(self) -> dict[str, Any]:
        return {"name": self.name, "stage": self.stage}


def _make_decorator(stage: str):
    def decorator_factory(*, name: str | None = None):
        def decorator(func: Callable[..., Any]) -> Callable[..., Any]:
            defn = GuardDefinition(
                name=name or func.__name__,
                stage=stage,
                func=func,
                is_coroutine=asyncio.iscoroutinefunction(func),
            )
            setattr(func, "__ai_agent_guard__", defn)
            return func

        return decorator

    return decorator_factory


input_guard = _make_decorator(STAGE_INPUT)
tool_call_guard = _make_decorator(STAGE_TOOL_CALL)
output_guard = _make_decorator(STAGE_OUTPUT)


def get_guard_definition(obj: Any) -> GuardDefinition | None:
    """Return the :class:`GuardDefinition` attached to ``obj`` by a guard
    decorator, or ``None`` if absent."""

    return getattr(obj, "__ai_agent_guard__", None)


__all__ = [
    "GuardCallable",
    "GuardDefinition",
    "STAGE_INPUT",
    "STAGE_TOOL_CALL",
    "STAGE_OUTPUT",
    "get_guard_definition",
    "input_guard",
    "output_guard",
    "tool_call_guard",
]
