# ai-agent Swift SDK

`AIAgent` — Swift Package for the ai-agent Go harness.

Python/JS SDK と同じ AOM (Agent Object Model) を Swift で実装したもの。
`agent --rpc` バイナリをサブプロセスとして起動し、JSON-RPC 2.0 over stdio で通信する。

## 要件

- macOS 13+ (Foundation, Process, FileHandle を使用)
- Swift 5.9+
- 事前にビルドした `agent` バイナリ:

```bash
go build -o agent ./cmd/agent/
```

## インストール (SwiftPM)

`Package.swift` に依存追加:

```swift
.package(path: "../path/to/ai-agent/sdk/swift")
// または
.package(url: "https://github.com/your-org/ai-agent.git", from: "0.1.0")
```

ターゲット依存:

```swift
.product(name: "AIAgent", package: "swift"),
```

## クイックスタート

```swift
import AIAgent

let config = AgentConfig(
    binary: "./agent",
    env: [
        "SLLM_ENDPOINT": "http://localhost:8080/v1/chat/completions",
        "SLLM_API_KEY": "sk-gemma4",
    ],
    systemPrompt: "あなたは親切なアシスタントです。",
    maxTurns: 20
)

let agent = Agent(config: config)
try await agent.start()
defer { Task { await agent.close() } }

let reply = try await agent.input("こんにちは！")
print(reply)
```

## AOM の主要 API

| メソッド | 説明 |
| --- | --- |
| `input(_:)`         | 入力送信、レスポンス取得 |
| `inputVerbose(_:)`  | turns / token usage 含む詳細結果 |
| `stream(_:)`        | `AsyncThrowingStream<String, Error>` でトークン逐次配信 |
| `context()`         | LLM で会話履歴を要約 |
| `fork()`            | 子エージェント (履歴コピー) |
| `add(_:)`           | 他エージェントの履歴を末尾に追加 |
| `addSummary(_:)`    | 他エージェントの要約を注入 |
| `branch(from:)`     | n 番目以降のメッセージで新エージェント |
| `export()`/`importHistory(_:)` | 会話状態のシリアライズ/復元 |
| `batch(_:)`         | 複数プロンプトを並列処理 (fork ベース) |
| `search(_:)`        | 過去会話を TF-IDF キーワード検索 |
| `registerTools(_:)` | カスタムツール登録 |
| `registerGuards(_:)`/`registerVerifiers(_:)`/`registerJudge(_:_:)` | ガードレール |
| `registerMCP(...)` / `registerSkills(_:)` | MCP / スキル登録 |
| `improveTool(_:feedback:)` | LLM でツール説明を改善 |

## ツール定義

```swift
let echo = Tool(
    name: "echo",
    description: "Echo back the input",
    parameters: .object([
        "type": .string("object"),
        "properties": .object([
            "text": .object(["type": .string("string")]),
        ]),
        "required": .array([.string("text")]),
    ]),
    readOnly: true
) { args in
    .text("ECHO: \(args["text"]?.stringValue ?? "")")
}

try await agent.registerTools(echo)
```

## ガード / ベリファイア

```swift
let inputGuard = GuardSpec.input(name: "no_secrets") { input in
    input.contains("password") ? (.deny, "secret detected") : (.allow, "")
}

let nonEmptyVerifier = Verifier(name: "non_empty") { _, _, result in
    (!result.isEmpty, result.isEmpty ? "result empty" : "ok")
}

try await agent.registerGuards(inputGuard)
try await agent.registerVerifiers(nonEmptyVerifier)
```

## ストリーミング

```swift
for try await chunk in agent.stream("長めの説明をしてください") {
    print(chunk, terminator: "")
}
```

`StreamingConfig(enabled: true)` を `AgentConfig` に設定すると Go コアからの
逐次配信が有効になる。未設定の場合は完了後に 1 チャンクとして配信される。

## 低レベル API

プロトコルに忠実な薄いラッパーが必要なら `RawAgent` を直接使う。

```swift
let raw = RawAgent(binaryPath: "./agent", env: env)
try await raw.start()
let applied = try await raw.configure(CoreAgentConfig(maxTurns: 5))
let result = try await raw.run("hello")
print(result.response, result.turns, result.usage.totalTokens)
await raw.close()
```

## テスト

```bash
# 単体テスト (バイナリ不要)
swift test

# E2E テスト (実バイナリと実 LLM 必要)
AGENT_BINARY=$(pwd)/../../bin/agent swift test --filter AgentE2ETests
```

`SLLM_ENDPOINT`/`SLLM_API_KEY` 環境変数で LLM 接続先を上書きできる
(デフォルト: `http://localhost:8080/v1/chat/completions` + `sk-gemma4`)。
