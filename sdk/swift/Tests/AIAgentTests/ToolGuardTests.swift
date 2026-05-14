import XCTest
@testable import AIAgent

final class ToolGuardTests: XCTestCase {
    func testToolWireFormat() throws {
        let t = Tool(
            name: "read_file",
            description: "Read a UTF-8 text file",
            parameters: [
                "type": "object",
                "properties": ["path": ["type": "string"]],
                "required": ["path"],
            ],
            readOnly: true
        ) { _ in
            .string("ok")
        }
        let wire = t.toWire()
        XCTAssertEqual(wire["name"].stringValue, "read_file")
        XCTAssertEqual(wire["description"].stringValue, "Read a UTF-8 text file")
        XCTAssertEqual(wire["read_only"].boolValue, true)
        XCTAssertEqual(wire["parameters"]?["type"].stringValue, "object")
    }

    func testToolReturnCoercion() throws {
        XCTAssertEqual(coerceToolResult(.string("hi")).content, "hi")

        let structured = ToolExecuteResult(content: "x", isError: true)
        XCTAssertEqual(coerceToolResult(.structured(structured)), structured)

        XCTAssertEqual(coerceToolResult(.json(.null)).content, "")
        XCTAssertEqual(coerceToolResult(.json(.string("y"))).content, "y")
    }

    func testGuardStagesAndWire() throws {
        let inp = Guard.input(name: "no_secrets") { input in
            input.contains("password")
                ? GuardResult(decision: .deny, reason: "secret")
                : GuardResult(decision: .allow)
        }
        XCTAssertEqual(inp.stage, .input)
        XCTAssertEqual(inp.toWire()["stage"].stringValue, "input")

        let tc = Guard.toolCall(name: "fs_root_only") { _, _ in
            GuardResult(decision: .allow)
        }
        XCTAssertEqual(tc.stage, .toolCall)
        XCTAssertEqual(tc.toWire()["stage"].stringValue, "tool_call")

        let out = Guard.output(name: "pii") { _ in GuardResult(decision: .allow) }
        XCTAssertEqual(out.stage, .output)
        XCTAssertEqual(out.toWire()["stage"].stringValue, "output")
    }

    func testGuardInvocation() async throws {
        let inp = Guard.input(name: "no_secrets") { input in
            input.lowercased().contains("password")
                ? GuardResult(decision: .deny, reason: "looks like a secret")
                : GuardResult(decision: .allow)
        }
        let ctx = GuardCallContext(input: "share my password please", toolName: "", args: .object([:]), output: "")
        let r = try await inp.call(ctx)
        XCTAssertEqual(r.decision, .deny)
        XCTAssertEqual(r.reason, "looks like a secret")
    }

    func testVerifierInvocation() async throws {
        let v = Verifier(name: "non_empty") { _, _, result in
            VerifierResult(passed: !result.isEmpty, summary: "len=\(result.count)")
        }
        let r = try await v.call("any_tool", .object([:]), "hello")
        XCTAssertTrue(r.passed)
        XCTAssertEqual(r.summary, "len=5")
    }
}
