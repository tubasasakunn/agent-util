import XCTest
@testable import AIAgent

final class ConfigTests: XCTestCase {
    func testCoreConfigOmitNil() {
        let cfg = CoreAgentConfig(maxTurns: 5, systemPrompt: "hi")
        let params = cfg.toParams()
        XCTAssertEqual(params["max_turns"]?.intValue, 5)
        XCTAssertEqual(params["system_prompt"]?.stringValue, "hi")
        XCTAssertNil(params["token_limit"])
        XCTAssertNil(params["delegate"])
    }

    func testStreamingConfigSerialization() {
        let cfg = CoreAgentConfig(streaming: StreamingConfig(enabled: true, contextStatus: true))
        let params = cfg.toParams()
        guard let streaming = params["streaming"]?.objectValue else {
            XCTFail("streaming should be object")
            return
        }
        XCTAssertEqual(streaming["enabled"]?.boolValue, true)
        XCTAssertEqual(streaming["context_status"]?.boolValue, true)
    }

    func testGuardsConfigCamelToSnake() {
        let cfg = CoreAgentConfig(guards: GuardsConfig(input: ["a"], toolCall: ["b"], output: ["c"]))
        let params = cfg.toParams()
        let guards = params["guards"]
        XCTAssertEqual(guards?["input"]?.arrayValue?[0].stringValue, "a")
        XCTAssertEqual(guards?["tool_call"]?.arrayValue?[0].stringValue, "b")
        XCTAssertEqual(guards?["output"]?.arrayValue?[0].stringValue, "c")
    }

    func testAgentConfigToCoreConfig() {
        let agent = AgentConfig(
            binary: "./agent",
            env: ["FOO": "BAR"],
            systemPrompt: "sys",
            maxTurns: 7,
            streaming: StreamingConfig(enabled: true)
        )
        let core = agent.toCoreConfig()
        XCTAssertEqual(core.maxTurns, 7)
        XCTAssertEqual(core.systemPrompt, "sys")
        XCTAssertEqual(core.streaming?.enabled, true)
    }

    func testStripNullRemovesNullEntries() {
        let v: JSONValue = .object([
            "keep": .int(1),
            "drop": .null,
            "nested": .object([
                "inner_keep": .string("yes"),
                "inner_drop": .null,
            ]),
        ])
        let stripped = stripNull(v)
        XCTAssertNotNil(stripped["keep"])
        XCTAssertNil(stripped["drop"])
        XCTAssertNotNil(stripped["nested"]?["inner_keep"])
        XCTAssertNil(stripped["nested"]?["inner_drop"])
    }
}
