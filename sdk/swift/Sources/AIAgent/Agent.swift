import Foundation

// MARK: - 高レベルAgentConfig (バイナリ設定 + エージェント挙動を集約)

/// 高レベル `Agent` 用の統合設定。
///
/// 必須:
///   - `binary`: コンパイル済みエージェントバイナリのパス (例: `"./agent"`)
///   - `env`:    LLM接続に必要な環境変数
///                 `["SLLM_ENDPOINT": "http://localhost:8080/v1/chat/completions",
///                  "SLLM_API_KEY": "sk-xxx"]`
///
/// クイックスタート:
///
///     let config = AgentConfig(
///         binary: "./agent",
///         env: ["SLLM_ENDPOINT": "http://localhost:8080/v1/chat/completions",
///               "SLLM_API_KEY": "sk-xxx"],
///         systemPrompt: "あなたは親切なアシスタントです。",
///         maxTurns: 20
///     )
///     let agent = Agent(config: config)
///     try await agent.start()
///     let reply = try await agent.input("こんにちは！")
///     await agent.close()
public struct AgentConfig: Sendable {
    // バイナリ / プロセス設定
    public var binary: String
    public var env: [String: String]?
    public var cwd: String?

    /// バイナリのバージョン検証ポリシー (E3)。
    public enum VersionCheckPolicy: Sendable, Equatable {
        /// バージョンが完全一致しないと start() が失敗する。
        case strict
        /// 不一致は警告のみ (stderr に出力)。デフォルト。
        case warn
        /// server.info ハンドシェイク自体をスキップする。
        case skip
    }

    /// バイナリのバージョン整合性チェック。デフォルトは `.warn`。
    public var versionCheck: VersionCheckPolicy = .warn

    // 基本エージェント挙動
    public var systemPrompt: String?
    public var maxTurns: Int?
    public var tokenLimit: Int?
    public var workDir: String?

    // サブエージェント
    public var delegate: DelegateConfig?
    public var coordinator: CoordinatorConfig?

    // コンテキスト / 安全性
    public var compaction: CompactionConfig?
    public var permission: PermissionConfig?
    public var guards: GuardsConfig?
    public var verify: VerifyConfig?
    public var toolScope: ToolScopeConfig?
    public var reminder: ReminderConfig?
    public var streaming: StreamingConfig?

    // ループ / LLM拡張
    public var loop: LoopConfig?
    public var router: RouterConfig?
    public var judge: JudgeConfig?

    /// メインLLMドライバの設定。`LLMConfig(mode: .remote)` でラッパー委譲を有効化。
    /// `llmHandler` を指定すると、未設定でも自動的に `mode: .remote` が適用される。
    public var llm: LLMConfig?

    /// `llm.execute` ハンドラ。指定すると `llm.mode=.remote` が自動で有効になり、
    /// すべての LLM 呼び出しがこの関数経由になる。OpenAI 互換 ChatRequest を受け取り、
    /// OpenAI 互換 ChatResponse を返す。任意の API 形式 (Anthropic / Bedrock /
    /// ollama / mock 等) への変換ポイント。
    public var llmHandler: LLMHandler?

    // MARK: - カスタムハンドラ (B1〜B4: start() 内で自動 register される)

    /// `Agent.start()` 内で `configure` より前に自動 register されるカスタムツール。
    ///
    /// 旧 API では `agent.start()` の後で `agent.registerTools(...)` する必要があり、
    /// しかも `AgentConfig.toolScope` で名前指定すると "unknown" で失敗していた。
    /// これに `[Tool]` を渡せば、内部で「subprocess 起動 → register → configure」の
    /// 順に処理されるため、`toolScope.includeAlways: ["myTool"]` 等が機能する。
    public var customTools: [Tool]?

    /// `Agent.start()` 内で `configure` より前に自動 register されるカスタムガード。
    ///
    /// 例: `customGuards: [GuardSpec.input(name: "no_secrets") { ... }]` と
    ///     `guards: GuardsConfig(input: ["no_secrets"])` を併用すれば、
    /// AgentConfig 1 つで「定義」と「有効化」が完結する。
    public var customGuards: [GuardSpec]?

    /// `Agent.start()` 内で `configure` より前に自動 register されるカスタム Verifier。
    public var customVerifiers: [Verifier]?

    /// `Agent.start()` 内で `configure` より前に自動 register されるカスタム Judge。
    /// キーはジャッジ名、値はハンドラ。`judge: JudgeConfig(name: "...")` と対応させる。
    public var customJudges: [String: JudgeHandler]?

    public init(
        binary: String = "agent",
        env: [String: String]? = nil,
        cwd: String? = nil,
        systemPrompt: String? = nil,
        maxTurns: Int? = nil,
        tokenLimit: Int? = nil,
        workDir: String? = nil,
        delegate: DelegateConfig? = nil,
        coordinator: CoordinatorConfig? = nil,
        compaction: CompactionConfig? = nil,
        permission: PermissionConfig? = nil,
        guards: GuardsConfig? = nil,
        verify: VerifyConfig? = nil,
        toolScope: ToolScopeConfig? = nil,
        reminder: ReminderConfig? = nil,
        streaming: StreamingConfig? = nil,
        loop: LoopConfig? = nil,
        router: RouterConfig? = nil,
        judge: JudgeConfig? = nil,
        llm: LLMConfig? = nil,
        llmHandler: LLMHandler? = nil,
        customTools: [Tool]? = nil,
        customGuards: [GuardSpec]? = nil,
        customVerifiers: [Verifier]? = nil,
        customJudges: [String: JudgeHandler]? = nil,
        versionCheck: VersionCheckPolicy = .warn
    ) {
        precondition(!binary.isEmpty, "AgentConfig.binary must be a non-empty string")
        precondition(maxTurns.map { $0 > 0 } ?? true, "AgentConfig.maxTurns must be positive")
        self.binary = binary
        self.env = env
        self.cwd = cwd
        self.systemPrompt = systemPrompt
        self.maxTurns = maxTurns
        self.tokenLimit = tokenLimit
        self.workDir = workDir
        self.delegate = delegate
        self.coordinator = coordinator
        self.compaction = compaction
        self.permission = permission
        self.guards = guards
        self.verify = verify
        self.toolScope = toolScope
        self.reminder = reminder
        self.streaming = streaming
        self.loop = loop
        self.router = router
        self.judge = judge
        self.llm = llm
        self.llmHandler = llmHandler
        self.customTools = customTools
        self.customGuards = customGuards
        self.customVerifiers = customVerifiers
        self.customJudges = customJudges
        self.versionCheck = versionCheck
    }

    /// JSON-RPC に送信される直前の `agent.configure` パラメータを
    /// 整形済み JSON 文字列で返す (D5)。
    ///
    /// camelCase で書いた設定が裏でどう snake_case に変換されるか、
    /// どのフィールドが省略されるかを目視確認できる。デバッグ専用。
    ///
    /// ```swift
    /// print(config.debugDump())
    /// ```
    public func debugDump() -> String {
        let coreParams = toCoreConfig().toParams()
        guard
            let data = try? JSONSerialization.data(
                withJSONObject: coreParams.toRaw(),
                options: [.prettyPrinted, .sortedKeys]
            ),
            let s = String(data: data, encoding: .utf8)
        else {
            return "<debugDump: serialization failed>"
        }
        return s
    }

    func toCoreConfig() -> CoreAgentConfig {
        // llmHandler が指定されていて llm が未指定なら自動で mode: .remote
        let effectiveLLM = llm ?? (llmHandler != nil ? LLMConfig(mode: .remote) : nil)
        return CoreAgentConfig(
            maxTurns: maxTurns,
            systemPrompt: systemPrompt,
            tokenLimit: tokenLimit,
            workDir: workDir,
            delegate: delegate,
            coordinator: coordinator,
            compaction: compaction,
            permission: permission,
            guards: guards,
            verify: verify,
            toolScope: toolScope,
            reminder: reminder,
            streaming: streaming,
            loop: loop,
            router: router,
            judge: judge,
            llm: effectiveLLM
        )
    }
}

// MARK: - Agent (高レベルAOM)

/// 高レベルエージェント。
///
/// **会話入力:**
///   - `input(_:)` : 入力送信・会話蓄積。`onDelta` でストリーミングも可能
///   - `stream(_:)` : `AsyncThrowingStream<String, Error>` でトークンを逐次配信
///
/// **コンテキスト操作:**
///   - `context()` : LLMによる会話要約
///   - `fork()` : 子エージェント (会話履歴をコピー)
///   - `add(_:)` : 他エージェントの履歴を末尾に追加
///   - `addSummary(_:)` : 他エージェントの要約を注入
///   - `branch(from:)` : n番目以降のメッセージで新エージェントを作成
///
/// **シリアライズ:**
///   - `export()` : 会話状態を `JSONValue` としてシリアライズ
///   - `importHistory(_:)` : `export()` データから会話状態を復元
///
/// **登録:**
///   - `registerTools(_:)` : `Tool` の登録
///   - `registerGuards(_:)` : `GuardSpec` の登録
///   - `registerVerifiers(_:)` : `Verifier` の登録
///   - `registerJudge(_:_:)` : ゴール達成判定器の登録
///   - `registerMCP(...)` : MCP設定の登録
///
/// **ユーティリティ:**
///   - `search(_:)` : 過去の会話をキーワード検索 (RAG)
///   - `batch(_:)` : 複数プロンプトを並列処理
///   - `improveTool(_:_:)` : LLMでツールの説明を改善
public actor Agent {
    public let config: AgentConfig
    public let name: String

    private var core: RawAgent?
    private var started: Bool = false
    private let index = MessageIndex()
    private var registeredTools: [String: Tool] = [:]

    public init(config: AgentConfig, name: String? = nil) {
        self.config = config
        self.name = name ?? "agent-\(UUID().uuidString.prefix(8))"
    }

    // MARK: - ライフサイクル

    @discardableResult
    public func start() async throws -> Agent {
        if started { return self }
        let raw = RawAgent(binaryPath: config.binary, env: config.env, cwd: config.cwd)
        try await raw.start()

        // E3: バイナリのバージョン互換性をハンドシェイクで検証する。
        if config.versionCheck != .skip {
            try await Self.performHandshake(raw: raw, policy: config.versionCheck)
        }

        // configure より前に llm.execute ハンドラを差し込む
        // (configure 直後に LLM 呼び出しが走る可能性があるため)
        if let handler = config.llmHandler {
            await raw.setLLMHandler(handler)
        }

        // B1〜B4: AgentConfig.custom* に積まれたハンドラを configure より前に
        // 全部 register する。これによって AgentConfig.guards / verify / judge
        // で名前指定したカスタムガード等が unknown にならない。
        if let tools = config.customTools, !tools.isEmpty {
            _ = try await raw.registerTools(tools)
            for t in tools { registeredTools[t.name] = t }
        }
        if let guards = config.customGuards, !guards.isEmpty {
            _ = try await raw.registerGuards(guards)
        }
        if let verifiers = config.customVerifiers, !verifiers.isEmpty {
            _ = try await raw.registerVerifiers(verifiers)
        }
        if let judges = config.customJudges {
            for (name, handler) in judges {
                try await raw.registerJudge(name: name, handler: handler)
            }
        }

        // すべて register し終わってから configure する。
        // configure 内で参照される guard/verifier/judge の名前は既知になっている。
        _ = try await raw.configure(config.toCoreConfig())

        self.core = raw
        self.started = true
        return self
    }

    /// `server.info` ハンドシェイクを実行し、ポリシーに従って判定する。
    /// - strict: 不一致または server.info 未対応で AgentError を throw する
    /// - warn  : stderr に警告を出して続行
    private static func performHandshake(
        raw: RawAgent,
        policy: AgentConfig.VersionCheckPolicy
    ) async throws {
        do {
            let info = try await raw.serverInfo()
            if info.libraryVersion != aiAgentSDKLibraryVersion {
                let msg = """
                [ai-agent] version mismatch: SDK=\(aiAgentSDKLibraryVersion) binary=\(info.libraryVersion). \
                Some features (e.g. llm.execute) may behave unexpectedly. \
                Rebuild the agent binary with the matching version.
                """
                if policy == .strict {
                    throw AgentError(msg)
                }
                FileHandle.standardError.write(Data((msg + "\n").utf8))
            }
        } catch let err as AgentError where err.code == -32601 {
            // 旧バイナリは server.info 未実装。
            let msg = """
            [ai-agent] binary does not implement server.info (likely older than \(aiAgentSDKLibraryVersion)). \
            Features such as llm.execute may be unavailable. \
            Rebuild the agent binary from this repository.
            """
            if policy == .strict {
                throw AgentError(msg)
            }
            FileHandle.standardError.write(Data((msg + "\n").utf8))
        } catch let err as AgentError {
            // strict ですでに throw 済みの場合は再 throw、それ以外は警告。
            if policy == .strict { throw err }
            let msg = "[ai-agent] handshake failed: \(err.localizedDescription)\n"
            FileHandle.standardError.write(Data(msg.utf8))
        } catch {
            if policy == .strict { throw AgentError("handshake failed: \(error)") }
            let msg = "[ai-agent] handshake failed: \(error)\n"
            FileHandle.standardError.write(Data(msg.utf8))
        }
    }

    public func close() async {
        guard let raw = core else { return }
        await raw.close()
        self.core = nil
        self.started = false
    }

    private func ensureStarted() async throws -> RawAgent {
        if let core = core { return core }
        _ = try await start()
        return core!
    }

    public var stderrOutput: String {
        get async {
            guard let core = core else { return "" }
            return await core.stderrOutput
        }
    }

    // MARK: - 登録

    @discardableResult
    public func registerTools(_ tools: Tool...) async throws -> [String] {
        try await registerTools(tools)
    }

    @discardableResult
    public func registerTools(_ tools: [Tool]) async throws -> [String] {
        let core = try await ensureStarted()
        for t in tools { registeredTools[t.name] = t }
        return try await core.registerTools(tools)
    }

    @discardableResult
    public func registerGuards(_ guards: GuardSpec...) async throws -> [String] {
        try await registerGuards(guards)
    }

    @discardableResult
    public func registerGuards(_ guards: [GuardSpec]) async throws -> [String] {
        let core = try await ensureStarted()
        return try await core.registerGuards(guards)
    }

    @discardableResult
    public func registerVerifiers(_ verifiers: Verifier...) async throws -> [String] {
        try await registerVerifiers(verifiers)
    }

    @discardableResult
    public func registerVerifiers(_ verifiers: [Verifier]) async throws -> [String] {
        let core = try await ensureStarted()
        return try await core.registerVerifiers(verifiers)
    }

    public func registerJudge(_ name: String, _ handler: @escaping JudgeHandler) async throws {
        let core = try await ensureStarted()
        try await core.registerJudge(name: name, handler: handler)
    }

    @discardableResult
    public func registerMCP(
        command: String? = nil,
        args: [String] = [],
        env: [String: String]? = nil,
        transport: String = "stdio",
        url: String? = nil
    ) async throws -> [String] {
        let core = try await ensureStarted()
        return try await core.registerMCP(
            command: command, args: args, env: env, transport: transport, url: url
        )
    }

    @discardableResult
    public func registerSkills(_ skillDir: String) async throws -> [String] {
        let configs = loadSkillConfigs(skillDir)
        var registered: [String] = []
        for cfg in configs {
            do {
                let tools = try await registerSingleMCP(cfg)
                registered.append(contentsOf: tools)
            } catch {
                // ログのみで続行
            }
        }
        return registered
    }

    @discardableResult
    public func registerMCP(configs: [[String: JSONValue]]) async throws -> [String] {
        var all: [String] = []
        for c in configs {
            let tools = try await registerSingleMCP(c)
            all.append(contentsOf: tools)
        }
        return all
    }

    private func registerSingleMCP(_ cfg: [String: JSONValue]) async throws -> [String] {
        let transport = cfg["transport"]?.stringValue ?? "stdio"
        if transport == "stdio" {
            let command = cfg["command"]?.stringValue
            let args = cfg["args"]?.arrayValue?.compactMap { $0.stringValue } ?? []
            var env: [String: String]? = nil
            if let envJson = cfg["env"]?.objectValue {
                var dict: [String: String] = [:]
                for (k, v) in envJson { if let s = v.stringValue { dict[k] = s } }
                env = dict
            }
            return try await registerMCP(command: command, args: args, env: env, transport: transport)
        } else {
            let url = cfg["url"]?.stringValue
            return try await registerMCP(transport: transport, url: url)
        }
    }

    // MARK: - 会話入力

    @discardableResult
    public func input(
        _ prompt: String,
        maxTurns: Int? = nil,
        onDelta: StreamCallback? = nil,
        onStatus: StatusCallback? = nil,
        onStatusEvent: StatusEventCallback? = nil,
        onPhase: PhaseCallback? = nil,
        timeout: Duration? = nil
    ) async throws -> String {
        let result = try await inputVerbose(
            prompt,
            maxTurns: maxTurns,
            onDelta: onDelta,
            onStatus: onStatus,
            onStatusEvent: onStatusEvent,
            onPhase: onPhase,
            timeout: timeout
        )
        return result.response
    }

    public func inputVerbose(
        _ prompt: String,
        maxTurns: Int? = nil,
        onDelta: StreamCallback? = nil,
        onStatus: StatusCallback? = nil,
        onStatusEvent: StatusEventCallback? = nil,
        onPhase: PhaseCallback? = nil,
        timeout: Duration? = nil
    ) async throws -> AgentResult {
        let core = try await ensureStarted()
        let result = try await core.run(
            prompt,
            maxTurns: maxTurns,
            stream: onDelta,
            onStatus: onStatus,
            onStatusEvent: onStatusEvent,
            onPhase: onPhase,
            timeout: timeout
        )
        await index.add(role: "user", content: prompt)
        await index.add(role: "assistant", content: result.response)
        return result
    }

    // MARK: - コンテキスト要約

    public func context() async throws -> String {
        let core = try await ensureStarted()
        return try await core.summarize()
    }

    // MARK: - フォーク / 履歴転送

    public func fork(name: String? = nil) async throws -> Agent {
        let core = try await ensureStarted()
        let history = try await exportHistoryRaw(core: core)

        let child = Agent(
            config: config,
            name: name ?? "\(self.name)-fork-\(UUID().uuidString.prefix(4))"
        )
        let childCore = try await child.ensureStarted()

        if !history.isEmpty {
            _ = try await childCore.rawCall(
                RpcMethod.sessionInject,
                params: .object([
                    "messages": .array(history),
                    "position": .string("replace"),
                ])
            )
        }

        // RAGインデックスをスナップショットコピー
        let snap = await index.snapshot()
        await child.index.restore(from: snap)
        return child
    }

    public func add(_ other: Agent) async throws {
        let myCore = try await ensureStarted()
        let otherCore = try await other.ensureStarted()
        let history = try await exportHistoryRaw(core: otherCore)
        guard !history.isEmpty else { return }
        _ = try await myCore.rawCall(
            RpcMethod.sessionInject,
            params: .object([
                "messages": .array(history),
                "position": .string("append"),
            ])
        )
    }

    public func addSummary(_ other: Agent) async throws {
        let summary = try await other.context()
        guard !summary.isEmpty else { return }
        let myCore = try await ensureStarted()
        _ = try await myCore.rawCall(
            RpcMethod.sessionInject,
            params: .object([
                "messages": .array([
                    .object([
                        "role": .string("user"),
                        "content": .string("[前のエージェントの会話要約]\n\(summary)"),
                    ])
                ]),
                "position": .string("prepend"),
            ])
        )
    }

    // MARK: - エクスポート / インポート / branch

    public func export() async throws -> JSONValue {
        let core = try await ensureStarted()
        let history = try await exportHistoryRaw(core: core)
        let ragSnapshot = await index.snapshot()
        let ragJson = ragSnapshot.docs.map { entry in
            JSONValue.object([
                "role": .string(entry.role),
                "content": .string(entry.content),
            ])
        }
        return .object([
            "version": .int(1),
            "agent_name": .string(name),
            "timestamp": .double(Date().timeIntervalSince1970),
            "messages": .array(history),
            "rag_index": .array(ragJson),
        ])
    }

    public func importHistory(_ data: JSONValue) async throws {
        let core = try await ensureStarted()
        let messages = data["messages"]?.arrayValue ?? []
        _ = try await core.rawCall(
            RpcMethod.sessionInject,
            params: .object([
                "messages": .array(messages),
                "position": .string("replace"),
            ])
        )
        // RAGインデックス復元
        var newDocs: [IndexedMessage] = []
        let ragSource = data["rag_index"]?.arrayValue ?? messages
        for m in ragSource {
            newDocs.append(IndexedMessage(
                role: m["role"]?.stringValue ?? "user",
                content: m["content"]?.stringValue ?? ""
            ))
        }
        await index.restore(from: MessageIndexSnapshot(docs: newDocs))
    }

    public func branch(from fromIndex: Int, name: String? = nil) async throws -> Agent {
        let core = try await ensureStarted()
        let history = try await exportHistoryRaw(core: core)
        let subset = Array(history.dropFirst(fromIndex))

        let child = Agent(
            config: config,
            name: name ?? "\(self.name)-branch-\(UUID().uuidString.prefix(4))"
        )
        let childCore = try await child.ensureStarted()
        if !subset.isEmpty {
            _ = try await childCore.rawCall(
                RpcMethod.sessionInject,
                params: .object([
                    "messages": .array(subset),
                    "position": .string("replace"),
                ])
            )
        }
        return child
    }

    // MARK: - バッチ処理

    public func batch(
        _ prompts: [String],
        maxConcurrency: Int = 3,
        timeout: Duration? = nil
    ) async throws -> [String] {
        let semCount = max(1, maxConcurrency)
        let semaphore = AsyncSemaphore(value: semCount)

        return try await withThrowingTaskGroup(of: (Int, String).self) { group in
            for (idx, prompt) in prompts.enumerated() {
                group.addTask {
                    await semaphore.wait()
                    defer { Task { await semaphore.signal() } }
                    let child = try await self.fork()
                    do {
                        let response = try await child.input(prompt, timeout: timeout)
                        await child.close()
                        return (idx, response)
                    } catch {
                        await child.close()
                        throw error
                    }
                }
            }
            var results = Array(repeating: "", count: prompts.count)
            for try await (idx, response) in group {
                results[idx] = response
            }
            return results
        }
    }

    // MARK: - ストリーミング

    public func stream(_ prompt: String) -> AsyncThrowingStream<String, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    let core = try await self.ensureStarted()
                    let counter = StreamingCounter()
                    let result = try await core.run(
                        prompt,
                        stream: { text, _ in
                            await counter.increment()
                            continuation.yield(text)
                        }
                    )
                    if await counter.value == 0 {
                        continuation.yield(result.response)
                    }
                    await self.index.add(role: "user", content: prompt)
                    await self.index.add(role: "assistant", content: result.response)
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    // MARK: - RAG検索

    public func search(_ query: String, topK: Int = 5) async -> [SearchHit] {
        await index.search(query, topK: topK)
    }

    // MARK: - 動的スキル修正

    /// ツールの説明をLLMで改善する。改善されたToolを返す (失敗時はnil)。
    ///
    /// 改善要求は汚染のない独立したエージェントで行うため、メイン会話に影響しない。
    /// 既存ツールはGoエンジンがduplicate name拒否のため上書きできない。fork等で使う想定。
    public func improveTool(_ toolName: String, feedback: String) async throws -> Tool? {
        guard let original = registeredTools[toolName] else { return nil }

        let improvePrompt = """
        あなたはツール定義の改善専門家です。以下のツールの説明文を改善してください。

        ツール名: \(toolName)
        現在の説明: \(original.description)
        フィードバック: \(feedback)

        改善した説明文だけを以下のJSON形式で返してください（他のテキスト不要）:
        {"description": "改善された説明文"}
        """

        let freshConfig = AgentConfig(
            binary: config.binary,
            env: config.env,
            cwd: config.cwd,
            systemPrompt: "ツール定義の改善専門家として、JSON形式のみで回答してください。",
            maxTurns: 2
        )

        let helper = Agent(config: freshConfig, name: "\(name)-improve")
        try await helper.start()
        defer { Task { await helper.close() } }

        let resp = try await helper.input(improvePrompt)

        // JSON抽出
        guard let newDesc = extractDescription(from: resp) else { return nil }

        let improved = Tool(
            name: toolName,
            description: newDesc,
            parameters: original.parameters,
            readOnly: original.readOnly,
            handler: original.handler
        )

        // 既登録の場合はSDK記録のみ更新
        if registeredTools[toolName] != nil {
            registeredTools[toolName] = improved
        }
        return improved
    }

    private nonisolated func extractDescription(from resp: String) -> String? {
        // {"description": "..."} を抽出
        let pattern = #"\{[^}]*"description"\s*:\s*"([^"]+)"[^}]*\}"#
        guard let regex = try? NSRegularExpression(pattern: pattern, options: [.dotMatchesLineSeparators]) else {
            return nil
        }
        let range = NSRange(resp.startIndex..<resp.endIndex, in: resp)
        guard let match = regex.firstMatch(in: resp, range: range),
              match.numberOfRanges >= 2,
              let r = Range(match.range(at: 1), in: resp) else {
            return nil
        }
        return String(resp[r])
    }

    // MARK: - 内部ヘルパー

    private func exportHistoryRaw(core: RawAgent) async throws -> [JSONValue] {
        let raw = try await core.rawCall(RpcMethod.sessionHistory, params: .object([:]))
        return raw["messages"]?.arrayValue ?? []
    }
}

// MARK: - スキル設定の読み込み

func loadSkillConfigs(_ skillDir: String) -> [[String: JSONValue]] {
    let fm = FileManager.default
    var isDir: ObjCBool = false
    guard fm.fileExists(atPath: skillDir, isDirectory: &isDir), isDir.boolValue else {
        return []
    }
    guard let entries = try? fm.contentsOfDirectory(atPath: skillDir) else { return [] }
    var configs: [[String: JSONValue]] = []
    for entry in entries.sorted() {
        let entryPath = (skillDir as NSString).appendingPathComponent(entry)
        for fname in ["skill.json", "mcp.json", "config.json"] {
            let cfgPath = (entryPath as NSString).appendingPathComponent(fname)
            if fm.fileExists(atPath: cfgPath) {
                if let data = fm.contents(atPath: cfgPath),
                   let raw = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                    var cfg: [String: JSONValue] = [:]
                    for (k, v) in raw { cfg[k] = JSONValue.from(v) }
                    cfg["_source"] = .string(cfgPath)
                    configs.append(cfg)
                }
                break
            }
        }
    }
    return configs
}

// MARK: - 内部ユーティリティ

actor StreamingCounter {
    private(set) var value: Int = 0
    func increment() { value += 1 }
}

actor AsyncSemaphore {
    private var value: Int
    private var waiters: [CheckedContinuation<Void, Never>] = []

    init(value: Int) {
        self.value = value
    }

    func wait() async {
        if value > 0 {
            value -= 1
            return
        }
        await withCheckedContinuation { (cont: CheckedContinuation<Void, Never>) in
            waiters.append(cont)
        }
    }

    func signal() {
        if let first = waiters.first {
            waiters.removeFirst()
            first.resume()
        } else {
            value += 1
        }
    }
}
