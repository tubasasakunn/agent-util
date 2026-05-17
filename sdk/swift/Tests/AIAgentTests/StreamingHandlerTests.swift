import XCTest
@testable import AIAgent

final class StreamingHandlerTests: XCTestCase {

    func testStreamingHandlerCallbackForwardsChunks() async throws {
        // streaming handler を直接呼んで、onDelta が動作することを確認する
        // (実バイナリ抜きの単体テスト)
        let received = ChunkBox()

        let handler: LLMStreamingHandler = { _, onDelta in
            await onDelta("Hello ")
            await onDelta("World")
            return .object([
                "id": .string("test"),
                "object": .string("chat.completion"),
                "choices": .array([.object([
                    "message": .object([
                        "role": .string("assistant"),
                        "content": .string("Hello World"),
                    ]),
                ])]),
            ])
        }

        let request: JSONValue = .object([
            "model": .string("test"),
            "messages": .array([]),
        ])

        let response = try await handler(request) { chunk in
            await received.append(chunk)
        }

        let chunks = await received.get()
        XCTAssertEqual(chunks, ["Hello ", "World"])
        XCTAssertEqual(
            response["choices"]?[0]?["message"]?["content"]?.stringValue,
            "Hello World"
        )
    }

    func testAgentConfigPrefersStreamingHandlerOverPlain() {
        let plain: LLMHandler = { _ in .object([:]) }
        let stream: LLMStreamingHandler = { _, _ in .object([:]) }

        let cfg = AgentConfig(
            binary: "agent",
            llmHandler: plain,
            llmStreamingHandler: stream
        )
        XCTAssertNotNil(cfg.llmHandler)
        XCTAssertNotNil(cfg.llmStreamingHandler)
        // 両方指定された場合の優先順位は Agent.start() 内で streaming が勝つ。
        // ここではフィールド両方が保持されていることだけ確認する。
    }

    func testLLMModeRemoteAppliedForStreamingHandler() {
        let stream: LLMStreamingHandler = { _, _ in .object([:]) }
        let cfg = AgentConfig(
            binary: "agent",
            llmStreamingHandler: stream
        )
        let core = cfg.toCoreConfig()
        XCTAssertEqual(core.llm?.mode, .remote)
    }

    func testLLMModeNotAppliedWithoutHandler() {
        let cfg = AgentConfig(binary: "agent")
        XCTAssertNil(cfg.toCoreConfig().llm)
    }
}

actor ChunkBox {
    private var items: [String] = []
    func append(_ s: String) { items.append(s) }
    func get() -> [String] { items }
}
