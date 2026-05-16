import XCTest
@testable import AIAgent

final class ToolTests: XCTestCase {
    func testToolProtocolDict() {
        let tool = Tool(
            name: "echo",
            description: "Echo input",
            parameters: .object([
                "type": .string("object"),
                "properties": .object([
                    "msg": .object(["type": .string("string")]),
                ]),
            ]),
            readOnly: true
        ) { args in
            .text(args["msg"]?.stringValue ?? "")
        }
        let dict = tool.toProtocolDict()
        XCTAssertEqual(dict["name"]?.stringValue, "echo")
        XCTAssertEqual(dict["description"]?.stringValue, "Echo input")
        XCTAssertEqual(dict["read_only"]?.boolValue, true)
    }

    func testToolReturnCoercion() {
        let text = ToolReturn.text("hello")
        let result = text.toResult()
        XCTAssertEqual(result["content"]?.stringValue, "hello")
        XCTAssertEqual(result["is_error"]?.boolValue, false)

        let structured = ToolReturn.structured(content: "oops", isError: true)
        let r2 = structured.toResult()
        XCTAssertEqual(r2["content"]?.stringValue, "oops")
        XCTAssertEqual(r2["is_error"]?.boolValue, true)
    }

    func testGuardSpecHelpers() async {
        let inputGuard = GuardSpec.input(name: "no_secrets") { input in
            input.contains("secret")
                ? GuardOutcome.deny("contains secret")
                : GuardOutcome.allow
        }
        let outcome = try! await inputGuard.handler(
            GuardInput(input: "this has secret", toolName: "", args: .object([:]), output: "")
        )
        XCTAssertEqual(outcome.decision, .deny)
        XCTAssertEqual(outcome.reason, "contains secret")
    }
}
