import XCTest
@testable import AIAgent

final class ErrorsTests: XCTestCase {

    // MARK: - kind 推定

    func testKindDerivedFromKnownRpcCode() {
        let err = AgentError("nope", code: -32000)
        XCTAssertEqual(err.kind, .toolNotFound)
    }

    func testKindUnknownCodeFallsBackToOther() {
        let err = AgentError("unknown", code: -1)
        XCTAssertEqual(err.kind, .other(code: -1))
    }

    func testMaxTurnsKindDerivedFromMessage() {
        let err = AgentError("max turns reached after 20 iterations", code: -32603)
        XCTAssertEqual(err.kind, .maxTurnsReached)
    }

    func testOtherInternalErrorIsNotMaxTurns() {
        let err = AgentError("something else", code: -32603)
        XCTAssertEqual(err.kind, .other(code: -32603))
    }

    // MARK: - LocalizedError

    func testLocalizedDescriptionIsHumanReadable() {
        let err = AgentError("registered: [echo, fetch]", code: -32000)
        let desc = err.localizedDescription
        XCTAssertFalse(
            desc.contains("The operation couldn't be completed"),
            "localizedDescription must not be the generic fallback"
        )
        XCTAssertTrue(desc.contains("tool not found"))
    }

    func testRecoverySuggestionForMaxTurns() {
        let err = AgentError("max turns reached after 20 iterations", code: -32603)
        XCTAssertNotNil(err.recoverySuggestion)
        XCTAssertTrue(err.recoverySuggestion?.contains("maxTurns") ?? false)
    }

    // MARK: - GuardDenied / Tripwire のサブクラス kind

    func testGuardDeniedFactoryHasGuardDeniedKind() {
        let err = fromRpcError(
            code: -32005,
            message: "denied",
            data: .object(["decision": .string("deny"), "reason": .string("blocked-keyword")])
        )
        XCTAssertEqual(err.kind, .guardDenied)
        XCTAssertTrue(err is GuardDenied)
        XCTAssertEqual((err as? GuardDenied)?.reason, "blocked-keyword")
    }

    func testTripwireFactoryHasTripwireKind() {
        let err = fromRpcError(
            code: -32006,
            message: "fire",
            data: .object(["reason": .string("limits")])
        )
        XCTAssertEqual(err.kind, .tripwireTriggered)
        XCTAssertTrue(err is TripwireTriggered)
    }

    // MARK: - rpcCode 双方向

    func testRpcCodeRoundTrip() {
        for kind in [
            AgentErrorKind.toolNotFound,
            .toolExecutionFailed,
            .agentBusy,
            .aborted,
            .messageTooLarge,
            .guardDenied,
            .tripwireTriggered,
            .maxTurnsReached,
        ] {
            guard let code = kind.rpcCode else {
                XCTFail("known kind \(kind) must have rpcCode")
                continue
            }
            let derived = AgentError("dummy", code: code).kind
            // maxTurnsReached は message に "max turns" が必要なので個別ケース
            if kind == .maxTurnsReached {
                let withMessage = AgentError("max turns reached", code: code).kind
                XCTAssertEqual(withMessage, .maxTurnsReached)
            } else {
                XCTAssertEqual(derived, kind, "code \(code) should derive to \(kind), got \(derived)")
            }
        }
    }
}
