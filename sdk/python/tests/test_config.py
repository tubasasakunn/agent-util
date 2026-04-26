"""Tests for the dataclass -> dict conversion (omitempty behaviour)."""

from __future__ import annotations

from ai_agent.config import (
    AgentConfig,
    CompactionConfig,
    GuardsConfig,
    PermissionConfig,
    StreamingConfig,
    VerifyConfig,
)


def test_empty_config_yields_empty_dict() -> None:
    cfg = AgentConfig()
    assert cfg.to_params() == {}


def test_top_level_scalars_pass_through() -> None:
    cfg = AgentConfig(max_turns=8, system_prompt="hello", token_limit=4096)
    out = cfg.to_params()
    assert out == {
        "max_turns": 8,
        "system_prompt": "hello",
        "token_limit": 4096,
    }


def test_none_fields_are_dropped() -> None:
    cfg = AgentConfig(max_turns=None, system_prompt="x")
    out = cfg.to_params()
    assert "max_turns" not in out
    assert out["system_prompt"] == "x"


def test_nested_streaming_config() -> None:
    cfg = AgentConfig(streaming=StreamingConfig(enabled=True, context_status=True))
    out = cfg.to_params()
    assert out == {"streaming": {"enabled": True, "context_status": True}}


def test_nested_partial_dropped() -> None:
    # context_status not set -> dropped, even though enabled=False is kept.
    cfg = AgentConfig(streaming=StreamingConfig(enabled=False))
    out = cfg.to_params()
    assert out == {"streaming": {"enabled": False}}


def test_guards_lists() -> None:
    cfg = AgentConfig(
        guards=GuardsConfig(input=["no_secrets"], output=["pii"])
    )
    out = cfg.to_params()
    assert out == {
        "guards": {"input": ["no_secrets"], "output": ["pii"]},
    }


def test_permission_lists() -> None:
    cfg = AgentConfig(
        permission=PermissionConfig(enabled=True, allow=["read_file"], deny=["*"])
    )
    out = cfg.to_params()
    assert out == {
        "permission": {
            "enabled": True,
            "allow": ["read_file"],
            "deny": ["*"],
        }
    }


def test_compaction_with_floats() -> None:
    cfg = AgentConfig(
        compaction=CompactionConfig(
            enabled=True, target_ratio=0.5, summarizer="llm"
        )
    )
    out = cfg.to_params()
    assert out == {
        "compaction": {
            "enabled": True,
            "target_ratio": 0.5,
            "summarizer": "llm",
        }
    }


def test_verify_block() -> None:
    cfg = AgentConfig(
        verify=VerifyConfig(verifiers=["v1"], max_step_retries=3)
    )
    out = cfg.to_params()
    assert out == {"verify": {"verifiers": ["v1"], "max_step_retries": 3}}


def test_combined_config_matches_openrpc_example() -> None:
    """The OpenRPC ``enable-streaming`` example must round-trip exactly."""

    cfg = AgentConfig(
        max_turns=10,
        streaming=StreamingConfig(enabled=True, context_status=True),
    )
    assert cfg.to_params() == {
        "max_turns": 10,
        "streaming": {"enabled": True, "context_status": True},
    }
