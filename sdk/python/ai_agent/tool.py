"""``@tool`` decorator and JSON-Schema generation.

Decorate any sync or async function with ``@tool(...)`` to make it
registrable with ``Agent.register_tools``. The function's signature and type
hints are used to derive a minimal JSON-Schema (``type: "object"``) suitable
for the Go core's ``ToolDefinition.parameters`` field.

Mapping (PEP 604 unions are also handled):

* ``str``               -> ``{"type": "string"}``
* ``int``               -> ``{"type": "integer"}``
* ``float``             -> ``{"type": "number"}``
* ``bool``              -> ``{"type": "boolean"}``
* ``list``/``list[T]``  -> ``{"type": "array", "items": <T schema>}``
* ``dict``/``dict[...]``-> ``{"type": "object"}``
* ``None``              -> ``{"type": "null"}``
* ``Literal["a","b"]``  -> ``{"type": "string", "enum": ["a", "b"]}``
* ``Enum`` subclass     -> ``{"type": "string", "enum": [<member values>]}``
* ``X | None`` (Optional)-> base schema for X; field is dropped from ``required``.
* anything else         -> ``{}`` (open schema; the core will pass through)
"""

from __future__ import annotations

import asyncio
import enum
import inspect
import types
import typing
from dataclasses import dataclass, field
from typing import Any, Callable, Literal, get_args, get_origin


# --- JSON-Schema mapping ----------------------------------------------------


_PRIMITIVE_MAP: dict[type, dict[str, Any]] = {
    str: {"type": "string"},
    int: {"type": "integer"},
    float: {"type": "number"},
    bool: {"type": "boolean"},
    type(None): {"type": "null"},
}


def _is_optional(annotation: Any) -> tuple[bool, Any]:
    """Return ``(is_optional, inner_type)`` for ``X | None`` style annotations.

    Handles both ``typing.Optional[X]`` / ``typing.Union[X, None]`` and the
    PEP 604 ``X | None`` syntax (``types.UnionType``). For unions with more
    than two members or no ``None`` member returns ``(False, annotation)``.
    """

    origin = get_origin(annotation)
    if origin is typing.Union or origin is types.UnionType:  # type: ignore[attr-defined]
        args = [a for a in get_args(annotation) if a is not type(None)]
        has_none = any(a is type(None) for a in get_args(annotation))
        if has_none and len(args) == 1:
            return True, args[0]
    return False, annotation


def annotation_to_schema(annotation: Any) -> dict[str, Any]:
    """Map a Python annotation to a JSON-Schema fragment.

    Optional/union handling is done by the caller (``_build_parameters``);
    here we always assume a non-optional type.
    """

    if annotation is inspect.Parameter.empty or annotation is Any:
        return {}

    # Strip Optional wrapper here too in case the caller didn't.
    is_opt, inner = _is_optional(annotation)
    if is_opt:
        annotation = inner

    if annotation in _PRIMITIVE_MAP:
        return dict(_PRIMITIVE_MAP[annotation])

    origin = get_origin(annotation)

    # list[T] / List[T]
    if origin in (list, typing.List):  # type: ignore[attr-defined]
        args = get_args(annotation)
        item_schema = annotation_to_schema(args[0]) if args else {}
        schema: dict[str, Any] = {"type": "array"}
        if item_schema:
            schema["items"] = item_schema
        return schema

    # tuple[T, ...] – treat as homogeneous array
    if origin in (tuple, typing.Tuple):  # type: ignore[attr-defined]
        args = get_args(annotation)
        if len(args) == 2 and args[1] is Ellipsis:
            return {"type": "array", "items": annotation_to_schema(args[0])}
        return {"type": "array"}

    # dict[K, V] – JSON Schema only models string keys; treat as object.
    if origin in (dict, typing.Dict):  # type: ignore[attr-defined]
        return {"type": "object"}

    # bare list/dict
    if annotation is list:
        return {"type": "array"}
    if annotation is dict:
        return {"type": "object"}

    # Literal["a", "b", ...] – derive enum type from the first value.
    if origin is Literal:
        values = list(get_args(annotation))
        if not values:
            return {}
        # Determine JSON type from the first literal value.
        first = values[0]
        if isinstance(first, bool):
            json_type = "boolean"
        elif isinstance(first, int):
            json_type = "integer"
        elif isinstance(first, float):
            json_type = "number"
        else:
            json_type = "string"
        return {"type": json_type, "enum": values}

    # Enum subclasses – use member values.
    if isinstance(annotation, type) and issubclass(annotation, enum.Enum):
        values = [m.value for m in annotation]
        if not values:
            return {}
        first = values[0]
        if isinstance(first, bool):
            json_type = "boolean"
        elif isinstance(first, int):
            json_type = "integer"
        elif isinstance(first, float):
            json_type = "number"
        else:
            json_type = "string"
        return {"type": json_type, "enum": values}

    # Unknown/complex types: open schema (the wrapper will pass args through).
    return {}


def _build_parameters(func: Callable[..., Any]) -> dict[str, Any]:
    """Build a JSON-Schema object for ``func``'s parameters.

    Skips ``self``/``cls``, ``*args`` and ``**kwargs``. A parameter without a
    default value goes into ``required``; ``X | None`` parameters always drop
    their ``required`` entry, even with no default.

    Annotations that are still strings (e.g. when the calling module uses
    ``from __future__ import annotations``) are resolved via
    ``inspect.signature(eval_str=True)``.
    """

    try:
        sig = inspect.signature(func, eval_str=True)
    except (NameError, SyntaxError, TypeError):
        # Fall back to raw (string) annotations if we can't resolve them.
        sig = inspect.signature(func)
    properties: dict[str, dict[str, Any]] = {}
    required: list[str] = []

    for name, param in sig.parameters.items():
        if name in ("self", "cls"):
            continue
        if param.kind in (
            inspect.Parameter.VAR_POSITIONAL,
            inspect.Parameter.VAR_KEYWORD,
        ):
            continue

        annotation = param.annotation
        is_opt, inner = _is_optional(annotation)
        schema = annotation_to_schema(inner if is_opt else annotation)
        properties[name] = schema

        if param.default is inspect.Parameter.empty and not is_opt:
            required.append(name)

    out: dict[str, Any] = {
        "type": "object",
        "properties": properties,
        "additionalProperties": False,
    }
    if required:
        out["required"] = required
    return out


# --- ToolDefinition --------------------------------------------------------


@dataclass
class ToolDefinition:
    """An SDK-side tool record produced by ``@tool``.

    ``Agent.register_tools`` consumes a list of these and forwards
    ``(name, description, parameters, read_only)`` to ``tool.register``.
    """

    name: str
    description: str
    parameters: dict[str, Any]
    read_only: bool
    func: Callable[..., Any] = field(repr=False)
    is_coroutine: bool = field(repr=False, default=False)

    async def call(self, args: dict[str, Any]) -> Any:
        """Invoke the underlying function with kwargs unpacked from ``args``.

        Sync functions are run in the default executor so the JSON-RPC reader
        loop is never blocked.
        """

        if self.is_coroutine:
            return await self.func(**args)
        loop = asyncio.get_running_loop()
        return await loop.run_in_executor(None, lambda: self.func(**args))

    def to_protocol_dict(self) -> dict[str, Any]:
        """Return the dict shape the core expects for ``tool.register``."""

        return {
            "name": self.name,
            "description": self.description,
            "parameters": self.parameters,
            "read_only": self.read_only,
        }


# --- decorator -------------------------------------------------------------


def tool(
    *,
    name: str | None = None,
    description: str = "",
    read_only: bool = False,
    parameters: dict[str, Any] | None = None,
) -> Callable[[Callable[..., Any]], Callable[..., Any]]:
    """Decorate a function as a registrable agent tool.

    Args:
        name: Tool name exposed to the agent. Defaults to the function name.
        description: Human-readable description (shown to the model).
        read_only: ``True`` if the tool has no observable side effects. The
            core uses this for permission auto-approval.
        parameters: Optional explicit JSON-Schema. When omitted, a schema is
            derived from the function signature.

    The returned object is the original function with a ``__ai_agent_tool__``
    attribute holding the :class:`ToolDefinition`. The function itself remains
    callable as before.
    """

    def decorator(func: Callable[..., Any]) -> Callable[..., Any]:
        params = parameters if parameters is not None else _build_parameters(func)
        if description:
            desc = description
        elif func.__doc__:
            desc = func.__doc__.strip().splitlines()[0]
        else:
            desc = ""
        defn = ToolDefinition(
            name=name or func.__name__,
            description=desc,
            parameters=params,
            read_only=read_only,
            func=func,
            is_coroutine=asyncio.iscoroutinefunction(func),
        )
        setattr(func, "__ai_agent_tool__", defn)
        return func

    return decorator


def get_tool_definition(obj: Any) -> ToolDefinition | None:
    """Return the :class:`ToolDefinition` attached to ``obj`` by ``@tool``."""

    return getattr(obj, "__ai_agent_tool__", None)


__all__ = [
    "tool",
    "ToolDefinition",
    "annotation_to_schema",
    "get_tool_definition",
]
