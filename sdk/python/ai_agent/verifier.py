"""``@verifier`` decorator.

A verifier is a wrapper-side function the core invokes via
``verifier.execute`` after a tool produced a result. It returns
``(passed, summary)``.

Signature (sync or async):

    @verifier(name="non_empty")
    def check(tool_name: str, args: dict, result: str) -> tuple[bool, str]: ...
"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass, field
from typing import Any, Awaitable, Callable

VerifierCallable = Callable[..., "tuple[bool, str] | Awaitable[tuple[bool, str]]"]


@dataclass
class VerifierDefinition:
    """SDK-side record of a registered verifier."""

    name: str
    func: VerifierCallable = field(repr=False)
    is_coroutine: bool = field(repr=False, default=False)

    async def call(
        self, *, tool_name: str, args: Any | None, result: str
    ) -> tuple[bool, str]:
        """Invoke the wrapped function and validate the return shape."""

        if self.is_coroutine:
            passed, summary = await self.func(
                tool_name=tool_name, args=args or {}, result=result
            )
        else:
            loop = asyncio.get_running_loop()
            passed, summary = await loop.run_in_executor(
                None,
                lambda: self.func(tool_name=tool_name, args=args or {}, result=result),
            )

        return bool(passed), str(summary or "")

    def to_protocol_dict(self) -> dict[str, Any]:
        return {"name": self.name}


def verifier(*, name: str | None = None):
    """Decorate a function as a registrable verifier.

    Args:
        name: Verifier name exposed to the core. Defaults to ``func.__name__``.

    The decorated function must accept ``tool_name``, ``args``, ``result``
    keyword arguments and return ``(passed, summary)``.
    """

    def decorator(func: Callable[..., Any]) -> Callable[..., Any]:
        defn = VerifierDefinition(
            name=name or func.__name__,
            func=func,
            is_coroutine=asyncio.iscoroutinefunction(func),
        )
        setattr(func, "__ai_agent_verifier__", defn)
        return func

    return decorator


def get_verifier_definition(obj: Any) -> VerifierDefinition | None:
    """Return the :class:`VerifierDefinition` attached to ``obj`` by ``@verifier``."""

    return getattr(obj, "__ai_agent_verifier__", None)


__all__ = [
    "verifier",
    "VerifierDefinition",
    "get_verifier_definition",
]
