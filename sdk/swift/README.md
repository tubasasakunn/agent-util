# ai-agent Swift SDK

Thin Swift client for the [ai-agent](../../) Go harness. Speaks JSON-RPC
2.0 over stdio with a child `agent --rpc` process.

- **Zero third-party dependencies** — only Foundation.
- Swift 5.9+, `async/await` first.
- Mirrors `pkg/protocol/methods.go` and `docs/openrpc.json` exactly.
- `AsyncThrowingStream` streaming for `for try await` ergonomics.
- Same protocol as the [Python SDK](../python/) and [JS SDK](../js/), so
  the same `agent --rpc` binary can be driven from any of the three.

## Install

```bash
# 1) Build the Go agent binary (from the repo root)
go build -o agent ./cmd/agent/

# 2) Use the SDK from your own SwiftPM project:
```

`Package.swift`:

```swift
.package(url: "https://github.com/tubasasakunn/agent-util.git", from: "0.1.0"),
// then add "AIAgent" to your target dependencies.
```

For local development, pull it in by path:

```swift
.package(path: "../agent-util/sdk/swift")
```

Requires Swift 5.9 or newer. macOS 13+ is the official platform target;
Linux is supported because the SDK uses only Foundation primitives
(`Process`, `Pipe`, `FileHandle`).

## Quickstart

### 1. Minimal run

```swift
import AIAgent

let agent = Agent(options: AgentOptions(binaryPath: "./agent"))
try await agent.start()
defer { Task { await agent.close() } }

let result = try await agent.run("こんにちは")
print(result.response)
```

`agent.run(_:)` returns an ``AgentResult`` with `.response`, `.reason`,
`.turns` and `.usage` (`promptTokens`, `completionTokens`, `totalTokens`).

### 2. Register a tool

```swift
import AIAgent
import Foundation

let readFile = Tool(
    name: "read_file",
    description: "Read a UTF-8 text file from the workspace.",
    parameters: [
        "type": "object",
        "properties": ["path": ["type": "string"]],
        "required": ["path"],
        "additionalProperties": false,
    ],
    readOnly: true
) { args in
    let path = args["path"].stringValue ?? ""
    let text = try String(contentsOfFile: path, encoding: .utf8)
    return .string(text)
}

let agent = Agent(options: AgentOptions(binaryPath: "./agent"))
try await agent.start()
defer { Task { await agent.close() } }

_ = try await agent.registerTools(readFile)
let r = try await agent.run("Read README.md and summarise it.")
print(r.response)
```

The handler receives the parsed `args` as a ``JSONValue``. Return a
``ToolReturn`` — typically `.string(_)`, `.structured(_)` (with
`isError`/`metadata`), or `.json(_)`. Non-string values are coerced.

### 3. Configure guards / permissions / streaming

```swift
let noSecrets = Guard.input(name: "no_secrets") { input in
    if input.lowercased().contains("password") {
        return GuardResult(decision: .deny, reason: "looks like a secret")
    }
    return GuardResult(decision: .allow)
}

let agent = Agent(options: AgentOptions(binaryPath: "./agent"))
try await agent.start()
defer { Task { await agent.close() } }

_ = try await agent.registerGuards(noSecrets)
try await agent.configure(AgentConfig(
    maxTurns: 8,
    permission: PermissionConfig(enabled: true, allow: ["read_file"]),
    guards: GuardsConfig(input: ["no_secrets"]),
    streaming: StreamingConfig(enabled: true)
))

// for-try-await streaming
for try await event in agent.runStream("Walk me through the README.") {
    switch event {
    case .delta(let text, _):
        print(text, terminator: "")
    case .status(let ratio, _, _):
        print("[status \(Int(ratio * 100))%]")
    case .end(let result):
        print("\n---\n" + result.reason)
    }
}
```

## API surface

```swift
public actor Agent {
    public init(options: AgentOptions, rpc: JsonRpcClient = JsonRpcClient())

    public func start() async throws
    public func close() async
    public func stderrOutput() async -> String

    public func configure(_ config: AgentConfig) async throws -> [String]
    public func run(_ prompt: String, options: RunOptions) async throws -> AgentResult
    public nonisolated func runStream(
        _ prompt: String, options: RunOptions
    ) -> AsyncThrowingStream<StreamEvent, Error>
    public func abort(reason: String) async throws -> Bool

    public func registerTools(_ tools: Tool...) async throws -> Int
    public func registerGuards(_ guards: Guard...) async throws -> Int
    public func registerVerifiers(_ verifiers: Verifier...) async throws -> Int
    public func registerMCP(_ options: MCPOptions) async throws -> [String]
}
```

`AgentConfig` and the nested `*Config` structs use Swift `camelCase`
field names but encode to `snake_case` on the wire, so they pass straight
through to the Go core (matching the Python and JS SDKs). `nil` fields
are stripped before serialisation.

### Errors

| Class            | JSON-RPC code       | When                                        |
| ---------------- | ------------------- | ------------------------------------------- |
| `AgentBusy`      | `-32002`            | `agent.run` while another run is in flight  |
| `AgentAborted`   | `-32003`            | A run was cancelled via `agent.abort`       |
| `ToolError`      | `-32000` / `-32001` | Tool not found / tool execution failed      |
| `GuardDenied`    | n/a                 | An input guard returned `.deny` / `.tripwire` |
| `AgentError`     | other               | Base class for everything from the SDK      |

## Tests

```bash
cd sdk/swift
swift test
```

The unit tests don't need the `agent` binary — they exercise the
client against an in-memory `JsonRpcTransport`. For an end-to-end run,
build the Go binary first and point the SDK at it:

```bash
go build -o ../../agent ../../cmd/agent/
# then pass binaryPath: "../../agent" to AgentOptions in your own test
```

## References

- [`docs/openrpc.json`](../../docs/openrpc.json) — full OpenRPC 1.2.6 spec.
- [`docs/schemas/`](../../docs/schemas/) — JSON Schemas for every type.
- [`pkg/protocol/methods.go`](../../pkg/protocol/methods.go) — Go source of truth.
- [`sdk/python/`](../python/) — sister Python SDK (same protocol).
- [`sdk/js/`](../js/) — sister TypeScript SDK (same protocol).
- ADR-001 (JSON-RPC over stdio), ADR-013 (RemoteTool adapter pattern).
