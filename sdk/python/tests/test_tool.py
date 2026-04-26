"""Tests for ``@tool`` decorator and JSON-Schema generation."""

from __future__ import annotations

import asyncio
from typing import Optional

import pytest

from ai_agent.tool import (
    ToolDefinition,
    annotation_to_schema,
    get_tool_definition,
    tool,
)


def test_primitive_schema_mapping() -> None:
    assert annotation_to_schema(str) == {"type": "string"}
    assert annotation_to_schema(int) == {"type": "integer"}
    assert annotation_to_schema(float) == {"type": "number"}
    assert annotation_to_schema(bool) == {"type": "boolean"}


def test_list_with_items() -> None:
    assert annotation_to_schema(list[int]) == {
        "type": "array",
        "items": {"type": "integer"},
    }
    assert annotation_to_schema(list) == {"type": "array"}


def test_dict_treated_as_object() -> None:
    assert annotation_to_schema(dict[str, int]) == {"type": "object"}
    assert annotation_to_schema(dict) == {"type": "object"}


def test_unknown_type_yields_open_schema() -> None:
    class Custom: ...

    assert annotation_to_schema(Custom) == {}


def test_tool_decorator_attaches_definition_and_schema() -> None:
    @tool(description="Read a file", read_only=True)
    def read_file(path: str, encoding: str = "utf-8") -> str:
        return ""

    defn = get_tool_definition(read_file)
    assert defn is not None
    assert defn.name == "read_file"
    assert defn.description == "Read a file"
    assert defn.read_only is True
    schema = defn.parameters
    assert schema["type"] == "object"
    assert schema["properties"]["path"] == {"type": "string"}
    assert schema["properties"]["encoding"] == {"type": "string"}
    assert schema["required"] == ["path"]
    assert schema["additionalProperties"] is False


def test_tool_decorator_optional_field_not_required() -> None:
    @tool()
    def f(a: str, b: Optional[int] = None) -> str:
        return ""

    defn = get_tool_definition(f)
    assert defn is not None
    assert defn.parameters["required"] == ["a"]
    assert defn.parameters["properties"]["b"] == {"type": "integer"}


def test_tool_decorator_pep604_optional() -> None:
    @tool()
    def f(a: str, b: int | None = None) -> str:
        return ""

    defn = get_tool_definition(f)
    assert defn is not None
    assert defn.parameters["required"] == ["a"]


def test_tool_explicit_parameters_override() -> None:
    custom = {"type": "object", "properties": {"foo": {"type": "string"}}}

    @tool(parameters=custom)
    def f(foo: str) -> str:
        return ""

    defn = get_tool_definition(f)
    assert defn is not None
    assert defn.parameters == custom


def test_tool_protocol_dict_shape() -> None:
    @tool(description="d", read_only=True)
    def echo(text: str) -> str:
        return text

    defn = get_tool_definition(echo)
    assert defn is not None
    proto = defn.to_protocol_dict()
    assert set(proto) == {"name", "description", "parameters", "read_only"}
    assert proto["read_only"] is True


@pytest.mark.asyncio
async def test_tool_call_async_function() -> None:
    @tool()
    async def add(a: int, b: int) -> str:
        return str(a + b)

    defn = get_tool_definition(add)
    assert defn is not None
    assert defn.is_coroutine is True
    result = await defn.call({"a": 2, "b": 3})
    assert result == "5"


@pytest.mark.asyncio
async def test_tool_call_sync_function_runs_in_executor() -> None:
    @tool()
    def upper(s: str) -> str:
        return s.upper()

    defn = get_tool_definition(upper)
    assert defn is not None
    assert defn.is_coroutine is False
    result = await defn.call({"s": "hi"})
    assert result == "HI"


def test_tool_definition_is_dataclass() -> None:
    @tool()
    def f(x: str) -> str:
        return x

    defn = get_tool_definition(f)
    assert isinstance(defn, ToolDefinition)
