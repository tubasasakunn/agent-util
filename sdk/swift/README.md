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
11. [フォーク / ブランチ / バッチ](#フォーク--ブランチ--バッチ)
12. [RAG 検索 / 会話要約](#rag-検索--会話要約)
13. [JSONValue リファレンス](#jsonvalue-リファレンス)
14. [エラーハンドリング](#エラーハンドリング)
15. [テスト](#テスト)
16. [実装メモ](#実装メモ)

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

`Package.swift` に依存追加:

```swift
// ローカルパス
.package(path: "../path/to/ai-agent/sdk/swift")

// Git
.package(url: "https://github.com/your-org/ai-agent.git", from: "0.1.0")
```

ターゲット依存:

```swift
.product(name: "AIAgent", package: "swift"),  // ローカル
// または
.product(name: "AIAgent", package: "ai-agent"),
```

import:

```swift
import AIAgent
```

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

### `GuardSpec`

```swift
GuardSpec.input(name: "no_secrets") { input in
    input.contains("password") ? (.deny, "secret detected") : (.allow, "")
}

GuardSpec.toolCall(name: "fs_root_only") { toolName, args in
    let path = args["path"]?.stringValue ?? ""
    return path.hasPrefix("/") ? (.deny, "root path") : (.allow, "")
}

GuardSpec.output(name: "pii_redactor") { output in
    output.contains("@") ? (.deny, "looks like email") : (.allow, "")
}
```

戻り値は `(GuardDecision, String)`。`GuardDecision` は `.allow` / `.deny` /
`.tripwire`。

### `Verifier`

```swift
let nonEmpty = Verifier(name: "non_empty") { toolName, args, result in
    (!result.isEmpty, result.isEmpty ? "result empty" : "ok")
}
try await agent.registerVerifiers(nonEmpty)
```

### ジャッジ (ゴール達成判定)

```swift
try await agent.registerJudge("goal") { response, turn in
    return (response.contains("FINAL ANSWER"), "marker detected")
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

```swift
do {
    _ = try await agent.input("...")
} catch let e as TripwireTriggered {
    alertSecurityTeam(e.reason)
} catch let e as GuardDenied {
    print("拒否: \(e.reason)")
} catch is AgentBusy {
    print("既に別の run が実行中")
} catch is AgentAborted {
    print("abort されました")
} catch let e as ToolError {
    print("ツール失敗: \(e)")
} catch let e as AgentError {
    print("SDK エラー: \(e)")
}
```

| クラス              | JSON-RPC code      | 発生条件                                       |
| ------------------- | ------------------ | ---------------------------------------------- |
| `AgentBusy`         | `-32002`           | 既に別の `agent.run` が実行中                  |
| `AgentAborted`      | `-32003`           | `abort()` でキャンセル                          |
| `ToolError`         | `-32000` / `-32001`| ツールが見つからない / 実行失敗                |
| `GuardDenied`       | `-32005`           | ガードが `deny` を返した                        |
| `TripwireTriggered` | `-32006`           | tripwire ガード発火 (`GuardDenied` のサブクラス) |
| `AgentError`        | その他              | SDK 基底クラス                                  |

## テスト

```bash
# ユニットテスト (バイナリ不要、16 ケース)
swift test

# E2E (実バイナリ + 実 LLM、6 ケース)
AGENT_BINARY=$(pwd)/../../bin/agent swift test
```

環境変数で LLM 接続先を上書き可能 (default `http://localhost:8080/v1/chat/completions` / `sk-gemma4`):

```bash
SLLM_ENDPOINT="https://api.openai.com/v1/chat/completions" \
SLLM_API_KEY="sk-..." \
AGENT_BINARY=./agent swift test
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
