import XCTest
@testable import AIAgent

final class ConfigTests: XCTestCase {
    func testEmptyConfigProducesEmptyObject() throws {
        let cfg = AgentConfig()
        let params = cfg.toParams()
        XCTAssertEqual(params.objectValue?.count, 0)
    }

    func testTopLevelFieldsOmitNils() throws {
        let cfg = AgentConfig(maxTurns: 5, systemPrompt: "hi")
        let params = cfg.toParams()
        XCTAssertEqual(params["max_turns"].intValue, 5)
        XCTAssertEqual(params["system_prompt"].stringValue, "hi")
        XCTAssertNil(params["token_limit"])
        XCTAssertNil(params["work_dir"])
        XCTAssertNil(params["streaming"])
    }

    func testNestedConfigsUseSnakeCase() throws {
        let cfg = AgentConfig(
            permission: PermissionConfig(enabled: true, allow: ["read_file"]),
            guards: GuardsConfig(input: ["no_secrets"], toolCall: ["fs_root_only"]),
            verify: VerifyConfig(verifiers: ["non_empty"], maxStepRetries: 3),
            toolScope: ToolScopeConfig(maxTools: 8, includeAlways: ["read_file"]),
            streaming: StreamingConfig(enabled: true, contextStatus: false)
        )
        let p = cfg.toParams()

        XCTAssertEqual(p["permission"]?["enabled"].boolValue, true)
        XCTAssertEqual(p["permission"]?["allow"].arrayValue?.count, 1)

        XCTAssertEqual(p["guards"]?["input"].arrayValue?.first?.stringValue, "no_secrets")
        XCTAssertEqual(p["guards"]?["tool_call"].arrayValue?.first?.stringValue, "fs_root_only")
        XCTAssertNil(p["guards"]?["output"])

        XCTAssertEqual(p["verify"]?["max_step_retries"].intValue, 3)
        XCTAssertNil(p["verify"]?["max_consecutive_failures"])

        XCTAssertEqual(p["tool_scope"]?["max_tools"].intValue, 8)
        XCTAssertEqual(p["streaming"]?["enabled"].boolValue, true)
        XCTAssertEqual(p["streaming"]?["context_status"].boolValue, false)
    }

    func testCompactionConfigEncoding() throws {
        let cfg = AgentConfig(
            compaction: CompactionConfig(
                enabled: true,
                budgetMaxChars: 12000,
                keepLast: 4,
                targetRatio: 0.6,
                summarizer: "llm"
            )
        )
        let p = cfg.toParams()
        XCTAssertEqual(p["compaction"]?["budget_max_chars"].intValue, 12000)
        XCTAssertEqual(p["compaction"]?["target_ratio"].doubleValue, 0.6)
        XCTAssertEqual(p["compaction"]?["summarizer"].stringValue, "llm")
    }
}
