import XCTest
@testable import AIAgent

final class ToolParametersTests: XCTestCase {

    func testEmptySchemaIsValidObject() {
        let schema = ToolParameters {}
        XCTAssertEqual(schema["type"]?.stringValue, "object")
        XCTAssertEqual(schema["additionalProperties"]?.boolValue, false)
        // 空の properties は object {}
        XCTAssertEqual(schema["properties"]?.objectValue?.isEmpty, true)
        // required が空のときは出力されない
        XCTAssertNil(schema["required"])
    }

    func testStringParamWithDescriptionAndRequired() {
        let schema = ToolParameters {
            StringParam("url")
                .description("HTTPS URL")
                .required()
        }
        let url = schema["properties"]?["url"]
        XCTAssertEqual(url?["type"]?.stringValue, "string")
        XCTAssertEqual(url?["description"]?.stringValue, "HTTPS URL")
        XCTAssertEqual(schema["required"]?.arrayValue?[0].stringValue, "url")
    }

    func testIntParamWithDefault() {
        let schema = ToolParameters {
            IntParam("timeoutMs").default_(.int(5000))
        }
        let p = schema["properties"]?["timeoutMs"]
        XCTAssertEqual(p?["type"]?.stringValue, "integer")
        XCTAssertEqual(p?["default"]?.intValue, 5000)
    }

    func testBoolParam() {
        let schema = ToolParameters {
            BoolParam("verbose").default_(.bool(false))
        }
        let p = schema["properties"]?["verbose"]
        XCTAssertEqual(p?["type"]?.stringValue, "boolean")
        XCTAssertEqual(p?["default"]?.boolValue, false)
    }

    func testArrayParam() {
        let schema = ToolParameters {
            ArrayParam("tags", itemsType: "string").required()
        }
        let p = schema["properties"]?["tags"]
        XCTAssertEqual(p?["type"]?.stringValue, "array")
        XCTAssertEqual(p?["items"]?["type"]?.stringValue, "string")
        XCTAssertEqual(schema["required"]?.arrayValue?[0].stringValue, "tags")
    }

    func testEnumValues() {
        let schema = ToolParameters {
            StringParam("mode")
                .enum([.string("fast"), .string("safe")])
                .required()
        }
        let p = schema["properties"]?["mode"]
        XCTAssertEqual(p?["enum"]?.arrayValue?.count, 2)
        XCTAssertEqual(p?["enum"]?.arrayValue?[0].stringValue, "fast")
    }

    func testNestedObjectParam() {
        let schema = ToolParameters {
            ObjectParam("location") {
                NumberParam("lat").required()
                NumberParam("lng").required()
            }.required()
        }
        let loc = schema["properties"]?["location"]
        XCTAssertEqual(loc?["type"]?.stringValue, "object")
        XCTAssertEqual(loc?["properties"]?["lat"]?["type"]?.stringValue, "number")
        XCTAssertEqual(loc?["required"]?.arrayValue?.count, 2)
    }

    func testToolWithDSLParameters() {
        // 実用シナリオ: Tool に直接渡せる
        let tool = Tool(
            name: "fetch",
            description: "Fetch a URL",
            parameters: ToolParameters {
                StringParam("url").description("HTTPS URL").required()
                IntParam("timeoutMs").default_(.int(5000))
            }
        ) { _ in .text("ok") }

        let dict = tool.toProtocolDict()
        let params = dict["parameters"]
        XCTAssertEqual(params?["type"]?.stringValue, "object")
        XCTAssertNotNil(params?["properties"]?["url"])
        XCTAssertEqual(params?["required"]?.arrayValue?[0].stringValue, "url")
    }

    func testMultipleRequiredFields() {
        let schema = ToolParameters {
            StringParam("a").required()
            StringParam("b").required()
            StringParam("c")  // 任意
        }
        let req = schema["required"]?.arrayValue?.compactMap { $0.stringValue } ?? []
        XCTAssertEqual(Set(req), Set(["a", "b"]))
    }
}
