import XCTest
@testable import AIAgent

final class DebugDumpTests: XCTestCase {

    func testDumpShowsSnakeCaseKeys() {
        let cfg = AgentConfig(
            binary: "agent",
            systemPrompt: "hi",
            maxTurns: 5,
            tokenLimit: 8000,
            workDir: "/tmp/work"
        )
        let dump = cfg.debugDump()
        // camelCase で書いた Swift 値が、送信時には snake_case になる
        XCTAssertTrue(dump.contains("\"system_prompt\""))
        XCTAssertTrue(dump.contains("\"max_turns\""))
        XCTAssertTrue(dump.contains("\"token_limit\""))
        XCTAssertTrue(dump.contains("\"work_dir\""))
        // camelCase の Swift 名は出ない
        XCTAssertFalse(dump.contains("\"systemPrompt\""))
        XCTAssertFalse(dump.contains("\"maxTurns\""))
    }

    func testDumpOmitsNilFields() {
        let cfg = AgentConfig(binary: "agent", maxTurns: 5)
        let dump = cfg.debugDump()
        // nil のフィールドは送信に含まれない (omitempty)
        XCTAssertFalse(dump.contains("\"system_prompt\""))
        XCTAssertFalse(dump.contains("\"token_limit\""))
        XCTAssertFalse(dump.contains("\"work_dir\""))
        XCTAssertTrue(dump.contains("\"max_turns\""))
    }

    func testDumpNestedConfigCamelToSnake() {
        let cfg = AgentConfig(
            binary: "agent",
            compaction: CompactionConfig(
                enabled: true,
                budgetMaxChars: 6000,
                keepLast: 3,
                targetRatio: 0.6,
                summarizer: "truncate"
            )
        )
        let dump = cfg.debugDump()
        XCTAssertTrue(dump.contains("\"budget_max_chars\""))
        XCTAssertTrue(dump.contains("\"keep_last\""))
        XCTAssertTrue(dump.contains("\"target_ratio\""))
        XCTAssertFalse(dump.contains("\"budgetMaxChars\""))
    }

    func testDumpIsValidJson() {
        let cfg = AgentConfig(binary: "agent", maxTurns: 5)
        let dump = cfg.debugDump()
        // 整形済み JSON として再パース可能
        let data = dump.data(using: .utf8)!
        XCTAssertNoThrow(try JSONSerialization.jsonObject(with: data))
    }

    func testDumpPrettyPrinted() {
        let cfg = AgentConfig(binary: "agent", maxTurns: 5)
        let dump = cfg.debugDump()
        // pretty: 改行・インデント付き
        XCTAssertTrue(dump.contains("\n"))
    }
}
