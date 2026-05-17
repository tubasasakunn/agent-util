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
    static let serverInfo = "server.info"

    // notifications
    static let streamDelta = "stream.delta"
    static let streamEnd = "stream.end"
    static let contextStatus = "context.status"
}

// MARK: - サーバー情報 (server.info)

/// `server.info` の応答。バイナリのバージョンと対応メソッド/機能フラグ。
///
/// SDK は `RawAgent.start()` でこれを自動取得し、`AgentConfig.expectedLibraryVersion`
/// が指定されていれば比較する (E3)。
public struct ServerInfo: Sendable, Equatable {
    public let libraryVersion: String
    public let protocolVersion: String
    public let methods: [String]
    public let features: [String: Bool]

    public init(libraryVersion: String, protocolVersion: String, methods: [String], features: [String: Bool]) {
        self.libraryVersion = libraryVersion
        self.protocolVersion = protocolVersion
        self.methods = methods
        self.features = features
    }

    /// 機能対応の早見ヘルパ。例: `info.supports("llm_execute")`
    public func supports(_ feature: String) -> Bool { features[feature] ?? false }
}

/// SDK が想定している ai-agent ライブラリのバージョン。
/// バイナリ側 `protocol.LibraryVersion` と一致するべき値。
public let aiAgentSDKLibraryVersion = "0.2.1"

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

// MARK: - 観測性レコード (G1/G2/G4)

/// ツール 1 回分の呼び出し記録。`AgentResult.toolCalls` で取得できる。
public struct ToolCallRecord: Sendable, Equatable {
    public let name: String
    public let isError: Bool
    public let argsJSON: String

    public init(name: String, isError: Bool, argsJSON: String) {
        self.name = name
        self.isError = isError
        self.argsJSON = argsJSON
    }
}

/// ガード/ベリファイア/ジャッジ 1 回分の発火記録。`AgentResult.guardFires` で取得できる。
public struct GuardFireRecord: Sendable, Equatable {
    /// "guard.input" / "guard.tool_call" / "guard.output" / "verifier" / "judge"
    public let kind: String
    public let name: String
    /// "allow"/"deny"/"tripwire" (guard) or "pass"/"fail" (verifier) or "continue"/"done" (judge)
    public let decision: String
    public let reason: String

    public init(kind: String, name: String, decision: String, reason: String) {
        self.kind = kind
        self.name = name
        self.decision = decision
        self.reason = reason
    }
}

// MARK: - 実行フェーズ (G3)

/// 実行中のおおまかなフェーズ。`onPhase` コールバックで通知される。
public enum AgentPhase: String, Sendable {
    /// ルーターがツール選択中
    case routing
    /// ツール実行中
    case tool
    /// ガード判定中 (入力/ツール呼出/出力)
    case guarding
    /// ベリファイア実行中
    case verifying
    /// ジャッジ評価中
    case judging
    /// 自然言語応答生成中
    case generating
}

public struct AgentResult: Sendable, Equatable {
    public var response: String
    public var reason: String
    public var turns: Int
    public var usage: UsageInfo
    /// 今回の run で実行されたツール呼び出し履歴 (G1/G2)。
    /// `toolCalls.count` で総呼び出し数、`toolCalls.filter { $0.name == "x" }.count`
    /// で個別ツールの呼び出し回数が得られる。
    public var toolCalls: [ToolCallRecord]
    /// 今回の run で発火したガード/ベリファイア/ジャッジ履歴 (G4)。
    public var guardFires: [GuardFireRecord]

    public init(
        response: String,
        reason: String,
        turns: Int,
        usage: UsageInfo,
        toolCalls: [ToolCallRecord] = [],
        guardFires: [GuardFireRecord] = []
    ) {
        self.response = response
        self.reason = reason
        self.turns = turns
        self.usage = usage
        self.toolCalls = toolCalls
        self.guardFires = guardFires
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
/// 現在実行中のフェーズを受け取るコールバック (G3)。
/// 例: `.routing` → `.tool` → `.guarding` → `.generating`。
public typealias PhaseCallback = @Sendable (_ phase: AgentPhase) async -> Void

/// 1 回の run() の間にツール/ガード等の呼び出しを記録する actor。
///
/// RawAgent.run の冒頭でリセットされ、終了時に AgentResult に詰めて返される。
actor RunObserver {
    private(set) var toolCalls: [ToolCallRecord] = []
    private(set) var guardFires: [GuardFireRecord] = []

    func reset() {
        toolCalls.removeAll()
        guardFires.removeAll()
    }

    func recordToolCall(_ rec: ToolCallRecord) { toolCalls.append(rec) }
    func recordGuardFire(_ rec: GuardFireRecord) { guardFires.append(rec) }

    func snapshot() -> (toolCalls: [ToolCallRecord], guardFires: [GuardFireRecord]) {
        (toolCalls, guardFires)
    }
}

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
    private var phaseCallback: PhaseCallback?
    private var llmHandler: LLMHandler?
    private let observer = RunObserver()

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

    /// バイナリの `server.info` を取得する。E3 のハンドシェイクで使う。
    /// 旧バイナリ (server.info 未実装) では `method not found (-32601)` で
    /// 失敗するので、呼び出し元はそれを「server.info 非対応バイナリ」と
    /// 解釈してフォールバックする。
    public func serverInfo() async throws -> ServerInfo {
        let raw = try await rpc.call(RpcMethod.serverInfo, params: .object([:]))
        return ServerInfo(
            libraryVersion: raw["library_version"]?.stringValue ?? "",
            protocolVersion: raw["protocol_version"]?.stringValue ?? "",
            methods: raw["methods"]?.arrayValue?.compactMap { $0.stringValue } ?? [],
            features: (raw["features"]?.objectValue ?? [:])
                .reduce(into: [String: Bool]()) { acc, pair in
                    acc[pair.key] = pair.value.boolValue ?? false
                }
        )
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
        onPhase: PhaseCallback? = nil,
        timeout: Duration? = nil
    ) async throws -> AgentResult {
        self.streamCallback = stream
        let prevStatus = self.statusCallback
        let prevPhase = self.phaseCallback
        if let onStatus = onStatus { self.statusCallback = onStatus }
        if let onPhase = onPhase { self.phaseCallback = onPhase }
        // 1 回の run につき観測レコードをリセットする
        await observer.reset()
        defer {
            self.streamCallback = nil
            self.statusCallback = prevStatus
            self.phaseCallback = prevPhase
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
        let snap = await observer.snapshot()
        return AgentResult(
            response: raw["response"]?.stringValue ?? "",
            reason: raw["reason"]?.stringValue ?? "",
            turns: raw["turns"]?.intValue ?? 0,
            usage: usage,
            toolCalls: snap.toolCalls,
            guardFires: snap.guardFires
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
        await rpc.setRequestHandler(RpcMethod.toolExecute) { [weak self, tools] params in
            let name = params["name"]?.stringValue ?? ""
            let args = params["args"] ?? .object([:])
            await self?.emitPhase(.tool)
            guard let tool = await tools.get(name) else {
                let registered = await tools.names()
                await self?.recordToolCall(name: name, isError: true, args: args)
                return .object([
                    "content": .string("tool not found: \(name) (registered: \(registered))"),
                    "is_error": .bool(true),
                ])
            }
            do {
                let result = try await tool.handler(args)
                await tools.recordSuccess(name)
                let outJSON = coerceToolResult(result)
                let isErr = outJSON["is_error"]?.boolValue ?? false
                await self?.recordToolCall(name: name, isError: isErr, args: args)
                return outJSON
            } catch {
                await tools.recordError(name)
                await self?.recordToolCall(name: name, isError: true, args: args)
                return .object([
                    "content": .string("tool execution failed: \(error)"),
                    "is_error": .bool(true),
                ])
            }
        }

        // ガード実行
        await rpc.setRequestHandler(RpcMethod.guardExecute) { [weak self, guards] params in
            let name = params["name"]?.stringValue ?? ""
            let stage = params["stage"]?.stringValue ?? ""
            await self?.emitPhase(.guarding)
            guard let g = await guards.get(name: name, stage: stage) else {
                let registered = await guards.registered()
                let reason = "guard not found: \(name)/\(stage) (registered: \(registered))"
                await self?.recordGuardFire(kind: "guard.\(stage)", name: name, decision: "deny", reason: reason)
                return .object([
                    "decision": .string("deny"),
                    "reason": .string(reason),
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
                await self?.recordGuardFire(
                    kind: "guard.\(stage)",
                    name: name,
                    decision: outcome.decision.rawValue,
                    reason: outcome.reason
                )
                return .object([
                    "decision": .string(outcome.decision.rawValue),
                    "reason": .string(outcome.reason),
                ])
            } catch {
                let reason = "guard error: \(error)"
                await self?.recordGuardFire(kind: "guard.\(stage)", name: name, decision: "deny", reason: reason)
                return .object([
                    "decision": .string("deny"),
                    "reason": .string(reason),
                ])
            }
        }

        // ベリファイア実行
        await rpc.setRequestHandler(RpcMethod.verifierExecute) { [weak self, verifiers] params in
            let name = params["name"]?.stringValue ?? ""
            await self?.emitPhase(.verifying)
            guard let v = await verifiers.get(name) else {
                let registered = await verifiers.registered()
                let summary = "verifier not found: \(name) (registered: \(registered))"
                await self?.recordGuardFire(kind: "verifier", name: name, decision: "fail", reason: summary)
                return .object([
                    "passed": .bool(false),
                    "summary": .string(summary),
                ])
            }
            do {
                let outcome = try await v.handler(
                    params["tool_name"]?.stringValue ?? "",
                    params["args"] ?? .object([:]),
                    params["result"]?.stringValue ?? ""
                )
                await self?.recordGuardFire(
                    kind: "verifier",
                    name: name,
                    decision: outcome.passed ? "pass" : "fail",
                    reason: outcome.summary
                )
                return .object([
                    "passed": .bool(outcome.passed),
                    "summary": .string(outcome.summary),
                ])
            } catch {
                let summary = "verifier error: \(error)"
                await self?.recordGuardFire(kind: "verifier", name: name, decision: "fail", reason: summary)
                return .object([
                    "passed": .bool(false),
                    "summary": .string(summary),
                ])
            }
        }

        // ジャッジ評価
        await rpc.setRequestHandler(RpcMethod.judgeEvaluate) { [weak self, judges] params in
            let name = params["name"]?.stringValue ?? ""
            await self?.emitPhase(.judging)
            guard let handler = await judges.get(name) else {
                let registered = await judges.registered()
                let reason = "judge not found: \(name) (registered: \(registered))"
                await self?.recordGuardFire(kind: "judge", name: name, decision: "continue", reason: reason)
                return .object([
                    "terminate": .bool(false),
                    "reason": .string(reason),
                ])
            }
            do {
                let outcome = try await handler(
                    params["response"]?.stringValue ?? "",
                    params["turn"]?.intValue ?? 0
                )
                await self?.recordGuardFire(
                    kind: "judge",
                    name: name,
                    decision: outcome.terminate ? "done" : "continue",
                    reason: outcome.reason
                )
                return .object([
                    "terminate": .bool(outcome.terminate),
                    "reason": .string(outcome.reason),
                ])
            } catch {
                let reason = "judge error: \(error)"
                await self?.recordGuardFire(kind: "judge", name: name, decision: "continue", reason: reason)
                return .object([
                    "terminate": .bool(false),
                    "reason": .string(reason),
                ])
            }
        }

        // LLM 委譲 (core -> wrapper)
        await rpc.setRequestHandler(RpcMethod.llmExecute) { [weak self] params in
            guard let self = self else {
                return .object([:])
            }
            // ChatRequest が response_format=json_object のときはルーターフェーズ、
            // そうでなければ自然言語生成フェーズ。
            let request = params["request"] ?? .object([:])
            let isRouter = request["response_format"] != nil
                && (request["response_format"]?["type"]?.stringValue == "json_object")
            await self.emitPhase(isRouter ? .routing : .generating)
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

    // MARK: - 観測ヘルパ (G1〜G4)

    /// 観測レコード集計用。引数 JSON を文字列に正規化して記録する。
    func recordToolCall(name: String, isError: Bool, args: JSONValue) async {
        let argsStr = Self.compactJSONString(args)
        await observer.recordToolCall(
            ToolCallRecord(name: name, isError: isError, argsJSON: argsStr)
        )
    }

    func recordGuardFire(kind: String, name: String, decision: String, reason: String) async {
        await observer.recordGuardFire(
            GuardFireRecord(kind: kind, name: name, decision: decision, reason: reason)
        )
    }

    /// 直近の `run()` の観測スナップショット (デバッグ用)。
    public func observerSnapshot() async -> (toolCalls: [ToolCallRecord], guardFires: [GuardFireRecord]) {
        await observer.snapshot()
    }

    func emitPhase(_ phase: AgentPhase) async {
        guard let cb = phaseCallback else { return }
        await cb(phase)
    }

    private static func compactJSONString(_ value: JSONValue) -> String {
        guard
            let data = try? JSONSerialization.data(
                withJSONObject: value.toRaw(),
                options: [.fragmentsAllowed]
            ),
            let s = String(data: data, encoding: .utf8)
        else { return "{}" }
        return s
    }
}
