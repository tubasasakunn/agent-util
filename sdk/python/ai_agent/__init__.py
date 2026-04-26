"""ai-agent Python SDK.

Thin JSON-RPC client over stdio for the Go-based ai-agent harness.

Public API:

    from ai_agent import Agent, AgentResult, AgentConfig, tool
    from ai_agent import (
        GuardsConfig, PermissionConfig, VerifyConfig,
        CompactionConfig, StreamingConfig, ReminderConfig,
        DelegateConfig, CoordinatorConfig, ToolScopeConfig,
    )
    from ai_agent import (
        input_guard, tool_call_guard, output_guard, verifier,
    )
    from ai_agent import AgentError, AgentBusy, ToolError, GuardDenied
"""

from ai_agent.client import Agent, AgentResult
from ai_agent.config import (
    AgentConfig,
    CompactionConfig,
    CoordinatorConfig,
    DelegateConfig,
    GuardsConfig,
    PermissionConfig,
    ReminderConfig,
    StreamingConfig,
    ToolScopeConfig,
    VerifyConfig,
)
from ai_agent.errors import (
    AgentAborted,
    AgentBusy,
    AgentError,
    GuardDenied,
    ToolError,
)
from ai_agent.guard import input_guard, output_guard, tool_call_guard
from ai_agent.tool import tool
from ai_agent.verifier import verifier

__all__ = [
    "Agent",
    "AgentResult",
    "AgentConfig",
    "CompactionConfig",
    "CoordinatorConfig",
    "DelegateConfig",
    "GuardsConfig",
    "PermissionConfig",
    "ReminderConfig",
    "StreamingConfig",
    "ToolScopeConfig",
    "VerifyConfig",
    "AgentError",
    "AgentBusy",
    "AgentAborted",
    "ToolError",
    "GuardDenied",
    "tool",
    "input_guard",
    "tool_call_guard",
    "output_guard",
    "verifier",
]

__version__ = "0.1.0"
