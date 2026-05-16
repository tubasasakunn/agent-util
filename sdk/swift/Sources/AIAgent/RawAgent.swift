import Foundation

// MARK: - JSON-RPCメソッド名

enum RpcMethod {
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
    static let contextSummarize = "context.summarize"
    static let judgeRegister = "judge.register"
    static let judgeEvaluate = "judge.evaluate"
    static let llmExecute = "llm.execute"
    static let sessionHistory = "session.history"
    static let sessionInject = "session.inject"

    // notifications
    static let streamDelta = "stream.delta"
    static let streamEnd = "stream.end"
    static let contextStatus = "context.status"
}

// MARK: - 結果型

public struct UsageInfo: Sendable, Equatable {
    public var promptTokens: Int
    public var completionTokens: Int
    public var totalTokens: Int

    public init(promptTokens: Int = 0, completionTokens: Int = 0, totalTokens: Int = 0) {
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

    public init(response: String, reason: String, turns: Int, usage: UsageInfo) {
        self.response = response
        self.reason = reason
        self.turns = turns
        self.usage = usage
    }

    /// 既知の終了理由。`AgentResult.reason` 文字列の判別を型で行うためのヘルパ。
    ///
    /// 文字列比較を避けたい場合に `result.terminationReason == .maxTurns` のように使う。
    /// 未知の文字列は `.other(raw:)` に落ちる。
    public enum Termination: Sendable, Equatable {
        case completed
        case maxTurns
        case userFixable
        case maxConsecutiveFailures
        case inputDenied
        case other(raw: String)

        init(_ raw: String) {
            switch raw {
            case "completed": self = .completed
            case "max_turns": self = .maxTurns
            case "user_fixable": self = .userFixable
            case "max_consecutive_failures": self = .maxConsecutiveFailures
            case "input_denied": self = .inputDenied
            default: self = .other(raw: raw)
            }
        }
    }

    public var terminationReason: Termination { Termination(reason) }

    /// 正常完了か (`reason == "completed"`)。
    public var isCompleted: Bool { terminationReason == .completed }

    /// ターン上限到達による打ち切り (`reason == "max_turns"`)。
    /// 旧挙動と異なり例外ではなく正常リターンに乗る。`response` は直近のアシスタント発話。
    public var isMaxTurns: Bool { terminationReason == .maxTurns }
}

public typealias StreamCallback = @Sendable (_ text: String, _ turn: Int) async -> Void
public typealias StatusCallback = @Sendable (_ usageRatio: Double, _ count: Int, _ limit: Int) async -> Void

/// `llm.execute` ハンドラ。OpenAI 互換 ChatRequest を表す `JSONValue` を受け取り、
/// OpenAI 互換 ChatResponse を表す `JSONValue` を返す。
/// `agent.configure(llm: LLMConfig(mode: .remote))` を指定すると、コア側のすべての
/// ChatCompletion 呼び出しがこのハンドラ経由になる。
public typealias LLMHandler = @Sendable (_ request: JSONValue) async throws -> JSONValue

// MARK: - RawAgent

/// 低レベルJSON-RPCクライアント。プロトコルに忠実な薄いラッパー。
///
/// 高レベルAOM API (`Agent`) は内部でこれを使用している。
public actor RawAgent {
    private let rpc: JsonRpcClient
    private let binaryPath: String
    private let env: [String: String]?
    private let cwd: String?

    private let tools = ToolRegistry()
    private let guards = GuardRegistry()
    private let verifiers = VerifierRegistry()
    private let judges = JudgeRegistry()

    private var streamCallback: StreamCallback?
    private var statusCallback: StatusCallback?
    private var llmHandler: LLMHandler?

    public init(
        binaryPath: String = "agent",
        env: [String: String]? = nil,
        cwd: String? = nil
    ) {
        self.rpc = JsonRpcClient()
        self.binaryPath = binaryPath
        self.env = env
        self.cwd = cwd
    }

    // MARK: - ライフサイクル

    public func start() async throws {
        try rpc.connectSubprocess(binaryPath, args: ["--rpc"], env: env, cwd: cwd)
        await wireHandlers()
    }

    public func close() async {
        await rpc.close()
    }

    public var stderrOutput: String {
        get async { await rpc.stderrOutput }
    }

    /// 内部用: easyレイヤーがsession.*RPCを呼ぶための直接アクセス。
    public func rawCall(_ method: String, params: JSONValue) async throws -> JSONValue {
        try await rpc.call(method, params: params)
    }

    // MARK: - configure

    @discardableResult
    public func configure(_ config: CoreAgentConfig) async throws -> [String] {
        let result = try await rpc.call(RpcMethod.agentConfigure, params: config.toParams())
        guard let applied = result["applied"]?.arrayValue else { return [] }
        return applied.compactMap { $0.stringValue }
    }

    // MARK: - run / abort

    public func run(
        _ prompt: String,
        maxTurns: Int? = nil,
        stream: StreamCallback? = nil,
        onStatus: StatusCallback? = nil,
        timeout: Duration? = nil
    ) async throws -> AgentResult {
        self.streamCallback = stream
        let prevStatus = self.statusCallback
        if let onStatus = onStatus { self.statusCallback = onStatus }
        defer {
            self.streamCallback = nil
            self.statusCallback = prevStatus
        }

        var params: [String: JSONValue] = ["prompt": .string(prompt)]
        if let maxTurns = maxTurns { params["max_turns"] = .int(Int64(maxTurns)) }

        let raw = try await rpc.call(RpcMethod.agentRun, params: .object(params), timeout: timeout)
        let usageJson = raw["usage"]
        let usage = UsageInfo(
            promptTokens: usageJson?["prompt_tokens"]?.intValue ?? 0,
            completionTokens: usageJson?["completion_tokens"]?.intValue ?? 0,
            totalTokens: usageJson?["total_tokens"]?.intValue ?? 0
        )
        return AgentResult(
            response: raw["response"]?.stringValue ?? "",
            reason: raw["reason"]?.stringValue ?? "",
            turns: raw["turns"]?.intValue ?? 0,
            usage: usage
        )
    }

    @discardableResult
    public func abort(reason: String = "") async throws -> Bool {
        var params: [String: JSONValue] = [:]
        if !reason.isEmpty { params["reason"] = .string(reason) }
        let raw = try await rpc.call(RpcMethod.agentAbort, params: .object(params))
        return raw["aborted"]?.boolValue ?? false
    }

    public func summarize() async throws -> String {
        let raw = try await rpc.call(RpcMethod.contextSummarize, params: .object([:]))
        return raw["summary"]?.stringValue ?? ""
    }

    // MARK: - 登録

    @discardableResult
    public func registerTools(_ tools: [Tool]) async throws -> [String] {
        await self.tools.registerAll(tools)
        let defs = tools.map { $0.toProtocolDict() }
        let params: JSONValue = .object(["tools": .array(defs)])
        _ = try await rpc.call(RpcMethod.toolRegister, params: params)
        return tools.map { $0.name }
    }

    @discardableResult
    public func registerGuards(_ guards: [GuardSpec]) async throws -> [String] {
        for g in guards { await self.guards.register(g) }
        let defs = guards.map { $0.toProtocolDict() }
        let params: JSONValue = .object(["guards": .array(defs)])
        _ = try await rpc.call(RpcMethod.guardRegister, params: params)
        return guards.map { $0.name }
    }

    @discardableResult
    public func registerVerifiers(_ verifiers: [Verifier]) async throws -> [String] {
        for v in verifiers { await self.verifiers.register(v) }
        let defs = verifiers.map { $0.toProtocolDict() }
        let params: JSONValue = .object(["verifiers": .array(defs)])
        _ = try await rpc.call(RpcMethod.verifierRegister, params: params)
        return verifiers.map { $0.name }
    }

    /// `llm.execute` ハンドラを登録 (`nil` でクリア)。
    ///
    /// `configure(LLMConfig(mode: .remote))` 適用中は、コアの ChatCompletion 呼び出しが
    /// すべてこのハンドラに転送される。ハンドラは OpenAI 互換 ChatRequest を受け取り、
    /// OpenAI 互換 ChatResponse (少なくとも `choices[0].message.content` または
    /// `choices[0].message.tool_calls` を含む) を返す必要がある。
    public func setLLMHandler(_ handler: LLMHandler?) {
        self.llmHandler = handler
    }

    public func registerJudge(name: String, handler: @escaping JudgeHandler) async throws {
        await self.judges.register(name: name, handler: handler)
        _ = try await rpc.call(
            RpcMethod.judgeRegister,
            params: .object(["name": .string(name)])
        )
    }

    @discardableResult
    public func registerMCP(
        command: String? = nil,
        args: [String] = [],
        env: [String: String]? = nil,
        transport: String = "stdio",
        url: String? = nil
    ) async throws -> [String] {
        var params: [String: JSONValue] = ["transport": .string(transport)]
        if let command = command { params["command"] = .string(command) }
        if !args.isEmpty { params["args"] = .array(args.map { .string($0) }) }
        if let env = env {
            var envJson: [String: JSONValue] = [:]
            for (k, v) in env { envJson[k] = .string(v) }
            params["env"] = .object(envJson)
        }
        if let url = url { params["url"] = .string(url) }
        let raw = try await rpc.call(RpcMethod.mcpRegister, params: .object(params))
        guard let tools = raw["tools"]?.arrayValue else { return [] }
        return tools.compactMap { $0.stringValue }
    }

    // MARK: - 内部ハンドラ配線

    private func wireHandlers() async {
        // ツール実行 (core -> wrapper)
        await rpc.setRequestHandler(RpcMethod.toolExecute) { [tools] params in
            let name = params["name"]?.stringValue ?? ""
            let args = params["args"] ?? .object([:])
            guard let tool = await tools.get(name) else {
                let registered = await tools.names()
                return .object([
                    "content": .string("tool not found: \(name) (registered: \(registered))"),
                    "is_error": .bool(true),
                ])
            }
            do {
                let result = try await tool.handler(args)
                await tools.recordSuccess(name)
                return coerceToolResult(result)
            } catch {
                await tools.recordError(name)
                return .object([
                    "content": .string("tool execution failed: \(error)"),
                    "is_error": .bool(true),
                ])
            }
        }

        // ガード実行
        await rpc.setRequestHandler(RpcMethod.guardExecute) { [guards] params in
            let name = params["name"]?.stringValue ?? ""
            let stage = params["stage"]?.stringValue ?? ""
            guard let g = await guards.get(name: name, stage: stage) else {
                let registered = await guards.registered()
                return .object([
                    "decision": .string("deny"),
                    "reason": .string("guard not found: \(name)/\(stage) (registered: \(registered))"),
                ])
            }
            let input = GuardInput(
                input: params["input"]?.stringValue ?? "",
                toolName: params["tool_name"]?.stringValue ?? "",
                args: params["args"] ?? .object([:]),
                output: params["output"]?.stringValue ?? ""
            )
            do {
                let outcome = try await g.handler(input)
                return .object([
                    "decision": .string(outcome.decision.rawValue),
                    "reason": .string(outcome.reason),
                ])
            } catch {
                return .object([
                    "decision": .string("deny"),
                    "reason": .string("guard error: \(error)"),
                ])
            }
        }

        // ベリファイア実行
        await rpc.setRequestHandler(RpcMethod.verifierExecute) { [verifiers] params in
            let name = params["name"]?.stringValue ?? ""
            guard let v = await verifiers.get(name) else {
                let registered = await verifiers.registered()
                return .object([
                    "passed": .bool(false),
                    "summary": .string("verifier not found: \(name) (registered: \(registered))"),
                ])
            }
            do {
                let outcome = try await v.handler(
                    params["tool_name"]?.stringValue ?? "",
                    params["args"] ?? .object([:]),
                    params["result"]?.stringValue ?? ""
                )
                return .object([
                    "passed": .bool(outcome.passed),
                    "summary": .string(outcome.summary),
                ])
            } catch {
                return .object([
                    "passed": .bool(false),
                    "summary": .string("verifier error: \(error)"),
                ])
            }
        }

        // ジャッジ評価
        await rpc.setRequestHandler(RpcMethod.judgeEvaluate) { [judges] params in
            let name = params["name"]?.stringValue ?? ""
            guard let handler = await judges.get(name) else {
                let registered = await judges.registered()
                return .object([
                    "terminate": .bool(false),
                    "reason": .string("judge not found: \(name) (registered: \(registered))"),
                ])
            }
            do {
                let outcome = try await handler(
                    params["response"]?.stringValue ?? "",
                    params["turn"]?.intValue ?? 0
                )
                return .object([
                    "terminate": .bool(outcome.terminate),
                    "reason": .string(outcome.reason),
                ])
            } catch {
                return .object([
                    "terminate": .bool(false),
                    "reason": .string("judge error: \(error)"),
                ])
            }
        }

        // LLM 委譲 (core -> wrapper)
        await rpc.setRequestHandler(RpcMethod.llmExecute) { [weak self] params in
            guard let self = self else {
                return .object([:])
            }
            return try await self.handleLLMExecute(params)
        }

        // ストリーム通知
        await rpc.setNotificationHandler(RpcMethod.streamDelta) { [weak self] params in
            await self?.handleStreamDelta(params)
        }
        await rpc.setNotificationHandler(RpcMethod.streamEnd) { _ in }
        await rpc.setNotificationHandler(RpcMethod.contextStatus) { [weak self] params in
            await self?.handleContextStatus(params)
        }
    }

    private func handleLLMExecute(_ params: JSONValue) async throws -> JSONValue {
        guard let handler = llmHandler else {
            throw AgentError(
                "received llm.execute but no handler is registered. " +
                "Call setLLMHandler(_:) before configuring llm.mode=.remote."
            )
        }
        let request = params["request"] ?? .object([:])
        let response = try await handler(request)
        if case .object = response {} else {
            throw AgentError(
                "llm handler must return a JSON object (OpenAI-style ChatResponse)"
            )
        }
        return .object(["response": response])
    }

    private func handleStreamDelta(_ params: JSONValue) async {
        guard let cb = streamCallback else { return }
        let text = params["text"]?.stringValue ?? ""
        let turn = params["turn"]?.intValue ?? 0
        await cb(text, turn)
    }

    private func handleContextStatus(_ params: JSONValue) async {
        guard let cb = statusCallback else { return }
        let ratio = params["usage_ratio"]?.doubleValue ?? 0.0
        let count = params["token_count"]?.intValue ?? 0
        let limit = params["token_limit"]?.intValue ?? 0
        await cb(ratio, count, limit)
    }
}
