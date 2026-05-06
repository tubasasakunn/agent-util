"""高レベル Agent API — ai_agent.easy

シンプルな使い心地を目指したラッパー。
低レベルの JSON-RPC/async 詳細を隠し、同期的に近い感覚で使える。

    from ai_agent.easy import Agent, AgentConfig, Tool

    config = AgentConfig(
        binary="./agent",
        env={"SLLM_ENDPOINT": "http://localhost:8080/v1",
             "SLLM_API_KEY": "sk-gemma4",
             "SLLM_MODEL": "gemma4"},
        system_prompt="あなたは親切なアシスタントです。",
        max_turns=20,
    )

    async def main():
        async with Agent(config) as agent:
            resp = await agent.input("こんにちは！")
            print(resp)

            # フォーク（子エージェント）
            child = await agent.fork()
            # 要約
            summary = await agent.context()
            # エクスポート / インポート
            data = await agent.export()
            # 会話検索
            hits = agent.search("挨拶")
"""

from __future__ import annotations

import asyncio
import json
import logging
import math
import re
import time
import uuid
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, AsyncIterator, Callable

from ai_agent.client import (
    Agent as _CoreAgent,
    AgentResult,
    AgentResult as _AgentResult,
    GoalJudgeCallable,
    StatusCallback,
    StreamCallback,
)
from ai_agent.config import (
    AgentConfig as _CoreConfig,
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
from ai_agent.guard import GuardCallable, GuardDefinition, get_guard_definition
from ai_agent.tool import ToolDefinition, _build_parameters
from ai_agent.verifier import VerifierCallable, VerifierDefinition, get_verifier_definition

logger = logging.getLogger("ai_agent.easy")

# ------------------------------------------------------------------ #
# JSON-RPC メソッド定数
# ------------------------------------------------------------------ #
_M_SESSION_HISTORY = "session.history"
_M_SESSION_INJECT = "session.inject"


# ------------------------------------------------------------------ #
# AgentConfig — バイナリ設定 + エージェント挙動を1クラスに集約
# ------------------------------------------------------------------ #

@dataclass
class AgentConfig:
    """高レベル Agent の統合設定。

    バイナリ起動パラメータとエージェント挙動設定を 1 クラスに集約する。
    これだけ渡せばエージェントが動く、ワンストップ設定クラス。

    必須:
        binary: コンパイル済みエージェントバイナリのパス（例: "./agent"）
        env:    LLM接続に必要な環境変数
                  {"SLLM_ENDPOINT": "http://localhost:8080/v1/chat/completions",
                   "SLLM_API_KEY": "sk-xxx",
                   "SLLM_MODEL": "gemma3:4b"}

    クイックスタート::

        config = AgentConfig(
            binary="./agent",
            env={"SLLM_ENDPOINT": "http://localhost:8080/v1/chat/completions",
                 "SLLM_API_KEY": "sk-xxx"},
            system_prompt="あなたは親切なアシスタントです。",
            max_turns=20,
        )
        async with Agent(config) as agent:
            reply = await agent.input("こんにちは！")
    """

    # --- バイナリ / プロセス設定 ---
    binary: str = "agent"
    """コンパイル済みエージェントバイナリのパス。"""
    env: dict[str, str] | None = None
    """子プロセスへ追加する環境変数。
    SLLM_ENDPOINT（必須）/ SLLM_API_KEY / SLLM_MODEL を含める。"""
    cwd: str | None = None
    """子プロセスの作業ディレクトリ。省略時は呼び出し元のカレントディレクトリ。"""

    # --- 基本エージェント挙動 ---
    system_prompt: str | None = None
    """エージェントへのシステムプロンプト。"""
    max_turns: int | None = None
    """最大ターン数。省略時はコアデフォルト（20）。"""
    token_limit: int | None = None
    """コンテキストウィンドウのトークン上限。省略時はモデルのデフォルト。"""
    work_dir: str | None = None
    """エージェントの作業ディレクトリ（ファイル操作のベースパス）。"""

    # --- サブエージェント・ツール機能 ---
    delegate: DelegateConfig | None = None
    """サブエージェント委任の設定。DelegateConfig(enabled=True) で有効化。"""
    coordinator: CoordinatorConfig | None = None
    """並列サブエージェントの設定。CoordinatorConfig(enabled=True) で有効化。"""

    # --- コンテキスト・安全性 ---
    compaction: CompactionConfig | None = None
    """コンテキスト縮約の設定。"""
    permission: PermissionConfig | None = None
    """ツール実行パーミッションの設定。"""
    guards: GuardsConfig | None = None
    """入力/ツール呼び出し/出力ガードレールの設定。"""
    verify: VerifyConfig | None = None
    """ツール実行結果の検証ループ設定。"""
    tool_scope: ToolScopeConfig | None = None
    """ツールスコーピング（表示するツールの絞り込み）設定。"""
    reminder: ReminderConfig | None = None
    """長い会話でのシステムリマインダー設定。"""
    streaming: StreamingConfig | None = None
    """ストリーミング通知の設定。StreamingConfig(enabled=True) で有効化。"""

    # --- ループ・LLM拡張 ---
    loop: LoopConfig | None = None
    """実行ループパターンの設定。LoopConfig(type="react") または LoopConfig(type="reaf")。"""
    router: RouterConfig | None = None
    """ルーター専用LLMの設定。省略時はメインLLMをルーターにも使用。"""
    judge: JudgeConfig | None = None
    """ゴール達成判定器の設定。register_judge() で登録した名前を指定。"""

    def _to_core_config(self) -> _CoreConfig:
        return _CoreConfig(
            max_turns=self.max_turns,
            system_prompt=self.system_prompt,
            token_limit=self.token_limit,
            work_dir=self.work_dir,
            delegate=self.delegate,
            coordinator=self.coordinator,
            compaction=self.compaction,
            permission=self.permission,
            guards=self.guards,
            verify=self.verify,
            tool_scope=self.tool_scope,
            reminder=self.reminder,
            streaming=self.streaming,
            loop=self.loop,
            router=self.router,
            judge=self.judge,
        )


# ------------------------------------------------------------------ #
# Tool — 関数をツール定義に変換するクラス
# ------------------------------------------------------------------ #

class Tool:
    """関数を Agent に登録可能なツール定義へ変換する。

    デコレータ (@tool) を使わずにクラスとして作成できる。

        def read_file(path: str) -> str:
            return open(path).read()

        tool_a = Tool(read_file, description="ファイルを読み込む")
        await agent.register_tools(tool_a)
    """

    def __init__(
        self,
        fn: Callable[..., Any],
        *,
        name: str | None = None,
        description: str = "",
        read_only: bool = False,
        parameters: dict[str, Any] | None = None,
    ) -> None:
        params = parameters if parameters is not None else _build_parameters(fn)
        desc = description or (fn.__doc__ or "").strip().splitlines()[0] if fn.__doc__ else description
        self._defn = ToolDefinition(
            name=name or fn.__name__,
            description=desc,
            parameters=params,
            read_only=read_only,
            func=fn,
            is_coroutine=asyncio.iscoroutinefunction(fn),
        )
        logger.debug("[Tool] 定義: name=%s read_only=%s", self._defn.name, self._defn.read_only)

    @property
    def definition(self) -> ToolDefinition:
        return self._defn

    @property
    def name(self) -> str:
        return self._defn.name


# ------------------------------------------------------------------ #
# SkillLoader — ディレクトリからスキル定義を読み込む
# ------------------------------------------------------------------ #

def _load_skill_configs(skill_dir: str) -> list[dict[str, Any]]:
    """スキルディレクトリから MCP/スキル設定を読み込む。

    各サブディレクトリに skill.json または mcp.json があれば読み込む。
    """
    configs: list[dict[str, Any]] = []
    root = Path(skill_dir)
    if not root.is_dir():
        logger.warning("[Skill] ディレクトリが見つかりません: %s", skill_dir)
        return configs

    for item in sorted(root.iterdir()):
        for fname in ("skill.json", "mcp.json", "config.json"):
            cfg_path = item / fname
            if cfg_path.exists():
                try:
                    with open(cfg_path) as f:
                        cfg = json.load(f)
                    cfg["_source"] = str(cfg_path)
                    configs.append(cfg)
                    logger.debug("[Skill] 読み込み: %s", cfg_path)
                except Exception as exc:
                    logger.warning("[Skill] 読み込みエラー %s: %s", cfg_path, exc)
                break
    return configs


# ------------------------------------------------------------------ #
# インメモリ RAG — TF-IDF ベースのキーワード検索
# ------------------------------------------------------------------ #

class _MessageIndex:
    """会話メッセージを TF-IDF でインデックス化して検索する。

    外部依存なし。埋め込み API が使えない環境でも動作する。
    """

    def __init__(self) -> None:
        self._docs: list[dict[str, Any]] = []
        self._tf: list[dict[str, float]] = []
        self._idf: dict[str, float] = {}
        self._dirty = False

    def add(self, role: str, content: str, meta: dict[str, Any] | None = None) -> None:
        doc = {"role": role, "content": content, "meta": meta or {}, "id": len(self._docs)}
        self._docs.append(doc)
        self._tf.append(self._compute_tf(content))
        self._dirty = True

    @staticmethod
    def _tokenize(text: str) -> list[str]:
        # フレーズ単位で分割
        phrases = re.findall(r"[^\s\.,;:!?、。！？\[\]()（）「」『』]+", text.lower())
        tokens: list[str] = []
        for phrase in phrases:
            tokens.append(phrase)
            # CJK文字（漢字・ひらがな・カタカナ）は単文字も個別トークンとして追加
            for ch in phrase:
                if (
                    "一" <= ch <= "鿿"  # 漢字
                    or "぀" <= ch <= "ゟ"  # ひらがな
                    or "゠" <= ch <= "ヿ"  # カタカナ
                ):
                    tokens.append(ch)
        return tokens

    @staticmethod
    def _compute_tf(text: str) -> dict[str, float]:
        tokens = _MessageIndex._tokenize(text)
        if not tokens:
            return {}
        counts: dict[str, int] = {}
        for t in tokens:
            counts[t] = counts.get(t, 0) + 1
        total = len(tokens)
        return {t: c / total for t, c in counts.items()}

    def _rebuild_idf(self) -> None:
        if not self._dirty:
            return
        n = len(self._docs)
        df: dict[str, int] = {}
        for tf in self._tf:
            for t in tf:
                df[t] = df.get(t, 0) + 1
        self._idf = {t: math.log((n + 1) / (d + 1)) + 1 for t, d in df.items()}
        self._dirty = False

    def search(self, query: str, top_k: int = 5) -> list[dict[str, Any]]:
        """クエリに最も関連するメッセージを返す。"""
        self._rebuild_idf()
        q_tf = self._compute_tf(query)
        if not q_tf:
            return []

        scores: list[tuple[float, int]] = []
        for i, tf in enumerate(self._tf):
            score = sum(
                q_tf.get(t, 0) * tf.get(t, 0) * self._idf.get(t, 1)
                for t in set(q_tf) | set(tf)
            )
            if score > 0:
                scores.append((score, i))

        scores.sort(reverse=True)
        return [
            {**self._docs[i], "score": s}
            for s, i in scores[:top_k]
        ]

    def all_messages(self) -> list[dict[str, Any]]:
        return list(self._docs)


# ------------------------------------------------------------------ #
# Agent — 高レベルエージェント
# ------------------------------------------------------------------ #

class Agent:
    """高レベルエージェント。

    **会話入力:**
    - ``input(prompt)``         : 入力送信・会話蓄積。``on_delta`` でストリーミングも可能
    - ``stream(prompt)``        : async generator でトークンを逐次 yield

    **コンテキスト操作:**
    - ``context()``             : LLM による会話要約
    - ``fork()``                : 子エージェント（会話履歴をコピー）
    - ``add(other)``            : 他エージェントの履歴を末尾に追加
    - ``add_summary(other)``    : 他エージェントの要約を注入
    - ``branch(n)``             : n 番目以降のメッセージで新エージェントを作成

    **シリアライズ:**
    - ``export()``              : 会話状態を dict としてシリアライズ
    - ``import_history(data)``  : export() データから会話状態を復元

    **登録:**
    - ``register_tools()``      : Tool インスタンスの登録
    - ``register_guards()``     : @input_guard / @output_guard / @tool_call_guard の登録
    - ``register_verifiers()``  : @verifier の登録
    - ``register_judge()``      : ゴール達成判定器の登録
    - ``register_skills()``     : スキルディレクトリ登録
    - ``register_mcp()``        : MCP 設定ファイルや dict の登録

    **ユーティリティ:**
    - ``search(query)``         : 過去の会話をキーワード検索（RAG）
    - ``batch(prompts)``        : 複数プロンプトを並列処理
    - ``improve_tool()``        : LLM でツールの説明を改善（動的スキル修正）
    """

    def __init__(
        self,
        config: AgentConfig,
        *,
        skill_dir: str | None = None,
        mcp: str | dict[str, Any] | list[dict[str, Any]] | None = None,
        name: str | None = None,
    ) -> None:
        self._config = config
        self._init_skill_dir = skill_dir
        self._init_mcp = mcp
        self._name = name or f"agent-{uuid.uuid4().hex[:8]}"
        self._core: _CoreAgent | None = None
        self._index = _MessageIndex()
        self._registered_tools: dict[str, Tool] = {}
        self._tool_stats: dict[str, dict[str, int]] = {}
        self._started = False
        logger.info("[Agent:%s] 初期化", self._name)

    # ---------------------------------------------------------------- #
    # ライフサイクル
    # ---------------------------------------------------------------- #

    async def _ensure_started(self) -> _CoreAgent:
        if self._core is not None:
            return self._core

        logger.info("[Agent:%s] 起動中 binary=%s", self._name, self._config.binary)
        core = _CoreAgent(
            binary_path=self._config.binary,
            env=self._config.env,
            cwd=self._config.cwd,
        )
        await core.start()
        applied = await core.configure(self._config._to_core_config())
        logger.info("[Agent:%s] configure 適用済み: %s", self._name, applied)

        self._core = core

        if self._init_skill_dir:
            await self.register_skills(self._init_skill_dir)
        if self._init_mcp is not None:
            await self.register_mcp(self._init_mcp)

        return core

    async def start(self) -> "Agent":
        await self._ensure_started()
        return self

    async def close(self) -> None:
        if self._core:
            logger.info("[Agent:%s] 終了", self._name)
            await self._core.close()
            self._core = None

    async def __aenter__(self) -> "Agent":
        await self._ensure_started()
        return self

    async def __aexit__(self, *_: Any) -> None:
        await self.close()

    # ---------------------------------------------------------------- #
    # ツール / スキル / MCP 登録
    # ---------------------------------------------------------------- #

    async def register_tools(self, *tools: Tool) -> list[str]:
        """ツールを登録してコアに通知する。

        Returns:
            登録されたツール名のリスト。
        """
        core = await self._ensure_started()
        defns = [t.definition for t in tools]
        for t in tools:
            self._registered_tools[t.name] = t
            self._tool_stats[t.name] = {"success": 0, "error": 0}
        names = await core.register_tools(*defns)
        logger.info("[Agent:%s] ツール登録: %s", self._name, names)
        return names

    async def register_guards(
        self, *guards: GuardDefinition | GuardCallable
    ) -> list[str]:
        """ガードを登録する。

        ``@input_guard`` / ``@tool_call_guard`` / ``@output_guard`` でデコレートした
        関数または :class:`~ai_agent.guard.GuardDefinition` インスタンスを渡す。

        Returns:
            登録されたガード名のリスト。
        """
        core = await self._ensure_started()
        names = await core.register_guards(*guards)
        logger.info("[Agent:%s] ガード登録: %s", self._name, names)
        return names

    async def register_verifiers(
        self, *verifiers: VerifierDefinition | VerifierCallable
    ) -> list[str]:
        """ベリファイアを登録する。

        ``@verifier`` でデコレートした関数または
        :class:`~ai_agent.verifier.VerifierDefinition` インスタンスを渡す。

        Returns:
            登録されたベリファイア名のリスト。
        """
        core = await self._ensure_started()
        names = await core.register_verifiers(*verifiers)
        logger.info("[Agent:%s] ベリファイア登録: %s", self._name, names)
        return names

    async def register_judge(self, name: str, handler: GoalJudgeCallable) -> None:
        """ゴール達成判定器を登録する。

        ``handler(response: str, turn: int) -> (terminate: bool, reason: str)``
        の形の callable を渡す。登録後、AgentConfig(judge=JudgeConfig(name=name)) で有効化する。
        """
        core = await self._ensure_started()
        await core.register_judge(name, handler)
        logger.info("[Agent:%s] judge 登録: %s", self._name, name)

    async def register_skills(self, skill_dir: str) -> list[str]:
        """スキルディレクトリを読み込み MCP サーバーとして登録する。"""
        core = await self._ensure_started()
        configs = _load_skill_configs(skill_dir)
        registered: list[str] = []
        for cfg in configs:
            try:
                tools = await self._register_single_mcp(core, cfg)
                registered.extend(tools)
            except Exception as exc:
                logger.warning("[Agent:%s] スキル登録失敗 %s: %s", self._name, cfg.get("_source", "?"), exc)
        logger.info("[Agent:%s] スキル登録完了: %s", self._name, registered)
        return registered

    async def register_mcp(
        self,
        mcp_config: str | dict[str, Any] | list[dict[str, Any]],
    ) -> list[str]:
        """MCP 設定（ファイルパス / dict / list）を登録する。"""
        core = await self._ensure_started()
        if isinstance(mcp_config, str):
            with open(mcp_config) as f:
                mcp_config = json.load(f)

        configs: list[dict[str, Any]] = mcp_config if isinstance(mcp_config, list) else [mcp_config]
        all_tools: list[str] = []
        for cfg in configs:
            tools = await self._register_single_mcp(core, cfg)
            all_tools.extend(tools)
        logger.info("[Agent:%s] MCP 登録完了: %s", self._name, all_tools)
        return all_tools

    async def _register_single_mcp(self, core: _CoreAgent, cfg: dict[str, Any]) -> list[str]:
        transport = cfg.get("transport", "stdio")
        kwargs: dict[str, Any] = {"transport": transport}
        if transport == "stdio":
            kwargs["command"] = cfg["command"]
            if "args" in cfg:
                kwargs["args"] = cfg["args"]
            if "env" in cfg:
                kwargs["env"] = cfg["env"]
        else:
            kwargs["url"] = cfg["url"]
        return await core.register_mcp(**kwargs)

    # ---------------------------------------------------------------- #
    # 会話入力
    # ---------------------------------------------------------------- #

    async def input(
        self,
        prompt: str,
        *,
        max_turns: int | None = None,
        on_delta: StreamCallback | None = None,
        on_status: StatusCallback | None = None,
        timeout: float | None = None,
    ) -> str:
        """エージェントに入力を送信し、レスポンス文字列を返す。

        スキル / MCP の内部呼び出しはGoコア側で処理され、
        メインコンテキストには最終応答のみが積まれる。

        Args:
            prompt: ユーザープロンプト。
            max_turns: このリクエストのみのターン数上限。省略時は AgentConfig.max_turns を使用。
            on_delta: ストリーミングチャンクを受け取るコールバック ``(text, turn)``。
                      ストリーミングを使う場合は AgentConfig(streaming=StreamingConfig(enabled=True)) も必要。
            on_status: コンテキスト使用状況のコールバック ``(usage_ratio, count, limit)``。
            timeout: タイムアウト秒数。
        """
        t0 = time.perf_counter()
        core = await self._ensure_started()
        logger.info("[Agent:%s] input: %s…", self._name, prompt[:60].replace("\n", " "))

        result = await core.run(
            prompt,
            max_turns=max_turns,
            stream=on_delta,
            on_status=on_status,
            timeout=timeout,
        )
        elapsed = time.perf_counter() - t0
        logger.info(
            "[Agent:%s] response: %s… (turns=%d reason=%s elapsed=%.2fs)",
            self._name,
            result.response[:60].replace("\n", " "),
            result.turns,
            result.reason,
            elapsed,
        )

        # SDK 側でもインデックスを更新（RAG 用）
        self._index.add("user", prompt)
        self._index.add("assistant", result.response)
        return result.response

    async def input_verbose(
        self,
        prompt: str,
        *,
        max_turns: int | None = None,
        on_delta: StreamCallback | None = None,
        on_status: StatusCallback | None = None,
        timeout: float | None = None,
    ) -> _AgentResult:
        """``input()`` と同じだが、ターン数・トークン使用量・終了理由も返す。

        Returns:
            AgentResult: .response (str), .turns (int), .reason (str), .usage

        Example::

            result = await agent.input_verbose("東京の人口は？")
            print(result.response)
            print(f"turns={result.turns} tokens={result.usage.total_tokens}")
        """
        t0 = time.perf_counter()
        core = await self._ensure_started()

        result = await core.run(
            prompt,
            max_turns=max_turns,
            stream=on_delta,
            on_status=on_status,
            timeout=timeout,
        )
        elapsed = time.perf_counter() - t0
        logger.info(
            "[Agent:%s] input_verbose: %s… (turns=%d reason=%s elapsed=%.2fs)",
            self._name,
            result.response[:60].replace("\n", " "),
            result.turns,
            result.reason,
            elapsed,
        )
        self._index.add("user", prompt)
        self._index.add("assistant", result.response)
        return result

    # ---------------------------------------------------------------- #
    # コンテキスト要約
    # ---------------------------------------------------------------- #

    async def context(self) -> str:
        """現在の会話履歴を LLM で要約して返す。"""
        core = await self._ensure_started()
        logger.info("[Agent:%s] context.summarize 呼び出し", self._name)
        summary = await core.summarize()
        logger.info("[Agent:%s] summary: %s…", self._name, summary[:80])
        return summary

    # ---------------------------------------------------------------- #
    # フォーク / 履歴転送
    # ---------------------------------------------------------------- #

    async def fork(self, *, name: str | None = None) -> "Agent":
        """現在の会話履歴を引き継いだ子エージェントを生成する。"""
        core = await self._ensure_started()
        logger.info("[Agent:%s] fork 開始", self._name)

        history = await self._export_history_raw(core)
        child = Agent(self._config, name=name or f"{self._name}-fork-{uuid.uuid4().hex[:4]}")
        child_core = await child._ensure_started()

        if history:
            await child_core._rpc.call(_M_SESSION_INJECT, {
                "messages": history,
                "position": "replace",
            })
            logger.info("[Agent:%s] fork 完了: %d メッセージ転送", self._name, len(history))

        # RAG インデックスもコピー
        child._index = self._index
        return child

    async def add(self, other: "Agent") -> None:
        """他エージェントの会話履歴をこのエージェントの末尾に追加する。"""
        other_core = await other._ensure_started()
        my_core = await self._ensure_started()

        history = await self._export_history_raw(other_core)
        if not history:
            return
        await my_core._rpc.call(_M_SESSION_INJECT, {
            "messages": history,
            "position": "append",
        })
        logger.info("[Agent:%s] add: %d メッセージ追加", self._name, len(history))

    async def add_summary(self, other: "Agent") -> None:
        """他エージェントの会話要約をこのエージェントのコンテキストに追加する。"""
        summary = await other.context()
        if not summary:
            return
        my_core = await self._ensure_started()
        await my_core._rpc.call(_M_SESSION_INJECT, {
            "messages": [{"role": "user", "content": f"[前のエージェントの会話要約]\n{summary}"}],
            "position": "prepend",
        })
        logger.info("[Agent:%s] add_summary: 要約 %d 文字 追加", self._name, len(summary))

    # ---------------------------------------------------------------- #
    # エクスポート / インポート / branch / checkpoint
    # ---------------------------------------------------------------- #

    async def export(self) -> dict[str, Any]:
        """会話状態を JSON シリアライズ可能な dict として返す。"""
        core = await self._ensure_started()
        history = await self._export_history_raw(core)
        data = {
            "version": 1,
            "agent_name": self._name,
            "timestamp": time.time(),
            "messages": history,
            "rag_index": [
                {"role": d["role"], "content": d["content"]}
                for d in self._index.all_messages()
            ],
        }
        logger.info("[Agent:%s] export: %d メッセージ", self._name, len(history))
        return data

    async def import_history(self, data: dict[str, Any]) -> None:
        """export() で取得したデータから会話状態を復元する。"""
        core = await self._ensure_started()
        messages = data.get("messages", [])
        await core._rpc.call(_M_SESSION_INJECT, {
            "messages": messages,
            "position": "replace",
        })
        # RAG インデックスも復元
        self._index = _MessageIndex()
        for m in data.get("rag_index", messages):
            self._index.add(m.get("role", "user"), m.get("content", ""))
        logger.info("[Agent:%s] import_history: %d メッセージ復元", self._name, len(messages))

    async def branch(self, from_index: int, *, name: str | None = None) -> "Agent":
        """from_index 番目以降のメッセージだけを引き継いだ新エージェントを作る。"""
        core = await self._ensure_started()
        history = await self._export_history_raw(core)
        subset = history[from_index:]

        child = Agent(self._config, name=name or f"{self._name}-branch-{uuid.uuid4().hex[:4]}")
        child_core = await child._ensure_started()

        if subset:
            await child_core._rpc.call(_M_SESSION_INJECT, {
                "messages": subset,
                "position": "replace",
            })
        logger.info("[Agent:%s] branch from=%d: %d メッセージ", self._name, from_index, len(subset))
        return child

    # ---------------------------------------------------------------- #
    # バッチ処理
    # ---------------------------------------------------------------- #

    async def batch(
        self,
        prompts: list[str],
        *,
        max_concurrency: int = 3,
    ) -> list[str]:
        """複数プロンプトを並列でフォーク処理して結果を返す。

        各プロンプトは独立した fork エージェントで処理されるため、
        メインの会話履歴には影響しない。
        """
        logger.info("[Agent:%s] batch: %d プロンプト (並列数=%d)", self._name, len(prompts), max_concurrency)
        sem = asyncio.Semaphore(max_concurrency)

        async def _run(prompt: str) -> str:
            async with sem:
                child = await self.fork()
                try:
                    return await child.input(prompt)
                finally:
                    await child.close()

        results = await asyncio.gather(*[_run(p) for p in prompts])
        return list(results)

    # ---------------------------------------------------------------- #
    # ストリーミング
    # ---------------------------------------------------------------- #

    async def stream(self, prompt: str) -> AsyncIterator[str]:
        """ストリーミングでトークンを1つずつ yield する。

        .. note::
            事前に ``AgentConfig(streaming=StreamingConfig(enabled=True))`` を
            設定する必要がある。未設定の場合、このメソッドは完了後に一括で
            レスポンスを yield する（ストリーミングなしの動作）。
        """
        core = await self._ensure_started()
        queue: asyncio.Queue[str | None] = asyncio.Queue()

        def _on_delta(text: str, turn: int) -> None:
            queue.put_nowait(text)

        async def _run() -> _AgentResult:
            try:
                return await core.run(prompt, stream=_on_delta)
            finally:
                queue.put_nowait(None)  # sentinel でループを終了させる

        run_task = asyncio.create_task(_run())
        try:
            while True:
                chunk = await queue.get()
                if chunk is None:
                    break
                yield chunk
        finally:
            result = await run_task
            self._index.add("user", prompt)
            self._index.add("assistant", result.response)

    # ---------------------------------------------------------------- #
    # RAG — 会話検索
    # ---------------------------------------------------------------- #

    def search(self, query: str, top_k: int = 5) -> list[dict[str, Any]]:
        """過去の会話をキーワード検索して関連メッセージを返す。

        Returns:
            [{"role": ..., "content": ..., "score": float, "id": int}, ...]
        """
        results = self._index.search(query, top_k=top_k)
        logger.info("[Agent:%s] search '%s': %d 件ヒット", self._name, query, len(results))
        return results

    # ---------------------------------------------------------------- #
    # 動的スキル修正 (Dynamic Skill Auto-modification)
    # ---------------------------------------------------------------- #

    async def improve_tool(self, tool_name: str, feedback: str) -> Tool | None:
        """フィードバックを元に LLM でツールの説明を改善し、再登録する。

        改善されたツール定義を返す（失敗時は None）。
        この機能を使うには対象ツールが事前に register_tools() で登録済みであること。

        改善要求は汚染のない独立したエージェントで行うため、
        メイン会話コンテキストに影響しない。
        """
        if tool_name not in self._registered_tools:
            logger.warning("[Agent:%s] improve_tool: ツール '%s' は未登録", self._name, tool_name)
            return None

        original = self._registered_tools[tool_name]
        stats = self._tool_stats.get(tool_name, {})

        improve_prompt = (
            f"あなたはツール定義の改善専門家です。以下のツールの説明文を改善してください。\n\n"
            f"ツール名: {tool_name}\n"
            f"現在の説明: {original.definition.description}\n"
            f"フィードバック: {feedback}\n"
            f"成功回数: {stats.get('success', 0)} / エラー回数: {stats.get('error', 0)}\n\n"
            '改善した説明文だけを以下のJSON形式で返してください（他のテキスト不要）:\n'
            '{"description": "改善された説明文"}'
        )

        logger.info("[Agent:%s] improve_tool '%s' 開始（独立エージェントで実行）", self._name, tool_name)

        # 汚染のない独立エージェントで改善要求を実行
        fresh_config = AgentConfig(
            binary=self._config.binary,
            env=self._config.env,
            cwd=self._config.cwd,
            system_prompt="ツール定義の改善専門家として、JSON形式のみで回答してください。",
            max_turns=2,
        )
        resp = ""
        async with Agent(fresh_config, name=f"{self._name}-improve") as helper:
            resp = await helper.input(improve_prompt)

        try:
            # JSON 部分を抽出（LLM がマークダウンで囲む場合に対応）
            match = re.search(r'\{[^}]*"description"\s*:\s*"([^"]+)"[^}]*\}', resp, re.DOTALL)
            if not match:
                logger.warning("[Agent:%s] improve_tool: JSON抽出失敗 resp=%s", self._name, resp[:120])
                return None
            new_desc = match.group(1)
        except Exception as exc:
            logger.warning("[Agent:%s] improve_tool パース失敗: %s", self._name, exc)
            return None

        improved = Tool(
            original.definition.func,
            name=tool_name,
            description=new_desc,
            read_only=original.definition.read_only,
        )
        # このエージェントに既に同名ツールが登録済みの場合は SDK 側の記録だけ更新する
        # （Go エンジンは duplicate name を拒否するため）
        # fork() や新規エージェントで improved を渡すことで改善説明が反映される
        if tool_name in self._registered_tools:
            self._registered_tools[tool_name] = improved
            logger.info(
                "[Agent:%s] improve_tool '%s' 完了（SDK記録更新のみ、新規エージェントで有効）: '%s'",
                self._name, tool_name, new_desc,
            )
        else:
            await self.register_tools(improved)
            logger.info("[Agent:%s] improve_tool '%s' 完了（新規登録）: '%s'", self._name, tool_name, new_desc)
        return improved

    # ---------------------------------------------------------------- #
    # 内部ヘルパー
    # ---------------------------------------------------------------- #

    async def _export_history_raw(self, core: _CoreAgent) -> list[dict[str, Any]]:
        raw = await core._rpc.call(_M_SESSION_HISTORY, {})
        return raw.get("messages", [])

    @property
    def name(self) -> str:
        return self._name


__all__ = [
    "Agent",
    "AgentConfig",
    "AgentResult",
    "GoalJudgeCallable",
    "StatusCallback",
    "StreamCallback",
    "Tool",
]
