# ai-agent Swift SDK

[![spm](https://img.shields.io/badge/SwiftPM-5.9+-orange)](#インストール)
[![platform](https://img.shields.io/badge/macOS-13+-blue)](#要件)
[![protocol](https://img.shields.io/badge/JSON--RPC-2.0-green)](../../docs/openrpc.json)

`AIAgent` — `ai-agent` の Go ハーネスを Swift から呼び出す SwiftPM ライブラリ。
**Python/JS/Swift 全 SDK で共通の Agent Object Model (AOM) を完全実装**しており、
Python と同じ感覚 (`fork()` / `branch()` / `batch()` / `search()` / `context()`)
で書ける。

- Foundation のみ依存 (`Process` / `FileHandle` / `JSONSerialization`)
- Swift 構造化並行性 (`async`/`await`, `actor`, `AsyncStream`)
- `pkg/protocol/methods.go` と完全一致

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
14. [小型モデル向けチューニング指針](#小型モデル向けチューニング指針-h4)
15. [JSONValue リファレンス](#jsonvalue-リファレンス)
16. [エラーハンドリング](#エラーハンドリング)
17. [テスト](#テスト)
18. [実装メモ](#実装メモ)

## TL;DR

```swift
import AIAgent

let config = AgentConfig(
    binary: "./agent",
    env: [
        "SLLM_ENDPOINT": "http://localhost:8080/v1/chat/completions",
        "SLLM_API_KEY": "sk-xxx",
    ],
    systemPrompt: "あなたは親切なアシスタントです。",
    maxTurns: 20
)

let agent = Agent(config: config)
try await agent.start()
defer { Task { await agent.close() } }

print(try await agent.input("こんにちは！"))
```

## 要件

- **macOS 13+** (`Foundation.Process` を使うため。Linux は未対応)
- **Swift 5.9+** (構造化並行性 + `AsyncStream.makeStream`)
- ビルド済みの `agent` バイナリ
- SLLM サーバ (OpenAI 互換)

```bash
go build -o agent ./cmd/agent/
```

## インストール

`Package.swift` の `dependencies` に追加:

```swift
// 推奨: GitHub リポジトリから (v0.2.1+)
.package(url: "https://github.com/tubasasakunn/agent-util.git", from: "0.2.1"),

// または: ローカルパス
.package(path: "../path/to/agent-util"),
```

ターゲット依存:

```swift
.product(name: "AIAgent", package: "agent-util"),
```

import:

```swift
import AIAgent
```

> **リポジトリ構造の注意**: `Package.swift` はリポジトリのルートに置かれている
> (SwiftPM はリポジトリルートの Package.swift しか認識しないため)。
> ソース本体は `sdk/swift/Sources/AIAgent/`、テストは `sdk/swift/Tests/AIAgentTests/`。
> ローカルで `swift build` / `swift test` を実行する際は **リポジトリのルート**
> から実行する。

## AOM (Agent Object Model)

> 「エージェントを、会話状態を持つファーストクラスオブジェクトとして扱う」

| 層           | 型             | 用途                                             |
| ------------ | -------------- | ------------------------------------------------ |
| 高レベル AOM | `Agent`        | エージェントをオブジェクトとして扱う。推奨。      |
| 低レベル     | `RawAgent`     | JSON-RPC をプロトコル通りに叩く。細かい制御用。   |

両者とも actor として実装されているため、複数スレッドから安全に呼べる。

## クイックスタート

### 1. 最小実行

```swift
let agent = Agent(config: AgentConfig(binary: "./agent", env: env))
try await agent.start()
defer { Task { await agent.close() } }
print(try await agent.input("こんにちは"))
```

### 2. ツールを登録

```swift
let readFile = Tool(
    name: "read_file",
    description: "ファイルを読む",
    parameters: .object([
        "type": .string("object"),
        "properties": .object([
            "path": .object(["type": .string("string")]),
        ]),
        "required": .array([.string("path")]),
        "additionalProperties": .bool(false),
    ]),
    readOnly: true
) { args in
    let path = args["path"]?.stringValue ?? ""
    return .text(try String(contentsOfFile: path))
}

try await agent.registerTools(readFile)
print(try await agent.input("README.md を要約して"))
```

### 3. ガード + ストリーミング

```swift
let noSecrets = GuardSpec.input(name: "no_secrets") { input in
    input.lowercased().contains("password")
        ? (.deny, "secret detected")
        : (.allow, "")
}

let config = AgentConfig(
    binary: "./agent",
    env: env,
    permission: PermissionConfig(enabled: true, allow: ["read_file"]),
    guards: GuardsConfig(input: ["no_secrets"]),
    streaming: StreamingConfig(enabled: true),
    maxTurns: 8
)

let agent = Agent(config: config)
try await agent.start()
defer { Task { await agent.close() } }
try await agent.registerGuards(noSecrets)

for try await chunk in agent.stream("README を案内して") {
    print(chunk, terminator: "")
}
```

### 4. fork で会話を派生させる

```swift
let parent = Agent(config: config)
try await parent.start()
defer { Task { await parent.close() } }

_ = try await parent.input("私の名前は花子です。覚えてください。")

let child = try await parent.fork()
defer { Task { await child.close() } }
print(try await child.input("私の名前は何ですか？"))   // → "花子です"
```

## API リファレンス

### `Agent` (高レベル AOM)

```swift
public actor Agent {
    public init(config: AgentConfig, name: String? = nil)

    // ライフサイクル
    @discardableResult public func start() async throws -> Agent
    public func close() async
    public var stderrOutput: String { get async }

    // 会話入力
    @discardableResult
    public func input(_ prompt: String, maxTurns: Int? = nil,
                      onDelta: StreamCallback? = nil,
                      onStatus: StatusCallback? = nil,
                      timeout: Duration? = nil) async throws -> String

    public func inputVerbose(_ prompt: String, ...) async throws -> AgentResult
    public func stream(_ prompt: String) -> AsyncThrowingStream<String, Error>

    // コンテキスト操作
    public func context() async throws -> String
    public func fork(name: String? = nil) async throws -> Agent
    public func add(_ other: Agent) async throws
    public func addSummary(_ other: Agent) async throws
    public func branch(from fromIndex: Int, name: String? = nil) async throws -> Agent

    // シリアライズ
    public func export() async throws -> JSONValue
    public func importHistory(_ data: JSONValue) async throws

    // 登録
    @discardableResult public func registerTools(_ tools: Tool...) async throws -> [String]
    @discardableResult public func registerTools(_ tools: [Tool]) async throws -> [String]
    @discardableResult public func registerGuards(_ guards: GuardSpec...) async throws -> [String]
    @discardableResult public func registerVerifiers(_ verifiers: Verifier...) async throws -> [String]
    public func registerJudge(_ name: String, _ handler: @escaping JudgeHandler) async throws
    @discardableResult public func registerMCP(command: String? = nil,
                                               args: [String] = [],
                                               env: [String: String]? = nil,
                                               transport: String = "stdio",
                                               url: String? = nil) async throws -> [String]
    @discardableResult public func registerSkills(_ skillDir: String) async throws -> [String]

    // ユーティリティ
    public func search(_ query: String, topK: Int = 5) async -> [SearchHit]
    public func batch(_ prompts: [String],
                      maxConcurrency: Int = 3,
                      timeout: Duration? = nil) async throws -> [String]
    public func improveTool(_ toolName: String, feedback: String) async throws -> Tool?
}
```

### `RawAgent` (低レベル)

```swift
public actor RawAgent {
    public init(binaryPath: String = "agent",
                env: [String: String]? = nil,
                cwd: String? = nil)

    public func start() async throws
    public func close() async

    @discardableResult
    public func configure(_ config: CoreAgentConfig) async throws -> [String]

    public func run(_ prompt: String,
                    maxTurns: Int? = nil,
                    stream: StreamCallback? = nil,
                    onStatus: StatusCallback? = nil,
                    timeout: Duration? = nil) async throws -> AgentResult

    @discardableResult public func abort(reason: String = "") async throws -> Bool
    public func summarize() async throws -> String

    @discardableResult public func registerTools(_ tools: [Tool]) async throws -> [String]
    @discardableResult public func registerGuards(_ guards: [GuardSpec]) async throws -> [String]
    @discardableResult public func registerVerifiers(_ verifiers: [Verifier]) async throws -> [String]
    public func registerJudge(name: String, handler: @escaping JudgeHandler) async throws
    @discardableResult public func registerMCP(...) async throws -> [String]

    /// 内部用: 任意の JSON-RPC メソッドを直接呼ぶ (session.history / session.inject 等)
    public func rawCall(_ method: String, params: JSONValue) async throws -> JSONValue
}
```

### 戻り値型

```swift
public struct AgentResult: Sendable, Equatable {
    public var response: String
    public var reason: String      // "completed" / "max_turns" / "aborted" / ...
    public var turns: Int
    public var usage: UsageInfo
}

public struct UsageInfo: Sendable, Equatable {
    public var promptTokens: Int
    public var completionTokens: Int
    public var totalTokens: Int
}

public struct SearchHit: Sendable, Equatable {
    public let role: String
    public let content: String
    public let score: Double
    public let id: Int
}
```

### コールバック型

```swift
public typealias StreamCallback  = @Sendable (_ text: String, _ turn: Int) async -> Void
public typealias StatusCallback  = @Sendable (_ usageRatio: Double, _ count: Int, _ limit: Int) async -> Void
public typealias JudgeHandler    = @Sendable (_ response: String, _ turn: Int) async throws -> (Bool, String)
public typealias ToolHandler     = @Sendable (JSONValue) async throws -> ToolReturn
public typealias VerifierHandler = @Sendable (_ toolName: String, _ args: JSONValue, _ result: String) async throws -> (Bool, String)
public typealias GuardHandler    = @Sendable (GuardInput) async throws -> (GuardDecision, String)
```

## 設定リファレンス

### `AgentConfig` (高レベル)

| プロパティ          | 型                       | 説明                                                 |
| ------------------- | ------------------------ | ---------------------------------------------------- |
| `binary`            | `String`                 | `agent` バイナリのパス                              |
| `env`               | `[String: String]?`      | 子プロセスへ追加する環境変数                         |
| `cwd`               | `String?`                | 子プロセスの作業ディレクトリ                         |
| `systemPrompt`      | `String?`                | システムプロンプト                                   |
| `maxTurns`          | `Int?`                   | 最大ターン数 (default 20)                           |
| `tokenLimit`        | `Int?`                   | コンテキストトークン上限                             |
| `workDir`           | `String?`                | エージェントの作業ディレクトリ                       |
| `delegate`          | `DelegateConfig?`        | サブエージェント委任                                 |
| `coordinator`       | `CoordinatorConfig?`     | 並列サブエージェント                                 |
| `compaction`        | `CompactionConfig?`      | コンテキスト圧縮                                     |
| `permission`        | `PermissionConfig?`      | ツール実行パーミッション                             |
| `guards`            | `GuardsConfig?`          | 入力/ツール呼出/出力ガード                           |
| `verify`            | `VerifyConfig?`          | ベリファイアループ                                   |
| `toolScope`         | `ToolScopeConfig?`       | ツールスコーピング                                   |
| `reminder`          | `ReminderConfig?`        | システムリマインダー                                 |
| `streaming`         | `StreamingConfig?`       | ストリーミング通知                                   |
| `loop`              | `LoopConfig?`            | ループパターン (`react`/`reaf`)                      |
| `router`            | `RouterConfig?`          | ルーター専用 LLM                                     |
| `judge`             | `JudgeConfig?`           | ゴール達成判定器                                     |

### サブ設定

```swift
DelegateConfig(enabled: true, maxChars: 4000)
CoordinatorConfig(enabled: true, maxChars: 4000)
CompactionConfig(enabled: true, budgetMaxChars: 16000, keepLast: 4,
                 targetRatio: 0.5, summarizer: "llm")
PermissionConfig(enabled: true, deny: ["delete_*"], allow: ["read_*"])
GuardsConfig(input: ["no_secrets"], toolCall: ["fs_root_only"], output: ["pii"])
VerifyConfig(verifiers: ["non_empty"], maxStepRetries: 2, maxConsecutiveFailures: 3)
ToolScopeConfig(maxTools: 8, includeAlways: ["finish"])
ReminderConfig(threshold: 4000, content: "Stay concise.")
StreamingConfig(enabled: true, contextStatus: true)
LoopConfig(type: "react")
RouterConfig(endpoint: "http://...", model: "...", apiKey: "...")
JudgeConfig(name: "goal")
```

camelCase で書いて、JSON 送信時に自動で snake_case に変換される。

#### `CompactionConfig` の 4 段カスケード (ADR-005)

`enabled: true` で会話履歴がトークン上限に近づいたとき、以下の 4 段の縮約戦略が
順に試される。前の段で目標サイズに収まれば次の段は走らない。

| 段                    | 戦略                                                                  |
| --------------------- | --------------------------------------------------------------------- |
| 1. ツール結果トリム     | tool ロールメッセージの content を `budgetMaxChars` の 1/2 程度で切る  |
| 2. アシスタント中間刈り | 古いアシスタント発話のうち `keepLast` 件より前を削除                    |
| 3. ユーザー圧縮         | 古いユーザー発話を要約マーカーで置き換え                                |
| 4. LLM 要約             | `summarizer: "llm"` のときに残り全体を LLM で要約 → 単一 system に置換 |

パラメータ:
- `budgetMaxChars` — 縮約後に目指す総文字数 (推奨: token 上限の 60〜70% 相当)
- `keepLast` — 直近何件のアシスタント発話を必ず残すか (推奨 3〜5)
- `targetRatio` — 縮約発火しきい値 (0.0〜1.0)。`token_count / token_limit` がこの値を
  超えたら起動
- `summarizer` — `"none"` (省略) / `"truncate"` (機械的) / `"llm"` (LLM 要約)

#### `LoopConfig.type` のループパターン

| 値          | 動作                                                                     |
| ----------- | ------------------------------------------------------------------------ |
| `"react"`   | デフォルト。ルーター → ツール → レスポンスの ReAct ループ                 |
| `"reaf"`    | ReAF。各ツール実行後に Verifier 評価ステップを挟む。`verify` 設定必須      |

`"reaf"` を使う場合は `VerifyConfig(verifiers: [...])` の登録が必須。各ツール結果に対し
`verifier.execute` が走り、`passed=false` のときは `maxStepRetries` 内でリトライする。

## ツール / ガード / ベリファイア / ジャッジ

### `Tool`

```swift
public struct Tool: Sendable {
    public init(name: String,
                description: String = "",
                parameters: JSONValue = ...,
                readOnly: Bool = false,
                handler: @escaping ToolHandler)
}
```

ハンドラは `async throws` で、`ToolReturn` を返す:

```swift
public enum ToolReturn: Sendable {
    case text(String)
    case structured(content: String, isError: Bool = false, metadata: [String: JSONValue]? = nil)
}
```

### AgentConfig で「定義」と「有効化」を 1 つに (B1〜B4)

旧 API は `Agent.start()` の中で `configure` が呼ばれていたため、
`AgentConfig.guards = GuardsConfig(input: ["my_guard"])` のように名前指定しても、
`registerGuards` は `start()` の後でしか呼べず **必ず "unknown guard" になる** という
ライフサイクル設計の罠があった。

現在は `AgentConfig.customGuards` / `customTools` / `customVerifiers` /
`customJudges` を使えば、`start()` 内で「subprocess → register → configure」の
順に処理される。設定 1 箇所で完結する:

```swift
let agent = Agent(config: AgentConfig(
    binary: "./agent",
    // 「どの名前を有効化するか」の設定
    guards:    GuardsConfig(input: ["no_secrets"]),
    verify:    VerifyConfig(verifiers: ["non_empty"]),
    judge:     JudgeConfig(name: "concise"),
    toolScope: ToolScopeConfig(maxTools: 3, includeAlways: ["echo"]),
    // 「ハンドラの実装」 (start() 内で自動 register)
    customTools: [
        Tool(name: "echo") { args in .text(args["text"]?.stringValue ?? "") },
    ],
    customGuards: [
        GuardSpec.input(name: "no_secrets") { input in
            input.contains("password") ? .deny("contains secret") : .allow
        },
    ],
    customVerifiers: [
        Verifier(name: "non_empty") { _, _, r in
            r.isEmpty ? .fail("empty") : .pass
        },
    ],
    customJudges: [
        "concise": { resp, _ in
            resp.count >= 30 ? .done("long enough") : .continue
        },
    ]
))
try await agent.start()  // ここまでで全部 register + configure 済み
```

`Agent.registerTools(...)` / `registerGuards(...)` を **start() 後に追加で**
呼ぶ経路も従来通り使える (動的にスキルを足したいケース)。

### `GuardSpec`

戻り値は `GuardOutcome` (struct)。タプル風の 2 引数 init / `.allow` / `.deny(_:)`
/ `.tripwire(_:)` のショートカットが利用可能 (D3 で型化)。

```swift
GuardSpec.input(name: "no_secrets") { input in
    input.contains("password") ? .deny("secret detected") : .allow
}

GuardSpec.toolCall(name: "fs_root_only") { toolName, args in
    let path = args["path"]?.stringValue ?? ""
    return path.hasPrefix("/") ? .deny("root path") : .allow
}

GuardSpec.output(name: "pii_redactor") { output in
    output.contains("@") ? .deny("looks like email") : .allow
}
```

`GuardDecision` は `.allow` / `.deny` / `.tripwire`。
`tripwire` を返すと `TripwireTriggered` が SDK 側で throw される (重大ガード)。

### `Verifier`

戻り値は `VerifierOutcome` (struct)。`.pass` / `.fail(_:)` ショートカット利用可。

```swift
let nonEmpty = Verifier(name: "non_empty") { toolName, args, result in
    result.isEmpty ? .fail("result empty") : .pass
}
try await agent.registerVerifiers(nonEmpty)
```

### ジャッジ (ゴール達成判定)

戻り値は `JudgeOutcome`。`.continue` / `.done(_:)` ショートカット利用可。

```swift
try await agent.registerJudge("goal") { response, turn in
    response.contains("FINAL ANSWER") ? .done("marker detected") : .continue
}

// AgentConfig(judge: JudgeConfig(name: "goal")) で有効化
```

## MCP / スキル統合

```swift
// 単体 stdio サーバ
try await agent.registerMCP(
    command: "uvx",
    args: ["mcp-server-fetch"],
    env: ["FOO": "bar"]
)

// SSE サーバ
try await agent.registerMCP(transport: "sse", url: "https://...")

// スキルディレクトリ (各サブの skill.json / mcp.json / config.json を読む)
try await agent.registerSkills("./skills/")
```

### スキル設定ファイルのスキーマ (H1)

`registerSkills(_:)` はディレクトリ直下の各サブディレクトリを走査し、
以下の優先順で 1 ファイルを読み込む:

1. `skill.json`
2. `mcp.json`
3. `config.json`

ファイル形式は **MCP 設定 (stdio または sse)** の JSON で、フィールドは以下:

```jsonc
// stdio トランスポート
{
  "transport": "stdio",          // 省略時は "stdio"
  "command": "uvx",               // 必須 (stdio のみ): 起動コマンド
  "args": ["mcp-server-fetch"],  // 省略可: 引数配列
  "env": {                        // 省略可: 追加環境変数
    "API_KEY": "sk-..."
  }
}

// SSE トランスポート
{
  "transport": "sse",
  "url": "https://api.example.com/mcp"  // 必須 (sse のみ)
}
```

ディレクトリ構造の例:

```
skills/
├── fetch/
│   └── skill.json          # mcp-server-fetch を起動する設定
├── filesystem/
│   └── mcp.json            # mcp-server-filesystem を起動する設定
└── custom/
    └── config.json         # 任意のサーバ
```

各スキルディレクトリの 1 ファイルが MCP セッションとして接続され、
公開ツールがすべて Agent に登録される。1 つでもエラーになっても残りは
処理される (登録できたツール名の配列が返る)。

## ストリーミング

### コールバック方式

```swift
_ = try await agent.input(
    "長めの説明をして",
    onDelta: { text, turn in print(text, terminator: "") },
    onStatus: { ratio, count, limit in print("[ctx \(Int(ratio * 100))%]") }
)
```

### AsyncStream 方式

```swift
for try await chunk in agent.stream("...") {
    print(chunk, terminator: "")
}
```

`StreamingConfig(enabled: true)` を `AgentConfig` に設定すること。未設定なら
完了後に 1 チャンクとして配信される。

## LLM ハンドラ (任意 API 形式で叩く)

Go ハーネスはデフォルトで `SLLM_ENDPOINT` の OpenAI 互換 HTTP API を叩く。
**OpenAI 非互換 API (Anthropic / Bedrock / Vertex AI / ollama / 独自プロキシ等)
を使いたい、あるいはテスト時に LLM をモックしたい場合**は、`llmHandler` を
指定して `agent.configure` の LLM mode を `.remote` に切り替える (ADR-016)。

指定すると、コアの **すべての ChatCompletion 呼び出し** (ルーター / 応答生成 /
ジャッジ / コンテキスト要約) が `llm.execute` 逆 RPC として Swift クロージャに
転送される。

```swift
import AIAgent

let myLLM: LLMHandler = { request in
    // request は OpenAI 互換 ChatRequest を表す JSONValue
    // ここで Anthropic / Bedrock / ollama / mock 等に変換して叩く
    // OpenAI 互換 ChatResponse を JSONValue で返す
    return .object([
        "id": .string("wrapper-1"),
        "object": .string("chat.completion"),
        "created": .int(0),
        "model": request["model"] ?? .string("custom"),
        "choices": .array([
            .object([
                "index": .int(0),
                "message": .object([
                    "role": .string("assistant"),
                    "content": .string("..."),
                ]),
                "finish_reason": .string("stop"),
            ])
        ]),
        "usage": .object([
            "prompt_tokens": .int(0),
            "completion_tokens": .int(0),
            "total_tokens": .int(0),
        ]),
    ])
}

let agent = Agent(config: AgentConfig(
    binary: "./agent",
    llmHandler: myLLM   // 自動で llm.mode=.remote が適用される
    // SLLM_ENDPOINT は不要 (使われない)
))
let response = try await agent.input("hi")
```

明示制御したい場合は `llm: LLMConfig(mode: .remote, timeoutSeconds: 180)` を併用:

```swift
let agent = Agent(config: AgentConfig(
    binary: "./agent",
    llm: LLMConfig(mode: .remote, timeoutSeconds: 180),
    llmHandler: myLLM
))
```

低レベル `RawAgent` から使う場合は `setLLMHandler` を `configure` 前に呼ぶ:

```swift
let raw = RawAgent(binaryPath: "./agent")
try await raw.start()
await raw.setLLMHandler(myLLM)
_ = try await raw.configure(CoreAgentConfig(llm: LLMConfig(mode: .remote)))
```

### 注意点

- ルーターは JSON mode で `{"tool":..., "arguments":..., "reasoning":...}` を
  期待する。`request["response_format"]?["type"]?.stringValue == "json_object"`
  なら **必ず有効な JSON 文字列を `content` に入れて返す** こと
- クロージャから throw すると JSON-RPC エラーとして ChatCompletion が失敗する
- ストリーミング (`StreamingConfig(enabled: true)`) と `mode=.remote` を併用しても
  delta 通知は飛ばない (ハンドラ完了後に 1 回まとめて配信)
- 動作確認の最小例: `sdk/swift/Tests/AIAgentTests/AgentE2ETests.swift` の
  `testLLMRemoteRoutesThroughHandler`

### 完全ローカル化サンプル (H3): URLSession + Anthropic Messages API

`SLLM_ENDPOINT` を一切立てず、ホスト側 Swift から Anthropic の Messages API
を叩く完全な例。**`llmHandler` だけで成立する** (ローカル LLM サーバ不要)。

```swift
import AIAgent
import Foundation

func anthropicMessagesHandler(apiKey: String) -> LLMHandler {
    return { request in
        // 1. OpenAI 互換 ChatRequest → Anthropic Messages 形式に変換
        let messages = (request["messages"]?.arrayValue ?? [])
            .filter { $0["role"]?.stringValue != "system" }
            .map { msg -> JSONValue in
                .object([
                    "role": .string(msg["role"]?.stringValue == "assistant" ? "assistant" : "user"),
                    "content": .string(msg["content"]?.stringValue ?? ""),
                ])
            }
        let systemPrompt = (request["messages"]?.arrayValue ?? [])
            .first(where: { $0["role"]?.stringValue == "system" })?
            ["content"]?.stringValue ?? ""

        var anthReq: [String: JSONValue] = [
            "model": .string(request["model"]?.stringValue ?? "claude-haiku-4-5-20251001"),
            "max_tokens": .int(4096),
            "messages": .array(messages),
        ]
        if !systemPrompt.isEmpty { anthReq["system"] = .string(systemPrompt) }

        // 2. HTTP リクエスト
        let body = try JSONSerialization.data(withJSONObject: JSONValue.object(anthReq).toRaw())
        var req = URLRequest(url: URL(string: "https://api.anthropic.com/v1/messages")!)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.setValue(apiKey, forHTTPHeaderField: "x-api-key")
        req.setValue("2023-06-01", forHTTPHeaderField: "anthropic-version")
        req.httpBody = body

        let (data, _) = try await URLSession.shared.data(for: req)
        let raw = try JSONSerialization.jsonObject(with: data)
        let resp = JSONValue.from(raw)

        // 3. Anthropic レスポンス → OpenAI 互換 ChatResponse に変換して返す
        let content = resp["content"]?.arrayValue?
            .compactMap { $0["text"]?.stringValue }
            .joined() ?? ""

        return .object([
            "id": resp["id"] ?? .string("anth-1"),
            "object": .string("chat.completion"),
            "created": .int(Int64(Date().timeIntervalSince1970)),
            "model": resp["model"] ?? .string("claude"),
            "choices": .array([.object([
                "index": .int(0),
                "message": .object([
                    "role": .string("assistant"),
                    "content": .string(content),
                ]),
                "finish_reason": .string("stop"),
            ])]),
            "usage": .object([
                "prompt_tokens": resp["usage"]?["input_tokens"] ?? .int(0),
                "completion_tokens": resp["usage"]?["output_tokens"] ?? .int(0),
                "total_tokens": .int(
                    (resp["usage"]?["input_tokens"]?.intValue ?? 0)
                    + (resp["usage"]?["output_tokens"]?.intValue ?? 0)
                ),
            ]),
        ])
    }
}

// 使用例 — SLLM_ENDPOINT / SLLM_API_KEY は一切設定しない
let agent = Agent(config: AgentConfig(
    binary: "./agent",
    systemPrompt: "あなたは親切なアシスタントです。",
    maxTurns: 10,
    llmHandler: anthropicMessagesHandler(apiKey: "sk-ant-...")
))
let reply = try await agent.input("こんにちは")
```

ポイント:
- `env` (SLLM_ENDPOINT/SLLM_API_KEY) を空のまま渡しても動く
- `llmHandler` を渡すと `LLMConfig(mode: .remote)` が自動適用される
- ルーターステップ用 (response_format=json_object) と応答生成用は **同一 handler** で
  処理する。判別したい場合は `request["response_format"]?["type"]?.stringValue == "json_object"`
  でフラグ立てして、ルーター時に出力を JSON 文字列に強制すること

## フォーク / ブランチ / バッチ

```swift
// fork: 現在の履歴をコピーした子エージェント
let child = try await parent.fork()

// branch: from_index 以降だけ引き継ぐ
let alt = try await parent.branch(from: 4)

// add: 他エージェントの履歴を末尾に追加
try await main.add(researcher)

// addSummary: 要約を注入
try await main.addSummary(researcher)

// batch: 複数プロンプトを並列でフォーク処理
let results = try await agent.batch(
    ["要約して", "翻訳して", "目次を作って"],
    maxConcurrency: 3
)
```

## RAG 検索 / 会話要約

```swift
// TF-IDF ベースのキーワード検索 (外部依存なし、日本語対応)
let hits = await agent.search("Tokyo", topK: 5)
// → [SearchHit(role: "user", content: "...", score: 0.8, id: 3), ...]

// LLM による会話要約
let summary = try await agent.context()
print(summary)
```

## 小型モデル向けチューニング指針 (H4)

E2B (Gemma 4 E2B など 1〜3B クラス) や Qwen-1.5B のような SLLM で動かす際の
**実用的な設定の出発点**。

### 推奨初期値

```swift
let config = AgentConfig(
    binary: "./agent",
    env: [
        "SLLM_ENDPOINT": "http://localhost:8080/v1/chat/completions",
        "SLLM_API_KEY": "sk-local",
    ],
    systemPrompt: "あなたは正確な日本語で簡潔に答えるアシスタントです。",
    maxTurns: 10,                       // 小型は長いほど暴走しやすい
    tokenLimit: 8000,                   // モデルのコンテキスト × 0.8 を目安
    compaction: CompactionConfig(       // 必ず有効化
        enabled: true,
        budgetMaxChars: 6000,
        keepLast: 3,
        targetRatio: 0.6,
        summarizer: "truncate"          // LLM 要約は小型では精度が落ちる
    ),
    permission: PermissionConfig(       // 危険ツールは明示拒否
        enabled: true,
        deny: ["shell", "fs_write_*"],
        allow: nil
    ),
    toolScope: ToolScopeConfig(
        maxTools: 5,                    // ルーター精度を上げる
        includeAlways: ["finish"]
    ),
    reminder: ReminderConfig(           // 暴走防止リマインダ
        threshold: 4,
        content: "簡潔に。1 ターンに 1 ツールだけ。"
    ),
    streaming: StreamingConfig(enabled: true, contextStatus: true),
    loop: LoopConfig(type: "react"),    // ReAF は小型では verifier 精度不足
    judge: JudgeConfig(name: "concise") // judge 必須 (本文参照)
)
```

### 各パラメータの効きどころ

| 設定                       | 値               | 理由                                                                       |
| -------------------------- | ---------------- | -------------------------------------------------------------------------- |
| `maxTurns`                 | 8〜12            | 上を増やすほど無限ループ気味になる。max_turns 到達は正常終了 (A3)            |
| `tokenLimit`               | ctx \* 0.8       | コンパクション余地を確保。8K モデルなら 6000〜6500                          |
| `compaction.summarizer`    | `"truncate"`     | LLM 要約は小型では会話の文脈を歪めるので非推奨                              |
| `compaction.keepLast`      | 2〜3             | 直近を確実に残し、それより前を縮める                                       |
| `toolScope.maxTools`       | 3〜5             | ルーターが選択に迷うのを防ぐ                                               |
| `reminder.threshold`       | 3〜5 ターン      | 「1 回しか呼ぶな」のような指示はリマインダで毎ターン再注入                  |
| `judge`                    | カスタム必須     | デフォルト judge がないため、応答長 / キーワード判定で `done` を返す       |
| `LLMConfig.timeoutSeconds` | 30〜60           | 小型モデルは遅いので、HTTP は長めに                                        |

### Judge の必須実装例

小型モデルは「答え終わったのに finish ツールを呼ばない」ことが頻発する。
**応答長と内容で完了を判定する judge は必須に近い**:

```swift
let isConcise: JudgeHandler = { response, turn in
    // 30 文字以上の自然言語応答が出たら done
    if response.count >= 30 && !response.contains("```") {
        return .done("response is long enough")
    }
    return .continue
}

try await agent.registerJudge("concise", isConcise)
```

`AgentConfig(judge: JudgeConfig(name: "concise"))` で結びつける。
Phase 3-1 で予定している `defaultJudge` が実装されるまでは利用側で必須。

### 動作確認したモデル

| モデル                | endpoint                                   | 推奨 maxTurns | 注記                       |
| --------------------- | ------------------------------------------ | ------------- | -------------------------- |
| Gemma 4 E2B (local)   | `http://localhost:8080/v1/chat/completions`| 10            | このリポジトリの主用       |
| Qwen2.5-3B (ollama)   | `http://localhost:11434/v1/chat/completions`| 10            | ollama デフォルトでも動作  |
| Llama-3.2-3B (ollama) | `http://localhost:11434/v1/chat/completions`| 8             | finish 検出弱め、judge 厚く |

## JSONValue リファレンス

`JSONValue` は動的な JSON を表す列挙型。ツール引数や任意の RPC パラメータに使う。

```swift
public enum JSONValue: Sendable, Codable, Equatable {
    case null
    case bool(Bool)
    case int(Int64)
    case double(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])
}
```

### リテラル風コンストラクタ

```swift
let v: JSONValue = [
    "name": "Alice",
    "age": 30,
    "active": true,
    "scores": [1, 2, 3],
    "meta": ["role": "admin"],
    "empty": nil,
]

v["name"]?.stringValue     // → "Alice"
v["age"]?.intValue         // → 30
v["scores"]?[1]?.intValue  // → 2
```

### 任意の値からの生成

```swift
let v = JSONValue.from(["a": 1, "b": [true, false]])
```

`NSNumber` を `Bool`/`Int`/`Double` に正しく振り分けるため、JSONSerialization
が返す `as? Bool` の罠 (整数 1 が `true` にマッチする) を回避している。

## エラーハンドリング

`AgentError` は `LocalizedError` 準拠なので `error.localizedDescription` で
有意な文字列が得られる (`"The operation couldn't be completed."` のような
汎用文字列ではない)。エラー分岐は **クラスでも `kind` でも** 可能。

### kind ベースの分岐 (推奨)

```swift
do {
    _ = try await agent.input("...")
} catch let e as AgentError {
    switch e.kind {
    case .tripwireTriggered:    alertSecurityTeam(e.message)
    case .guardDenied:          print("拒否: \(e.message)")
    case .toolNotFound:         print("ツール未登録")
    case .agentBusy:            print("別 run 中")
    case .aborted:              print("中断")
    case .maxTurnsReached:      print("ターン上限") // 通常は AgentResult.isMaxTurns
    case .other(let code):      print("RPC エラー code=\(String(describing: code))")
    default:                    print(e.localizedDescription)
    }
    if let stderr = e.data?["stderr_tail"]?.stringValue {
        print("--- stderr tail ---\n\(stderr)")
    }
}
```

### サブクラスでの分岐 (旧 API 互換)

```swift
do {
    _ = try await agent.input("...")
} catch let e as TripwireTriggered { alertSecurityTeam(e.reason) }
catch let e as GuardDenied         { print("拒否: \(e.reason)") }
catch is AgentBusy                 { print("既に別の run が実行中") }
catch is AgentAborted              { print("abort されました") }
catch let e as ToolError           { print("ツール失敗: \(e.localizedDescription)") }
catch let e as AgentError          { print(e.localizedDescription) }
```

### エラーコード対照

| クラス              | `kind`               | JSON-RPC code      | 発生条件                                |
| ------------------- | -------------------- | ------------------ | --------------------------------------- |
| `AgentError` (基底) | `.other(code:)`      | その他              | SDK 基底クラス                          |
| `AgentBusy`         | `.agentBusy`         | `-32002`           | 既に別の `agent.run` が実行中            |
| `AgentAborted`     | `.aborted`           | `-32003`           | `abort()` でキャンセル                    |
| (なし)              | `.messageTooLarge`   | `-32004`           | メッセージサイズ超過                    |
| `ToolError`         | `.toolNotFound`/`.toolExecutionFailed` | `-32000`/`-32001` | ツール未登録 / 実行失敗 |
| `GuardDenied`       | `.guardDenied`       | `-32005`           | ガードが `deny` を返した                  |
| `TripwireTriggered` | `.tripwireTriggered` | `-32006`           | tripwire ガード発火                       |
| (なし)              | `.maxTurnsReached`   | `-32603` + msg     | ターン上限 (現バージョンでは AgentResult として返るため通常は発生しない) |

### `max_turns` 到達は **エラーではない** (A3)

旧バージョン (< 0.2.2) では `max_turns` 到達は `AgentError(code=-32603)` で
throw されていた。**現バージョン以降は `AgentResult` で正常 return** し、
`response` に直近のアシスタント発話が、`reason` に `"max_turns"` が入る:

```swift
let result = try await agent.inputVerbose("...")
if result.isMaxTurns {
    print("ターン上限で停止: \(result.response)")
    // result.toolCalls で何のツールが呼ばれたか確認可能
}
```

### バイナリのバージョン整合チェック (E3)

`AgentConfig.versionCheck` で `start()` 時のハンドシェイク挙動を変えられる:

| 値      | 動作                                                           |
| ------- | -------------------------------------------------------------- |
| `.warn` | (デフォルト) 不一致なら stderr に警告を書いて続行              |
| `.strict` | 不一致なら `AgentError` を throw して `start()` を失敗させる |
| `.skip` | ハンドシェイクをスキップ (旧バイナリ互換用)                    |

旧バイナリ (`server.info` 未実装) は `-32601` を返すため、SDK は明確な案内文を
出す: `binary does not implement server.info ... Rebuild the agent binary...`。

### 観測性: run 実行中に何が呼ばれたか (G1〜G4)

`inputVerbose` の `AgentResult` には以下のフィールドが入る:

```swift
let result = try await agent.inputVerbose(
    "コードレビューして",
    onPhase: { phase in print("[phase] \(phase.rawValue)") }
)

// G1/G2: ツール呼び出し履歴
print("呼ばれたツール: \(result.toolCalls.map { $0.name })")
let echoCount = result.toolCalls.filter { $0.name == "echo" }.count

// G4: ガード/ベリファイア/ジャッジ発火履歴
for fire in result.guardFires {
    print("[\(fire.kind)] \(fire.name) → \(fire.decision): \(fire.reason)")
}

// G3: フェーズ通知 (onPhase に渡したコールバック経由)
// .routing → .tool → .guarding → .generating
```

## テスト

**リポジトリルートから実行する** (Package.swift がルートにあるため):

```bash
# リポジトリのルートで
cd /path/to/agent-util

# ユニットテスト (バイナリ不要、16 ケース)
swift test

# E2E (実バイナリ + 実 LLM、6 ケース)
go build -o bin/agent ./cmd/agent/
AGENT_BINARY="$(pwd)/bin/agent" swift test
```

環境変数で LLM 接続先を上書き可能 (default `http://localhost:8080/v1/chat/completions` / `sk-gemma4`):

```bash
SLLM_ENDPOINT="https://api.openai.com/v1/chat/completions" \
SLLM_API_KEY="sk-..." \
AGENT_BINARY=./bin/agent swift test
```

## 実装メモ

Swift で実装する上で踏んだ落とし穴。同様の SDK を作る人向けのメモ:

| 問題                                                              | 対処                                                                                  |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `JSONSerialization` が返す `NSNumber(1)` が `as? Bool` にマッチして `id: 1` が `true` 化 | `NSNumber.objCType` で振り分けるよう `JSONValue.from` を実装                          |
| `FileHandle.read(upToCount:)` が Pipe で期待通りにブロック解除されない              | `readabilityHandler` + `AsyncStream` ベースのイベント駆動読み出しに変更                |
| `Process.waitUntilExit()` がブロッキングで cooperative pool を奪う | `DispatchQueue.global()` に逃がし、`withCheckedContinuation` でブリッジ                |
| Race: 送信を先に行うと、即座に返ったレスポンスを取りこぼす          | `state.registerPending` を必ず `writeMessage` より前に await する                       |

## 参考

- [`../../docs/openrpc.json`](../../docs/openrpc.json) — OpenRPC 1.2.6 完全仕様
- [`../../docs/schemas/`](../../docs/schemas/) — 各型の JSON Schema
- [`../../pkg/protocol/methods.go`](../../pkg/protocol/methods.go) — Go 側の真実の源
- [`../README.md`](../README.md) — SDK 全体のハブ
- [`../python/`](../python/) — 兄弟 Python SDK (同じ AOM)
- [`../js/`](../js/) — 兄弟 Node SDK
- ADR-001 (JSON-RPC over stdio), ADR-013 (RemoteTool アダプタ)
