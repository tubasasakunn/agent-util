import XCTest
@testable import AIAgent

final class ObservabilityTests: XCTestCase {

    // MARK: - ToolCallRecord / GuardFireRecord 構造

    func testToolCallRecordEquatable() {
        let a = ToolCallRecord(name: "echo", isError: false, argsJSON: "{}")
        let b = ToolCallRecord(name: "echo", isError: false, argsJSON: "{}")
        XCTAssertEqual(a, b)
    }

    func testGuardFireRecordEquatable() {
        let a = GuardFireRecord(kind: "guard.input", name: "x", decision: "deny", reason: "no")
        let b = GuardFireRecord(kind: "guard.input", name: "x", decision: "deny", reason: "no")
        XCTAssertEqual(a, b)
    }

    // MARK: - AgentResult のフィールド

    func testAgentResultDefaultsObservabilityFieldsToEmpty() {
        let r = AgentResult(response: "hi", reason: "completed", turns: 1, usage: UsageInfo())
        XCTAssertEqual(r.toolCalls, [])
        XCTAssertEqual(r.guardFires, [])
    }

    func testAgentResultWithRecords() {
        let r = AgentResult(
            response: "hi",
            reason: "completed",
            turns: 2,
            usage: UsageInfo(),
            toolCalls: [
                ToolCallRecord(name: "echo", isError: false, argsJSON: "{}"),
                ToolCallRecord(name: "echo", isError: false, argsJSON: "{}"),
            ],
            guardFires: [
                GuardFireRecord(kind: "guard.input", name: "no_secrets", decision: "allow", reason: ""),
            ]
        )
        XCTAssertEqual(r.toolCalls.count, 2)
        XCTAssertEqual(r.toolCalls.filter { $0.name == "echo" }.count, 2)
        XCTAssertEqual(r.guardFires.first?.decision, "allow")
    }

    // MARK: - RunObserver actor の独立動作

    func testRunObserverRecordsAndResets() async {
        let obs = RunObserver()
        await obs.recordToolCall(.init(name: "a", isError: false, argsJSON: "{}"))
        await obs.recordToolCall(.init(name: "b", isError: true, argsJSON: "{}"))
        await obs.recordGuardFire(.init(kind: "guard.input", name: "g", decision: "deny", reason: "x"))
        let snap1 = await obs.snapshot()
        XCTAssertEqual(snap1.toolCalls.count, 2)
        XCTAssertEqual(snap1.guardFires.count, 1)
        await obs.reset()
        let snap2 = await obs.snapshot()
        XCTAssertEqual(snap2.toolCalls.count, 0)
        XCTAssertEqual(snap2.guardFires.count, 0)
    }

    // MARK: - AgentPhase

    func testAgentPhaseRawValues() {
        XCTAssertEqual(AgentPhase.routing.rawValue, "routing")
        XCTAssertEqual(AgentPhase.tool.rawValue, "tool")
        XCTAssertEqual(AgentPhase.guarding.rawValue, "guarding")
        XCTAssertEqual(AgentPhase.verifying.rawValue, "verifying")
        XCTAssertEqual(AgentPhase.judging.rawValue, "judging")
        XCTAssertEqual(AgentPhase.generating.rawValue, "generating")
    }
}
