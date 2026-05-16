import XCTest
@testable import AIAgent

final class AgentResultTests: XCTestCase {

    func testCompletedReason() {
        let r = AgentResult(
            response: "hi",
            reason: "completed",
            turns: 1,
            usage: UsageInfo()
        )
        XCTAssertEqual(r.terminationReason, .completed)
        XCTAssertTrue(r.isCompleted)
        XCTAssertFalse(r.isMaxTurns)
    }

    func testMaxTurnsReason() {
        let r = AgentResult(
            response: "Stopped after 20 turns (max_turns reached)",
            reason: "max_turns",
            turns: 20,
            usage: UsageInfo()
        )
        XCTAssertEqual(r.terminationReason, .maxTurns)
        XCTAssertTrue(r.isMaxTurns)
        XCTAssertFalse(r.isCompleted)
    }

    func testInputDeniedReason() {
        let r = AgentResult(
            response: "Input rejected: blocked",
            reason: "input_denied",
            turns: 0,
            usage: UsageInfo()
        )
        XCTAssertEqual(r.terminationReason, .inputDenied)
    }

    func testUnknownReasonFallsBackToOther() {
        let r = AgentResult(
            response: "x",
            reason: "weird_custom_reason",
            turns: 1,
            usage: UsageInfo()
        )
        XCTAssertEqual(r.terminationReason, .other(raw: "weird_custom_reason"))
    }

    func testEmptyReason() {
        let r = AgentResult(response: "", reason: "", turns: 0, usage: UsageInfo())
        XCTAssertEqual(r.terminationReason, .other(raw: ""))
    }
}
