"""Configuration dataclasses for ``agent.configure``.

These mirror ``pkg/protocol.AgentConfigureParams`` and the nested config
structs. Each ``to_params()`` / ``_strip_none()`` step drops ``None`` values so
the JSON-RPC payload behaves like Go's ``omitempty`` (un-set fields keep their
existing core-side defaults).

JSON-Schema sources (docs/schemas/*.json):

* ``AgentConfigureParams.json``
* ``DelegateConfig.json``
* ``CoordinatorConfig.json``
* ``CompactionConfig.json``
* ``PermissionConfig.json``
* ``GuardsConfig.json``
* ``VerifyConfig.json``
* ``ToolScopeConfig.json``
* ``ReminderConfig.json``
* ``StreamingConfig.json``
"""

from __future__ import annotations

from dataclasses import asdict, dataclass, field, fields, is_dataclass
from typing import Any


def _strip_none(value: Any) -> Any:
    """Recursively drop ``None`` values so JSON output mimics ``omitempty``.

    * dicts: keys with ``None`` values are dropped; nested dicts are recursed.
    * lists/tuples: each element is processed but ``None`` elements are kept
      (the protocol uses ``None`` as a sentinel only at field level).
    * other values: returned as-is.
    """

    if isinstance(value, dict):
        cleaned: dict[str, Any] = {}
        for k, v in value.items():
            if v is None:
                continue
            cleaned[k] = _strip_none(v)
        return cleaned
    if isinstance(value, (list, tuple)):
        return [_strip_none(v) for v in value]
    return value


def _to_dict(obj: Any) -> dict[str, Any]:
    """Convert a dataclass into a dict with ``None`` fields stripped."""

    if not is_dataclass(obj):
        raise TypeError(f"expected dataclass, got {type(obj).__name__}")
    return _strip_none(asdict(obj))


@dataclass
class DelegateConfig:
    """Sub-agent delegate config (docs/schemas/DelegateConfig.json)."""

    enabled: bool | None = None
    max_chars: int | None = None


@dataclass
class CoordinatorConfig:
    """Parallel sub-agent config (docs/schemas/CoordinatorConfig.json)."""

    enabled: bool | None = None
    max_chars: int | None = None


@dataclass
class CompactionConfig:
    """Context compaction cascade config (docs/schemas/CompactionConfig.json)."""

    enabled: bool | None = None
    budget_max_chars: int | None = None
    keep_last: int | None = None
    target_ratio: float | None = None
    summarizer: str | None = None  # "" or "llm"


@dataclass
class PermissionConfig:
    """Permission pipeline config (docs/schemas/PermissionConfig.json)."""

    enabled: bool | None = None
    deny: list[str] | None = None
    allow: list[str] | None = None


@dataclass
class GuardsConfig:
    """Three-stage guardrails (docs/schemas/GuardsConfig.json).

    Each list is a slice of guard names (built-in or wrapper-registered).
    """

    input: list[str] | None = None
    tool_call: list[str] | None = None
    output: list[str] | None = None


@dataclass
class VerifyConfig:
    """Verification loop config (docs/schemas/VerifyConfig.json)."""

    verifiers: list[str] | None = None
    max_step_retries: int | None = None
    max_consecutive_failures: int | None = None


@dataclass
class ToolScopeConfig:
    """Tool scoping config (docs/schemas/ToolScopeConfig.json)."""

    max_tools: int | None = None
    include_always: list[str] | None = None


@dataclass
class ReminderConfig:
    """System reminder config (docs/schemas/ReminderConfig.json)."""

    threshold: int | None = None
    content: str | None = None


@dataclass
class StreamingConfig:
    """Streaming notification config (docs/schemas/StreamingConfig.json)."""

    enabled: bool | None = None
    context_status: bool | None = None


@dataclass
class AgentConfig:
    """Top-level configuration for ``agent.configure``.

    See ``docs/schemas/AgentConfigureParams.json``.
    """

    max_turns: int | None = None
    system_prompt: str | None = None
    token_limit: int | None = None
    work_dir: str | None = None

    delegate: DelegateConfig | None = None
    coordinator: CoordinatorConfig | None = None
    compaction: CompactionConfig | None = None
    permission: PermissionConfig | None = None
    guards: GuardsConfig | None = None
    verify: VerifyConfig | None = None
    tool_scope: ToolScopeConfig | None = None
    reminder: ReminderConfig | None = None
    streaming: StreamingConfig | None = None

    def to_params(self) -> dict[str, Any]:
        """Convert to the JSON-RPC ``params`` dict (omitempty-style).

        Fields whose value is ``None`` are omitted entirely, matching how the
        Go core treats absent fields (keep existing defaults). Nested config
        blocks are recursively cleaned the same way.
        """

        return _to_dict(self)


__all__ = [
    "AgentConfig",
    "DelegateConfig",
    "CoordinatorConfig",
    "CompactionConfig",
    "PermissionConfig",
    "GuardsConfig",
    "VerifyConfig",
    "ToolScopeConfig",
    "ReminderConfig",
    "StreamingConfig",
]
# 'fields' / 'field' re-exported for downstream tests if useful.
_ = (fields, field)
