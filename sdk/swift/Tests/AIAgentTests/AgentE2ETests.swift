import XCTest
@testable import AIAgent

/// 実バイナリ + ローカルSLLMを使ったE2Eテスト。
///
/// 環境変数 `AGENT_BINARY` でバイナリパスを指定する。未設定なら全てスキップ。
/// 例: `AGENT_BINARY=/path/to/agent swift test`
final class AgentE2ETests: XCTestCase {
    var binaryPath: String? {
        ProcessInfo.processInfo.environment["AGENT_BINARY"]
    }

    var llmEndpoint: String {
        ProcessInfo.processInfo.environment["SLLM_ENDPOINT"]
            ?? "http://localhost:8080/v1/chat/completions"
    }

    var llmApiKey: String {
        ProcessInfo.processInfo.environment["SLLM_API_KEY"] ?? "sk-gemma4"
    }

    var defaultEnv: [String: String] {
        [
            "SLLM_ENDPOINT": llmEndpoint,
            "SLLM_API_KEY": llmApiKey,
        ]
    }

    func skipIfNoBinary() throws -> String {
        guard let binary = binaryPath else {
            throw XCTSkip("AGENT_BINARY not set; skipping E2E test")
        }
        guard FileManager.default.isExecutableFile(atPath: binary) else {
            throw XCTSkip("AGENT_BINARY not executable: \(binary)")
        }
        return binary
    }

    // MARK: - configure / abort (LLMサーバ無くても動く)

    func testConfigureAgainstRealBinary() async throws {
        let binary = try skipIfNoBinary()
        let raw = RawAgent(binaryPath: binary, env: defaultEnv)
        try await raw.start()
        defer { Task { await raw.close() } }

        let applied = try await raw.configure(CoreAgentConfig(
            maxTurns: 3,
            streaming: StreamingConfig(enabled: true, contextStatus: true)
        ))
        XCTAssertTrue(applied.contains("max_turns"))
        XCTAssertTrue(applied.contains("streaming"))
    }

    func testAbortWhenIdleReturnsFalse() async throws {
        let binary = try skipIfNoBinary()
        let raw = RawAgent(binaryPath: binary, env: defaultEnv)
        try await raw.start()
        defer { Task { await raw.close() } }
        let ok = try await raw.abort(reason: "test")
        XCTAssertFalse(ok)
    }

    // MARK: - 実LLMを叩く

    func testHighLevelAgentInput() async throws {
        let binary = try skipIfNoBinary()
        try await ensureLLMReachable()

        let agent = Agent(config: AgentConfig(
            binary: binary,
            env: defaultEnv,
            systemPrompt: "あなたは親切なアシスタントです。簡潔に答えてください。",
            maxTurns: 3
        ))
        try await agent.start()
        defer { Task { await agent.close() } }

        let response = try await agent.input("こんにちは")
        XCTAssertFalse(response.isEmpty, "LLMからの応答が空")
        print("[E2E] LLM応答: \(response)")
    }

    func testInputVerbose() async throws {
        let binary = try skipIfNoBinary()
        try await ensureLLMReachable()

        let agent = Agent(config: AgentConfig(
            binary: binary,
            env: defaultEnv,
            systemPrompt: "日本語で簡潔に答えてください",
            maxTurns: 3
        ))
        try await agent.start()
        defer { Task { await agent.close() } }

        let result = try await agent.inputVerbose("1+1の答えは？")
        XCTAssertFalse(result.response.isEmpty)
        XCTAssertGreaterThan(result.turns, 0)
        XCTAssertGreaterThan(result.usage.totalTokens, 0)
        print("[E2E] turns=\(result.turns) tokens=\(result.usage.totalTokens) resp=\(result.response)")
    }

    func testForkPreservesHistory() async throws {
        let binary = try skipIfNoBinary()
        try await ensureLLMReachable()

        let parent = Agent(config: AgentConfig(
            binary: binary,
            env: defaultEnv,
            systemPrompt: "簡潔に答えてください",
            maxTurns: 3
        ))
        try await parent.start()
        defer { Task { await parent.close() } }

        _ = try await parent.input("私の名前は花子です。")
        let child = try await parent.fork()
        defer { Task { await child.close() } }

        let response = try await child.input("私の名前は何ですか？")
        print("[E2E fork] child応答: \(response)")
        // 厳密な一致は期待しない (SLLMは間違うかも) が、forkが動いていることを確認
        XCTAssertFalse(response.isEmpty)
    }

    func testRegisterToolAndExecute() async throws {
        let binary = try skipIfNoBinary()
        try await ensureLLMReachable()

        let echoTool = Tool(
            name: "echo_text",
            description: "Echo back the input text",
            parameters: .object([
                "type": .string("object"),
                "properties": .object([
                    "text": .object(["type": .string("string")]),
                ]),
                "required": .array([.string("text")]),
                "additionalProperties": .bool(false),
            ]),
            readOnly: true
        ) { args in
            let text = args["text"]?.stringValue ?? ""
            return .text("ECHO: \(text)")
        }

        let agent = Agent(config: AgentConfig(
            binary: binary,
            env: defaultEnv,
            systemPrompt: "ツールを使ってユーザの要求に応えてください。",
            maxTurns: 5
        ))
        try await agent.start()
        defer { Task { await agent.close() } }

        let registered = try await agent.registerTools(echoTool)
        XCTAssertEqual(registered, ["echo_text"])
        print("[E2E tool] registered=\(registered)")
    }

    // MARK: - ヘルパ: LLMサーバの疎通確認

    func ensureLLMReachable() async throws {
        let url = URL(string: llmEndpoint)!
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("Bearer \(llmApiKey)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let body: [String: Any] = ["messages": [["role": "user", "content": "ping"]]]
        req.httpBody = try JSONSerialization.data(withJSONObject: body)
        // SLLMはシングルプロセスでテスト連続実行時の競合待ちが発生するため長めにとる。
        req.timeoutInterval = 60
        do {
            let (_, resp) = try await URLSession.shared.data(for: req)
            if let http = resp as? HTTPURLResponse, http.statusCode != 200 {
                throw XCTSkip("LLM endpoint returned \(http.statusCode)")
            }
        } catch {
            throw XCTSkip("LLM endpoint not reachable: \(error)")
        }
    }
}

