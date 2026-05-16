import XCTest
@testable import AIAgent

final class GuardOutcomeTests: XCTestCase {

    // MARK: - GuardOutcome

    func testGuardOutcomeNamedInit() {
        let o = GuardOutcome(decision: .deny, reason: "blocked")
        XCTAssertEqual(o.decision, .deny)
        XCTAssertEqual(o.reason, "blocked")
    }

    func testGuardOutcomeTupleStyleInit() {
        let o = GuardOutcome(.tripwire, "limits")
        XCTAssertEqual(o.decision, .tripwire)
        XCTAssertEqual(o.reason, "limits")
    }

    func testGuardOutcomeShortcuts() {
        XCTAssertEqual(GuardOutcome.allow.decision, .allow)
        XCTAssertEqual(GuardOutcome.deny("x").reason, "x")
        XCTAssertEqual(GuardOutcome.tripwire("y").decision, .tripwire)
    }

    // MARK: - VerifierOutcome

    func testVerifierOutcomeTupleStyleInit() {
        let o = VerifierOutcome(false, "regression")
        XCTAssertFalse(o.passed)
        XCTAssertEqual(o.summary, "regression")
    }

    func testVerifierOutcomeShortcuts() {
        XCTAssertTrue(VerifierOutcome.pass.passed)
        XCTAssertEqual(VerifierOutcome.fail("nope").summary, "nope")
    }

    // MARK: - JudgeOutcome

    func testJudgeOutcomeShortcuts() {
        XCTAssertFalse(JudgeOutcome.continue.terminate)
        let done = JudgeOutcome.done("good")
        XCTAssertTrue(done.terminate)
        XCTAssertEqual(done.reason, "good")
    }

    // MARK: - GuardSpec のヘルパが新シグネチャで動く

    func testToolCallGuardWithStructOutcome() async throws {
        let g = GuardSpec.toolCall(name: "no_shell") { tool, _ in
            tool == "shell" ? .deny("shell tool blocked") : .allow
        }
        let r1 = try await g.handler(
            GuardInput(input: "", toolName: "shell", args: .object([:]), output: "")
        )
        XCTAssertEqual(r1.decision, .deny)
        let r2 = try await g.handler(
            GuardInput(input: "", toolName: "echo", args: .object([:]), output: "")
        )
        XCTAssertEqual(r2.decision, .allow)
    }
}
