"""ai-agent Python SDK.

高レベル API（推奨）::

    from ai_agent import Agent, AgentConfig, Tool
    from ai_agent import (
        DelegateConfig, CoordinatorConfig, CompactionConfig,
        GuardsConfig, PermissionConfig, VerifyConfig,
        StreamingConfig, ReminderConfig, ToolScopeConfig,
        LoopConfig, RouterConfig, JudgeConfig,
    )
    from ai_agent import AgentError, GuardDenied, ToolError

    config = AgentConfig(
        binary="./agent",
        env={"SLLM_ENDPOINT": "http://localhost:8080/v1/chat/completions",
             "SLLM_API_KEY": "sk-xxx"},
        system_prompt="あなたは親切なアシスタントです。",
    )
    async with Agent(config) as agent:
        reply = await agent.input("こんにちは！")

低レベル API（上級者向け）::

    from ai_agent.client import Agent as RawAgent, AgentResult
    from ai_agent.config import AgentConfig as CoreAgentConfig
"""

# --- 高レベル API（推奨） ---
from ai_agent.easy import (
    Agent,
    AgentConfig,
    GoalJudgeCallable,
    StatusCallback,
    StreamCallback,
    Tool,
)

# --- 設定サブクラス（高・低レベル共用） ---
from ai_agent.config import (
    CompactionConfig,
    CoordinatorConfig,
    DelegateConfig,
    GuardsConfig,
    JudgeConfig,
    LoopConfig,
    PermissionConfig,
    ReminderConfig,
    RouterConfig,
    StreamingConfig,
    ToolScopeConfig,
    VerifyConfig,
)

# --- エラー ---
from ai_agent.errors import (
    AgentAborted,
    AgentBusy,
    AgentError,
    GuardDenied,
    ToolError,
)

# --- デコレータ / ユーティリティ ---
from ai_agent.guard import input_guard, output_guard, tool_call_guard
from ai_agent.tool import tool
from ai_agent.verifier import verifier

# --- 低レベル API（後方互換・高度な用途向け） ---
from ai_agent.client import Agent as RawAgent, AgentResult
from ai_agent.config import AgentConfig as CoreAgentConfig

__all__ = [
    # 高レベル（推奨）
    "Agent",
    "AgentConfig",
    "GoalJudgeCallable",
    "StatusCallback",
    "StreamCallback",
    "Tool",
    # 設定サブクラス
    "CompactionConfig",
    "CoordinatorConfig",
    "DelegateConfig",
    "GuardsConfig",
    "JudgeConfig",
    "LoopConfig",
    "PermissionConfig",
    "ReminderConfig",
    "RouterConfig",
    "StreamingConfig",
    "ToolScopeConfig",
    "VerifyConfig",
    # エラー
    "AgentError",
    "AgentBusy",
    "AgentAborted",
    "ToolError",
    "GuardDenied",
    # デコレータ / ユーティリティ
    "tool",
    "input_guard",
    "tool_call_guard",
    "output_guard",
    "verifier",
    # 低レベル（後方互換）
    "RawAgent",
    "AgentResult",
    "CoreAgentConfig",
]

__version__ = "0.1.0"
