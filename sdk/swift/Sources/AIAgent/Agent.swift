/// High-level ``Agent`` client.
///
/// Spawns the Go `agent --rpc` binary and exposes `configure`, `run`,
/// `abort` and registration helpers (`registerTools`, `registerGuards`,
/// `registerVerifiers`, `registerMCP`) as `async` methods.
///
/// ```swift
/// let agent = Agent(options: .init(binaryPath: "./agent"))
/// try await agent.start()
/// defer { Task { await agent.close() } }
///
/// try await agent.configure(AgentConfig(maxTurns: 5))
/// let result = try await agent.run("hello")
/// print(result.response)
/// ```
import Foundation

// JSON-RPC method names (mirrors `pkg/protocol/methods.go`).
private enum Methods {
    static let agentRun = "agent.run"
    static let agentAbort = "agent.abort"
    static let agentConfigure = "agent.configure"
    static let toolRegister = "tool.register"
    static let toolExecute = "tool.execute"
    static let mcpRegister = "mcp.register"
    static let guardRegister = "guard.register"
    static let guardExecute = "guard.execute"
    static let verifierRegister = "verifier.register"
    static let verifierExecute = "verifier.execute"

    static let streamDelta = "stream.delta"
    static let streamEnd = "stream.end"
    static let contextStatus = "context.status"
}

public struct UsageInfo: Sendable, Equatable {
    public var promptTokens: Int64
    public var completionTokens: Int64
    public var totalTokens: Int64

    public init(promptTokens: Int64 = 0, completionTokens: Int64 = 0, totalTokens: Int64 = 0) {
        self.promptTokens = promptTokens
        self.completionTokens = completionTokens
        self.totalTokens = totalTokens
    }
}

public struct AgentResult: Sendable, Equatable {
    public var response: String
    public var reason: String
    public var turns: Int
    public var usage: UsageInfo

    public init(response: String = "", reason: String = "", turns: Int = 0, usage: UsageInfo = .init()) {
        self.response = response
        self.reason = reason
        self.turns = turns
        self.usage = usage
    }
}

public typealias StreamCallback = @Sendable (String, Int) async -> Void
public typealias StatusCallback = @Sendable (Double, Int64, Int64) async -> Void

public struct AgentOptions: Sendable {
    /// Path to the compiled `agent` binary. Defaults to `"agent"` (PATH lookup).
    public var binaryPath: String
    /// Extra environment variables for the subprocess.
    public var env: [String: String]?
    /// Working directory for the subprocess.
    public var cwd: String?
    /// How to handle the subprocess `stderr`. Defaults to `.pipe` (captured).
    public var stderr: SubprocessTransport.StderrMode

    public init(
        binaryPath: String = "agent",
        env: [String: String]? = nil,
        cwd: String? = nil,
        stderr: SubprocessTransport.StderrMode = .pipe
    ) {
        self.binaryPath = binaryPath
        self.env = env
        self.cwd = cwd
        self.stderr = stderr
    }
}

public struct RunOptions: Sendable {
    public var maxTurns: Int?
    public var onDelta: StreamCallback?
    public var onStatus: StatusCallback?
    public var timeout: TimeInterval?

    public init(
        maxTurns: Int? = nil,
        onDelta: StreamCallback? = nil,
        onStatus: StatusCallback? = nil,
        timeout: TimeInterval? = nil
    ) {
        self.maxTurns = maxTurns
        self.onDelta = onDelta
        self.onStatus = onStatus
        self.timeout = timeout
    }
}

public struct MCPOptions: Sendable {
    public var command: String?
    public var args: [String]?
    public var env: [String: String]?
    public var transport: String
    public var url: String?

    public init(
        command: String? = nil,
        args: [String]? = nil,
        env: [String: String]? = nil,
        transport: String = "stdio",
        url: String? = nil
    ) {
        self.command = command
        self.args = args
        self.env = env
        self.transport = transport
        self.url = url
    }
}

public enum StreamEvent: Sendable {
    case delta(text: String, turn: Int)
    case status(usageRatio: Double, tokenCount: Int64, tokenLimit: Int64)
    case end(AgentResult)
}

public actor Agent {
    private let options: AgentOptions
    public let rpc: JsonRpcClient

    private var tools: [String: Tool] = [:]
    private var guards: [String: Guard] = [:]
    private var verifiers: [String: Verifier] = [:]

    private var streamCallback: StreamCallback?
    private var statusCallback: StatusCallback?
    private var runInProgress = false

    // MARK: lifecycle

    public init(options: AgentOptions = .init(), rpc: JsonRpcClient = JsonRpcClient()) {
        self.options = options
        self.rpc = rpc
    }

    /// Spawn the agent subprocess and wire up callbacks.
    public func start() async throws {
        try await rpc.connectSubprocess(
            binaryPath: options.binaryPath,
            args: ["--rpc"],
            env: options.env,
            cwd: options.cwd,
            stderr: options.stderr
        )
        await wireHandlers()
    }

    /// Terminate the subprocess and release resources.
    public func close() async {
        await rpc.close()
    }

    /// Captured stderr from the subprocess (handy for debugging).
    public func stderrOutput() async -> String {
        await rpc.stderrOutput
    }

    /// Internal: wire callbacks; exposed for tests that bypass `start()`.
    public func wireHandlers() async {
        let toolHandler: RequestHandler = { [weak self] params in
            guard let self else { return .object(["content": .string(""), "is_error": .bool(true)]) }
            return await self.handleToolExecute(params)
        }
        let guardHandler: RequestHandler = { [weak self] params in
            guard let self else {
                return .object(["decision": .string("deny"), "reason": .string("agent gone")])
            }
            return await self.handleGuardExecute(params)
        }
        let verifierHandler: RequestHandler = { [weak self] params in
            guard let self else {
                return .object(["passed": .bool(false), "summary": .string("agent gone")])
            }
            return await self.handleVerifierExecute(params)
        }
        let deltaHandler: NotificationHandler = { [weak self] params in
            await self?.handleStreamDelta(params)
        }
        let statusHandler: NotificationHandler = { [weak self] params in
            await self?.handleContextStatus(params)
        }
        let endHandler: NotificationHandler = { _ in /* no-op */ }

        await rpc.setRequestHandler(Methods.toolExecute, toolHandler)
        await rpc.setRequestHandler(Methods.guardExecute, guardHandler)
        await rpc.setRequestHandler(Methods.verifierExecute, verifierHandler)
        await rpc.setNotificationHandler(Methods.streamDelta, deltaHandler)
        await rpc.setNotificationHandler(Methods.streamEnd, endHandler)
        await rpc.setNotificationHandler(Methods.contextStatus, statusHandler)
    }

    // MARK: configuration

    @discardableResult
    public func configure(_ config: AgentConfig) async throws -> [String] {
        let params = config.toParams()
        let raw = try await rpc.call(Methods.agentConfigure, params: params)
        if case .object(let dict) = raw, case .array(let arr)? = dict["applied"] {
            return arr.compactMap { $0.stringValue }
        }
        return []
    }

    // MARK: run / abort

    public func run(_ prompt: String, options: RunOptions = .init()) async throws -> AgentResult {
        if runInProgress {
            throw AgentError("agent.run already in progress on this client")
        }
        runInProgress = true
        let previousStream = streamCallback
        let previousStatus = statusCallback
        if let cb = options.onDelta { streamCallback = cb }
        if let cb = options.onStatus { statusCallback = cb }
        defer {
            streamCallback = previousStream
            statusCallback = previousStatus
            runInProgress = false
        }

        var params: [String: JSONValue] = ["prompt": .string(prompt)]
        if let maxTurns = options.maxTurns {
            params["max_turns"] = .int(Int64(maxTurns))
        }
        let raw = try await rpc.call(Methods.agentRun, params: .object(params), timeout: options.timeout)
        return parseAgentResult(raw)
    }

    /// Abort an in-flight ``run(_:options:)``. Returns `true` if a run was
    /// actually cancelled.
    @discardableResult
    public func abort(reason: String = "") async throws -> Bool {
        var params: [String: JSONValue] = [:]
        if !reason.isEmpty { params["reason"] = .string(reason) }
        let raw = try await rpc.call(Methods.agentAbort, params: .object(params))
        return raw["aborted"].boolValue ?? false
    }

    /// Stream the run as an `AsyncThrowingStream`.
    ///
    /// Each iteration yields one of:
    /// * `.delta(text:turn:)` for `stream.delta` notifications
    /// * `.status(usageRatio:tokenCount:tokenLimit:)` for `context.status`
    /// * `.end(_:)` exactly once when the run completes
    ///
    /// Streaming must also be enabled via
    /// ``configure(_:)`` with `StreamingConfig(enabled: true)` for delta
    /// events to arrive.
    nonisolated public func runStream(
        _ prompt: String,
        options: RunOptions = .init()
    ) -> AsyncThrowingStream<StreamEvent, Error> {
        return AsyncThrowingStream { continuation in
            let userOnDelta = options.onDelta
            let userOnStatus = options.onStatus

            var merged = options
            merged.onDelta = { text, turn in
                continuation.yield(.delta(text: text, turn: turn))
                await userOnDelta?(text, turn)
            }
            merged.onStatus = { ratio, count, limit in
                continuation.yield(.status(usageRatio: ratio, tokenCount: count, tokenLimit: limit))
                await userOnStatus?(ratio, count, limit)
            }

            let task = Task {
                do {
                    let result = try await self.run(prompt, options: merged)
                    continuation.yield(.end(result))
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    // MARK: registration

    @discardableResult
    public func registerTools(_ tools: Tool...) async throws -> Int {
        return try await registerTools(tools)
    }

    @discardableResult
    public func registerTools(_ tools: [Tool]) async throws -> Int {
        for def in tools { self.tools[def.name] = def }
        let payload: JSONValue = .object([
            "tools": .array(tools.map { $0.toWire() }),
        ])
        let raw = try await rpc.call(Methods.toolRegister, params: payload)
        return Int(raw["registered"].intValue ?? 0)
    }

    @discardableResult
    public func registerGuards(_ guards: Guard...) async throws -> Int {
        return try await registerGuards(guards)
    }

    @discardableResult
    public func registerGuards(_ guards: [Guard]) async throws -> Int {
        for def in guards { self.guards[def.name] = def }
        let payload: JSONValue = .object([
            "guards": .array(guards.map { $0.toWire() }),
        ])
        let raw = try await rpc.call(Methods.guardRegister, params: payload)
        return Int(raw["registered"].intValue ?? 0)
    }

    @discardableResult
    public func registerVerifiers(_ verifiers: Verifier...) async throws -> Int {
        return try await registerVerifiers(verifiers)
    }

    @discardableResult
    public func registerVerifiers(_ verifiers: [Verifier]) async throws -> Int {
        for def in verifiers { self.verifiers[def.name] = def }
        let payload: JSONValue = .object([
            "verifiers": .array(verifiers.map { $0.toWire() }),
        ])
        let raw = try await rpc.call(Methods.verifierRegister, params: payload)
        return Int(raw["registered"].intValue ?? 0)
    }

    @discardableResult
    public func registerMCP(_ options: MCPOptions) async throws -> [String] {
        var params: [String: JSONValue] = ["transport": .string(options.transport)]
        if let command = options.command { params["command"] = .string(command) }
        if let args = options.args, !args.isEmpty {
            params["args"] = .array(args.map { .string($0) })
        }
        if let env = options.env {
            var d: [String: JSONValue] = [:]
            for (k, v) in env { d[k] = .string(v) }
            params["env"] = .object(d)
        }
        if let url = options.url { params["url"] = .string(url) }
        let raw = try await rpc.call(Methods.mcpRegister, params: .object(params))
        if let arr = raw["tools"].arrayValue {
            return arr.compactMap { $0.stringValue }
        }
        return []
    }

    // MARK: handler implementations

    private func handleToolExecute(_ params: JSONValue) async -> JSONValue {
        let name = params["name"].stringValue ?? ""
        let args = params["args"] ?? .object([:])
        guard let def = tools[name] else {
            return .object([
                "content": .string("tool not found: \(name)"),
                "is_error": .bool(true),
            ])
        }
        do {
            let raw = try await def.handler(args)
            return coerceToolResult(raw).toWire()
        } catch {
            return .object([
                "content": .string("tool execution failed: \(error)"),
                "is_error": .bool(true),
            ])
        }
    }

    private func handleGuardExecute(_ params: JSONValue) async -> JSONValue {
        let name = params["name"].stringValue ?? ""
        let stage = params["stage"].stringValue ?? ""
        guard let def = guards[name], def.stage.rawValue == stage else {
            return .object([
                "decision": .string("deny"),
                "reason": .string("guard not found: \(name)/\(stage)"),
            ])
        }
        let ctx = GuardCallContext(
            input: params["input"].stringValue ?? "",
            toolName: params["tool_name"].stringValue ?? "",
            args: params["args"] ?? .object([:]),
            output: params["output"].stringValue ?? ""
        )
        do {
            let out = try await def.call(ctx)
            return .object([
                "decision": .string(out.decision.rawValue),
                "reason": .string(out.reason),
            ])
        } catch {
            return .object([
                "decision": .string("deny"),
                "reason": .string("guard error: \(error)"),
            ])
        }
    }

    private func handleVerifierExecute(_ params: JSONValue) async -> JSONValue {
        let name = params["name"].stringValue ?? ""
        guard let def = verifiers[name] else {
            return .object([
                "passed": .bool(false),
                "summary": .string("verifier not found: \(name)"),
            ])
        }
        let toolName = params["tool_name"].stringValue ?? ""
        let args = params["args"] ?? .object([:])
        let result = params["result"].stringValue ?? ""
        do {
            let out = try await def.call(toolName, args, result)
            return .object([
                "passed": .bool(out.passed),
                "summary": .string(out.summary),
            ])
        } catch {
            return .object([
                "passed": .bool(false),
                "summary": .string("verifier error: \(error)"),
            ])
        }
    }

    private func handleStreamDelta(_ params: JSONValue) async {
        guard let cb = streamCallback else { return }
        let text = params["text"].stringValue ?? ""
        let turn = Int(params["turn"].intValue ?? 0)
        await cb(text, turn)
    }

    private func handleContextStatus(_ params: JSONValue) async {
        guard let cb = statusCallback else { return }
        let ratio = params["usage_ratio"].doubleValue ?? 0
        let count = params["token_count"].intValue ?? 0
        let limit = params["token_limit"].intValue ?? 0
        await cb(ratio, count, limit)
    }

    // MARK: result parsing

    private func parseAgentResult(_ raw: JSONValue) -> AgentResult {
        let response = raw["response"].stringValue ?? ""
        let reason = raw["reason"].stringValue ?? ""
        let turns = Int(raw["turns"].intValue ?? 0)
        let usageRaw = raw["usage"] ?? .object([:])
        let usage = UsageInfo(
            promptTokens: usageRaw["prompt_tokens"].intValue ?? 0,
            completionTokens: usageRaw["completion_tokens"].intValue ?? 0,
            totalTokens: usageRaw["total_tokens"].intValue ?? 0
        )
        return AgentResult(response: response, reason: reason, turns: turns, usage: usage)
    }
}
