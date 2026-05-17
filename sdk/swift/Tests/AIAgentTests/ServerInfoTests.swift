import XCTest
@testable import AIAgent

final class ServerInfoTests: XCTestCase {

    func testServerInfoSupportsFeature() {
        let info = ServerInfo(
            libraryVersion: "0.3.0",
            protocolVersion: "2.0",
            methods: ["agent.run"],
            features: ["llm_execute": true, "experimental": false]
        )
        XCTAssertTrue(info.supports("llm_execute"))
        XCTAssertFalse(info.supports("experimental"))
        XCTAssertFalse(info.supports("nonexistent"))
    }

    func testServerInfoEquatable() {
        let a = ServerInfo(
            libraryVersion: "0.3.0",
            protocolVersion: "2.0",
            methods: ["agent.run"],
            features: ["x": true]
        )
        let b = ServerInfo(
            libraryVersion: "0.3.0",
            protocolVersion: "2.0",
            methods: ["agent.run"],
            features: ["x": true]
        )
        XCTAssertEqual(a, b)
    }

    func testSDKLibraryVersionConstant() {
        // 真実の源 (pkg/protocol/version.go) と同期されているべき値。
        // CLAUDE.md `## バージョン` の同期表に従う。
        XCTAssertEqual(aiAgentSDKLibraryVersion, "0.3.0")
    }

    // MARK: - withStderrHint (E4)

    func testWithStderrHintAddsTailToData() {
        let err = AgentError("boom", code: -32603)
        let withHint = err.withStderrHint("trailing log message\n")
        XCTAssertEqual(withHint.data?["stderr_tail"]?.stringValue, "trailing log message\n")
        // kind / code / message は維持される
        XCTAssertEqual(withHint.code, err.code)
        XCTAssertEqual(withHint.message, err.message)
    }

    func testWithStderrHintTruncatesLargeOutput() {
        let bigStderr = String(repeating: "X", count: 5_000)
        let err = AgentError("oops")
        let withHint = err.withStderrHint(bigStderr)
        let tail = withHint.data?["stderr_tail"]?.stringValue ?? ""
        XCTAssertEqual(tail.count, 2048, "stderr tail should be truncated to last 2KB")
    }

    func testWithStderrHintEmptyKeepsOriginal() {
        let err = AgentError("e")
        let withHint = err.withStderrHint("")
        XCTAssertNil(withHint.data?["stderr_tail"])
    }

    // MARK: - AgentConfig.VersionCheckPolicy

    func testVersionCheckPolicyDefaultIsWarn() {
        let cfg = AgentConfig(binary: "agent")
        XCTAssertEqual(cfg.versionCheck, .warn)
    }

    func testVersionCheckPolicyCanBeSetToStrict() {
        let cfg = AgentConfig(binary: "agent", versionCheck: .strict)
        XCTAssertEqual(cfg.versionCheck, .strict)
    }

    func testVersionCheckPolicyCanBeSkipped() {
        let cfg = AgentConfig(binary: "agent", versionCheck: .skip)
        XCTAssertEqual(cfg.versionCheck, .skip)
    }
}
