# ai-agent Python SDK

[![pypi-local](https://img.shields.io/badge/install-local-blue)](#インストール)
[![python](https://img.shields.io/badge/python-3.10+-3776AB)](#要件)
[![protocol](https://img.shields.io/badge/JSON--RPC-2.0-green)](../../docs/openrpc.json)

`ai-agent` の Go ハーネスを Python から薄くラップする SDK。
**Python/JS/Swift 全 SDK で共通の Agent Object Model (AOM) を完全実装**しており、
1 行 (`async with Agent(config) as a: await a.input("hi")`) から本格的な
ツール呼び出し・サブエージェント連携まで段階的に拡張できる。

- 外部ランタイム依存ゼロ (`asyncio` + `subprocess` + `json` のみ)
- `async`/`await` ファースト。`@tool` は sync/async どちらの関数でも動く
- `pkg/protocol/methods.go` / `docs/openrpc.json` と完全一致

## 目次

1. [TL;DR](#tldr)
2. [要件](#要件)
3. [インストール](#インストール)
4. [AOM (Agent Object Model)](#aom-agent-object-model)
5. [クイックスタート](#クイックスタート)
6. [API リファレンス](#api-リファレンス)
7. [設定リファレンス](#設定リファレンス)
8. [ツール / ガード / ベリファイア / ジャッジ](#ツール--ガード--ベリファイア--ジャッジ)
9. [MCP / スキル統合](#mcp--スキル統合)
10. [ストリーミング](#ストリーミング)
11. [LLM ハンドラ (任意 API 形式で叩く)](#llm-ハンドラ-任意-api-形式で叩く)
12. [フォーク / ブランチ / バッチ](#フォーク--ブランチ--バッチ)
13. [RAG 検索 / 会話要約](#rag-検索--会話要約)
14. [エラーハンドリング](#エラーハンドリング)
15. [テスト](#テスト)
16. [トラブルシューティング](#トラブルシューティング)

## TL;DR

```python
import asyncio
from ai_agent import Agent, AgentConfig

async def main():
    config = AgentConfig(
        binary="./agent",
        env={
            "SLLM_ENDPOINT": "http://localhost:8080/v1/chat/completions",
            "SLLM_API_KEY": "sk-xxx",
        },
        system_prompt="あなたは親切なアシスタントです。",
    )
    async with Agent(config) as agent:
        print(await agent.input("こんにちは！"))

asyncio.run(main())
```

## 要件

- Python 3.10+
- ビルド済みの `agent` バイナリ (リポジトリルートで `go build -o agent ./cmd/agent/`)
- SLLM サーバ (OpenAI 互換)

## インストール

```bash
# 1. Go バイナリをビルド
go build -o agent ./cmd/agent/

# 2. SDK をインストール (editable)
cd sdk/python
pip install -e .

# テスト用 extras を入れる場合
pip install -e ".[test]"
```

## AOM (Agent Object Model)

> 「エージェントを、会話状態を持つファーストクラスオブジェクトとして扱う」

| 層           | クラス                          | 用途                                            |
| ------------ | ------------------------------- | ----------------------------------------------- |
| 高レベル AOM | `Agent`     (`ai_agent.easy`)   | エージェントをオブジェクトとして扱う。これが推奨。 |
| 低レベル     | `RawAgent`  (`ai_agent.client`) | JSON-RPC をプロトコル通りに叩く。細かい制御用。   |

```python
from ai_agent import Agent, AgentConfig                # ◀ 高レベル (推奨)
from ai_agent import RawAgent, CoreAgentConfig         # ◀ 低レベル
```

## クイックスタート

### 1. 最小実行

```python
async with Agent(AgentConfig(binary="./agent", env=env)) as agent:
    print(await agent.input("こんにちは"))
```

### 2. ツールを登録

```python
from pathlib import Path
from ai_agent import Agent, AgentConfig, tool

@tool(description="ファイルを読み込む", read_only=True)
def read_file(path: str) -> str:
    return Path(path).read_text()

async with Agent(AgentConfig(binary="./agent", env=env)) as agent:
    await agent.register_tools(read_file)
    print(await agent.input("README.md を要約して"))
```

シグネチャから JSON Schema が自動生成される (詳細は
[`ai_agent/tool.py`](./ai_agent/tool.py) の型マッピング表)。
`async def` のツールはネイティブ呼び出し。`def` のツールは default executor で
ディスパッチされ、JSON-RPC リーダループをブロックしない。

### 3. ガード + ストリーミング

```python
from ai_agent import (
    Agent, AgentConfig, GuardsConfig, PermissionConfig,
    StreamingConfig, input_guard,
)

@input_guard(name="no_secrets")
def reject_secrets(input: str) -> tuple[str, str]:
    if "password" in input.lower():
        return ("deny", "secret detected")
    return ("allow", "")

config = AgentConfig(
    binary="./agent", env=env,
    permission=PermissionConfig(enabled=True, allow=["read_file"]),
    guards=GuardsConfig(input=["no_secrets"]),
    streaming=StreamingConfig(enabled=True),
    max_turns=8,
)

async with Agent(config) as agent:
    await agent.register_guards(reject_secrets)
    await agent.input(
        "README を案内して",
        on_delta=lambda text, turn: print(text, end="", flush=True),
    )
```

### 4. fork で会話を派生させる

```python
async with Agent(config) as parent:
    await parent.input("私の名前は花子です。覚えてください。")

    # 子エージェントは親の履歴をコピー
    child = await parent.fork()
    try:
        print(await child.input("私の名前は何ですか？"))   # → "花子です"
    finally:
        await child.close()
```

## API リファレンス

### `Agent` (高レベル AOM)

```python
class Agent:
    def __init__(
        self,
        config: AgentConfig,
        *,
        skill_dir: str | None = None,
        mcp: str | dict | list[dict] | None = None,
        name: str | None = None,
    ) -> None: ...

    # ライフサイクル
    async def start(self) -> "Agent": ...
    async def close(self) -> None: ...
    async def __aenter__(self) -> "Agent": ...
    async def __aexit__(self, *exc) -> None: ...

    # 会話入力
    async def input(self, prompt: str, *,
                    max_turns: int | None = None,
                    on_delta: StreamCallback | None = None,
                    on_status: StatusCallback | None = None,
                    timeout: float | None = None) -> str: ...
    async def input_verbose(self, prompt, ...) -> AgentResult: ...
    async def stream(self, prompt: str) -> AsyncIterator[str]: ...

    # コンテキスト操作
    async def context(self) -> str: ...                 # LLM で履歴を要約
    async def fork(self, *, name=None) -> "Agent": ...  # 子エージェント (履歴コピー)
    async def add(self, other: "Agent") -> None: ...     # 他エージェントの履歴を末尾に追加
    async def add_summary(self, other: "Agent") -> None: ...  # 要約を注入
    async def branch(self, from_index: int, *, name=None) -> "Agent": ...

    # シリアライズ
    async def export(self) -> dict: ...
    async def import_history(self, data: dict) -> None: ...

    # 登録
    async def register_tools(self, *tools) -> list[str]: ...
    async def register_guards(self, *guards) -> list[str]: ...
    async def register_verifiers(self, *verifiers) -> list[str]: ...
    async def register_judge(self, name: str, handler: GoalJudgeCallable) -> None: ...
    async def register_mcp(self, mcp_config) -> list[str]: ...
    async def register_skills(self, skill_dir: str) -> list[str]: ...

    # ユーティリティ
    def search(self, query: str, top_k: int = 5) -> list[dict]: ...   # RAG (TF-IDF)
    async def batch(self, prompts: list[str], *,
                    max_concurrency: int = 3,
                    timeout: float | None = None) -> list[str]: ...
    async def improve_tool(self, tool_name: str, feedback: str) -> Tool | None: ...
```

### `RawAgent` (低レベル)

`Agent` クラスの内部で動いている薄いラッパー。プロトコルに忠実な API が欲しい
ときに使う:

```python
from ai_agent import RawAgent, CoreAgentConfig

async with RawAgent(binary_path="./agent", env=env) as raw:
    applied = await raw.configure(CoreAgentConfig(max_turns=5))
    result  = await raw.run("hello")
    print(result.response, result.turns, result.usage.total_tokens)
```

主要メソッド: `start` / `close` / `configure` / `run` / `abort` / `summarize` /
`register_tools` / `register_guards` / `register_verifiers` / `register_judge` /
`register_mcp` / `_rpc.call(method, params)` (生 RPC)。

### 戻り値型

```python
@dataclass
class AgentResult:
    response: str
    reason: str        # "completed" / "max_turns" / "aborted" / ...
    turns: int
    usage: UsageInfo

@dataclass
class UsageInfo:
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int
```

## 設定リファレンス

### `AgentConfig` (高レベル)

| フィールド          | 型                  | 説明                                                |
| ------------------- | ------------------- | --------------------------------------------------- |
| `binary`            | `str`               | `agent` バイナリのパス                              |
| `env`               | `dict[str, str]?`   | 子プロセスに渡す環境変数                            |
| `cwd`               | `str?`              | 子プロセスの作業ディレクトリ                        |
| `system_prompt`     | `str?`              | システムプロンプト                                  |
| `max_turns`         | `int?`              | 最大ターン数 (default 20)                          |
| `token_limit`       | `int?`              | コンテキストのトークン上限                          |
| `work_dir`          | `str?`              | エージェントの作業ディレクトリ                      |
| `delegate`          | `DelegateConfig?`   | サブエージェント委任                                |
| `coordinator`       | `CoordinatorConfig?`| 並列サブエージェント                                |
| `compaction`        | `CompactionConfig?` | コンテキスト圧縮                                    |
| `permission`        | `PermissionConfig?` | ツール実行パーミッション                            |
| `guards`            | `GuardsConfig?`     | 入力/ツール呼出/出力ガード                          |
| `verify`            | `VerifyConfig?`     | ベリファイアループ                                  |
| `tool_scope`        | `ToolScopeConfig?`  | ツールスコーピング                                  |
| `reminder`          | `ReminderConfig?`   | システムリマインダー                                |
| `streaming`         | `StreamingConfig?`  | ストリーミング通知                                  |
| `loop`              | `LoopConfig?`       | ループパターン (`react` / `reaf`)                   |
| `router`            | `RouterConfig?`     | ルーター専用 LLM                                    |
| `judge`             | `JudgeConfig?`      | ゴール達成判定器                                    |

### サブ設定

```python
from ai_agent import (
    DelegateConfig, CoordinatorConfig, CompactionConfig, PermissionConfig,
    GuardsConfig, VerifyConfig, ToolScopeConfig, ReminderConfig,
    StreamingConfig, LoopConfig, RouterConfig, JudgeConfig,
)
```

| サブ設定             | 主なフィールド                                                  |
| -------------------- | --------------------------------------------------------------- |
| `DelegateConfig`     | `enabled`, `max_chars`                                          |
| `CoordinatorConfig`  | `enabled`, `max_chars`                                          |
| `CompactionConfig`   | `enabled`, `budget_max_chars`, `keep_last`, `target_ratio`, `summarizer` (`""`/`"llm"`) |
| `PermissionConfig`   | `enabled`, `deny: list[str]`, `allow: list[str]`                |
| `GuardsConfig`       | `input: list[str]`, `tool_call: list[str]`, `output: list[str]` |
| `VerifyConfig`       | `verifiers: list[str]`, `max_step_retries`, `max_consecutive_failures` |
| `ToolScopeConfig`    | `max_tools`, `include_always: list[str]`                        |
| `ReminderConfig`     | `threshold`, `content`                                          |
| `StreamingConfig`    | `enabled`, `context_status`                                     |
| `LoopConfig`         | `type: "react" | "reaf"`                                       |
| `RouterConfig`       | `endpoint`, `model`, `api_key`                                  |
| `JudgeConfig`        | `name: str` (`register_judge` で登録した名前)                    |

`None` フィールドは JSON 送信時に省略される (Go の `omitempty` と同じ挙動)。

## ツール / ガード / ベリファイア / ジャッジ

### `@tool` デコレータ

```python
@tool(
    name="read_file",            # 省略時は関数名
    description="ファイルを読む",  # 省略時は docstring 1 行目
    read_only=True,              # core が auto-approve 判定に使う
    parameters=None,             # 省略時は型ヒントから自動生成
)
def read_file(path: str) -> str:
    return Path(path).read_text()
```

戻り値は `str` / `dict({"content": str, "is_error"?: bool, "metadata"?: dict})` /
その他 (str に変換) のいずれか。

### ガード (`@input_guard` / `@tool_call_guard` / `@output_guard`)

`(decision, reason)` の 2-tuple を返す関数を登録する。`decision` は
`"allow" | "deny" | "tripwire"` のいずれか。

```python
from ai_agent import input_guard, tool_call_guard, output_guard

@input_guard(name="no_secrets")
def check_input(input: str) -> tuple[str, str]: ...

@tool_call_guard(name="fs_root_only")
def check_tool(tool_name: str, args: dict) -> tuple[str, str]: ...

@output_guard(name="pii_redactor")
def check_output(output: str) -> tuple[str, str]: ...
```

登録後、`GuardsConfig(input=["no_secrets"], ...)` で有効化する。

### `@verifier`

ツール実行後の結果検証。`(passed, summary)` を返す。

```python
from ai_agent import verifier

@verifier(name="non_empty")
def check(tool_name: str, args: dict, result: str) -> tuple[bool, str]:
    if not result.strip():
        return (False, "result is empty")
    return (True, "ok")
```

### ジャッジ (ゴール達成判定)

応答 1 回ごとに「目的を達成したか」を判定する関数。`(terminate, reason)` を返す。

```python
from ai_agent import JudgeConfig

async def my_judge(response: str, turn: int) -> tuple[bool, str]:
    return ("FINAL ANSWER" in response, "marker detected")

async with Agent(AgentConfig(..., judge=JudgeConfig(name="goal"))) as agent:
    await agent.register_judge("goal", my_judge)
    await agent.input("...")
```

## MCP / スキル統合

```python
# 単体 stdio サーバ
await agent.register_mcp({
    "transport": "stdio",
    "command": "uvx",
    "args": ["mcp-server-fetch"],
    "env": {"FOO": "bar"},
})

# 設定ファイル
await agent.register_mcp("./mcp_config.json")

# 設定リスト
await agent.register_mcp([
    {"transport": "stdio", "command": "..."},
    {"transport": "sse", "url": "https://..."},
])

# スキルディレクトリ (各サブディレクトリの skill.json / mcp.json を読む)
await agent.register_skills("./skills/")
```

## ストリーミング

```python
# コールバック方式
await agent.input(
    "長めの説明をして",
    on_delta=lambda text, turn: print(text, end="", flush=True),
    on_status=lambda ratio, count, limit: print(f"\n[ctx {ratio:.0%}]"),
)

# async iterator 方式
async for chunk in agent.stream("長めの説明をして"):
    print(chunk, end="", flush=True)
```

事前に `StreamingConfig(enabled=True)` を `AgentConfig` に設定すること。
未設定の場合は完了後に 1 チャンクとして配信される。

## LLM ハンドラ (任意 API 形式で叩く)

Go ハーネスはデフォルトで `SLLM_ENDPOINT` の OpenAI 互換 HTTP API を叩く。
**OpenAI 非互換の API (Anthropic / Bedrock / Vertex AI / ollama / 独自プロキシ等)
を使いたい、あるいはテスト時に LLM をモックしたい場合**は、`llm_handler` を
指定して `agent.configure` の LLM mode を `remote` に切り替える (ADR-016)。

指定すると、コアの **すべての ChatCompletion 呼び出し** (ルーター / 応答生成 /
ジャッジ / コンテキスト要約) が `llm.execute` 逆 RPC として Python ハンドラに
転送される。

```python
from ai_agent import Agent, AgentConfig

def my_llm(request: dict) -> dict:
    """OpenAI 互換 ChatRequest dict を受け取り、ChatResponse dict を返す。"""
    # ここで Anthropic / Bedrock / ollama / mock 等に変換して叩く
    messages = request["messages"]
    model = request.get("model", "claude-haiku")
    # ... 実 API 呼び出し ...
    text = "..."  # 結果テキスト
    return {
        "choices": [{
            "index": 0,
            "message": {"role": "assistant", "content": text},
            "finish_reason": "stop",
        }],
        "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
    }

config = AgentConfig(
    binary="./agent",
    llm_handler=my_llm,   # 自動で llm.mode="remote" が適用される
    # SLLM_ENDPOINT は不要 (使われない)
)
async with Agent(config) as agent:
    print(await agent.input("hi"))
```

明示制御したい場合は `llm=LLMConfig(mode="remote", timeout_seconds=120)` を併用:

```python
from ai_agent import Agent, AgentConfig, LLMConfig

config = AgentConfig(
    binary="./agent",
    llm=LLMConfig(mode="remote", timeout_seconds=180),
    llm_handler=my_llm,
)
```

低レベル `RawAgent` から使う場合は `set_llm_handler` を `configure` 前に呼ぶ:

```python
from ai_agent.client import Agent as RawAgent
from ai_agent.config import AgentConfig as CoreConfig, LLMConfig

raw = RawAgent(binary_path="./agent")
await raw.start()
raw.set_llm_handler(my_llm)
await raw.configure(CoreConfig(llm=LLMConfig(mode="remote")))
```

### 注意点

- ルーターは JSON mode で `{"tool":..., "arguments":..., "reasoning":...}` を
  期待する。ハンドラ側で `request.get("response_format", {}).get("type") == "json_object"`
  を見て **必ず有効な JSON を返す** こと
- 例外を投げると JSON-RPC エラーとして ChatCompletion が失敗する
- ストリーミング (`StreamingConfig(enabled=True)`) は `llm.mode="remote"` と
  併用しても delta 通知は飛ばない (ハンドラ呼び出し完了後に 1 回まとめて配信)
- 動作確認の最小例: `sdk/python/examples/e2e_llm_remote.py`

## フォーク / ブランチ / バッチ

```python
# fork: 現在の履歴をコピーした子エージェントを作る
child = await parent.fork()

# branch: from_index 以降のメッセージだけ引き継いだ新エージェント
alt = await parent.branch(from_index=4)

# add: 他エージェントの履歴を末尾に追加
await main.add(researcher)

# add_summary: 他エージェントの会話要約をシステムメッセージとして注入
await main.add_summary(researcher)

# batch: 複数プロンプトを並列でフォーク処理 (各々独立した子で実行)
results = await agent.batch(
    ["要約して", "翻訳して", "目次を作って"],
    max_concurrency=3,
)
```

## RAG 検索 / 会話要約

```python
# TF-IDF ベースのキーワード検索 (外部依存なし)
hits = agent.search("Tokyo")
# → [{"role": "user", "content": "...", "score": 0.8, "id": 3}, ...]

# LLM による会話要約
summary = await agent.context()
print(summary)
```

## エラーハンドリング

```python
from ai_agent import (
    AgentError, AgentBusy, AgentAborted, ToolError,
    GuardDenied, TripwireTriggered,
)

try:
    await agent.input("...")
except TripwireTriggered as e:
    alert_security_team(e.reason)
except GuardDenied as e:
    print(f"拒否されました: {e.reason}")
except AgentBusy:
    print("既に別の run が実行中")
except AgentAborted:
    print("abort() で中断された")
except ToolError as e:
    print(f"ツール実行失敗: {e}")
except AgentError as e:
    print(f"その他の SDK エラー: {e}")
```

| 例外クラス          | JSON-RPC code      | 発生条件                                       |
| ------------------- | ------------------ | ---------------------------------------------- |
| `AgentBusy`         | `-32002`           | 既に別の `agent.run` が実行中                  |
| `AgentAborted`      | `-32003`           | `agent.abort` でキャンセル                      |
| `ToolError`         | `-32000` / `-32001`| ツールが見つからない / 実行失敗                |
| `GuardDenied`       | `-32005`           | ガードが `deny` / `tripwire` を返した          |
| `TripwireTriggered` | `-32006`           | tripwire ガードが発火 (`GuardDenied` のサブクラス) |
| `AgentError`        | その他              | SDK 基底例外                                    |

## テスト

```bash
cd sdk/python

# ユニットテスト (バイナリ不要)
python -m pytest

# E2E (実バイナリ + 実 LLM)
go build -o ../../agent ../../cmd/agent/
AGENT_BINARY=$(pwd)/../../agent python -m pytest
```

## トラブルシューティング

| 症状                                          | 対処                                                                  |
| --------------------------------------------- | --------------------------------------------------------------------- |
| `FileNotFoundError: Agent binary not found`   | `go build -o agent ./cmd/agent/` を実行し、`AgentConfig(binary=...)` のパスを確認 |
| `PermissionError: Agent binary is not executable` | `chmod +x agent`                                                  |
| `SLLM_ENDPOINT not set` 系のエラーが core から出る | `AgentConfig(env={"SLLM_ENDPOINT": "..."})` を設定                |
| `RPC timeout` が頻発                           | `agent.input(..., timeout=60)` で延長、または SLLM 側のレスポンス遅延を確認 |
| ストリーミングが届かない                         | `AgentConfig(streaming=StreamingConfig(enabled=True))` を設定          |
| ガード/ベリファイアが呼ばれない                  | `GuardsConfig(input=[...])` で有効化されているか確認                    |
| `agent.stderr_output` を見て core の stderr を確認 | デバッグ時に活用                                                     |

## 参考

- [`../../docs/openrpc.json`](../../docs/openrpc.json) — OpenRPC 1.2.6 完全仕様
- [`../../docs/schemas/`](../../docs/schemas/) — 各型の JSON Schema
- [`../../pkg/protocol/methods.go`](../../pkg/protocol/methods.go) — Go 側の真実の源
- [`../README.md`](../README.md) — SDK 全体のハブ
- ADR-001 (JSON-RPC over stdio), ADR-013 (RemoteTool アダプタ) ほか — リポジトリルートで `/decision list`
