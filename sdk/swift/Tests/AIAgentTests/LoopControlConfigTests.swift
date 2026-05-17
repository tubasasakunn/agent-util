import XCTest
@testable import AIAgent

/// Phase 3-1 (A1/A2/A4) の AgentConfig 配線テスト。
final class LoopControlConfigTests: XCTestCase {

    func testToolScopeWithToolBudgetSerializesToSnakeCase() {
        let cfg = AgentConfig(
            binary: "agent",
            toolScope: ToolScopeConfig(
                maxTools: 3,
                includeAlways: ["echo"],
                toolBudget: ["shell": 1, "fetch": 3]
            )
        )
        let dump = cfg.debugDump()
        XCTAssertTrue(dump.contains("\"tool_budget\""))
        XCTAssertTrue(dump.contains("\"shell\""))
        XCTAssertTrue(dump.contains("\"max_tools\""))
        XCTAssertFalse(dump.contains("\"toolBudget\""))
    }

    func testJudgeConfigWithBuiltin() {
        let cfg = AgentConfig(
            binary: "agent",
            judge: JudgeConfig(builtin: "min_length:30")
        )
        let dump = cfg.debugDump()
        XCTAssertTrue(dump.contains("\"builtin\""))
        XCTAssertTrue(dump.contains("\"min_length:30\""))
        // name は空文字でも省略
        XCTAssertFalse(dump.contains("\"name\""))
    }

    func testJudgeConfigWithName() {
        let cfg = AgentConfig(
            binary: "agent",
            judge: JudgeConfig(name: "concise")
        )
        let dump = cfg.debugDump()
        XCTAssertTrue(dump.contains("\"name\":\"concise\"") || dump.contains("\"name\" : \"concise\""))
        // builtin は nil なので出ない
        XCTAssertFalse(dump.contains("\"builtin\""))
    }

    func testToolScopeWithoutToolBudget() {
        let cfg = AgentConfig(
            binary: "agent",
            toolScope: ToolScopeConfig(maxTools: 5)
        )
        let dump = cfg.debugDump()
        // tool_budget は nil なので出ない
        XCTAssertFalse(dump.contains("\"tool_budget\""))
    }
}
