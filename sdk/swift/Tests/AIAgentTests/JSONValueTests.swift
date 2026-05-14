import XCTest
@testable import AIAgent

final class JSONValueTests: XCTestCase {
    func testLiteralConstruction() throws {
        let v: JSONValue = [
            "name": "agent",
            "count": 3,
            "ratio": 0.5,
            "enabled": true,
            "tags": ["alpha", "beta"],
            "extra": nil,
        ]
        XCTAssertEqual(v["name"].stringValue, "agent")
        XCTAssertEqual(v["count"].intValue, 3)
        XCTAssertEqual(v["ratio"].doubleValue, 0.5)
        XCTAssertEqual(v["enabled"].boolValue, true)
        XCTAssertEqual(v["tags"].arrayValue?.count, 2)
        XCTAssertTrue(v["extra"].isNull)
    }

    func testRoundTrip() throws {
        let v: JSONValue = .object([
            "a": .int(1),
            "b": .string("hi"),
            "c": .array([.bool(true), .null, .double(2.5)]),
        ])
        let encoded = try v.encodedString()
        let decoded = try JSONValue.decode(encoded)
        XCTAssertEqual(decoded, v)
    }

    func testStripNulls() throws {
        let v: JSONValue = .object([
            "keep": .string("yes"),
            "drop": .null,
            "nested": .object([
                "inner_keep": .int(1),
                "inner_drop": .null,
            ]),
        ])
        let stripped = v.stripNulls()
        XCTAssertEqual(stripped["keep"].stringValue, "yes")
        XCTAssertNil(stripped["drop"])
        XCTAssertNil(stripped["nested"]?["inner_drop"])
        XCTAssertEqual(stripped["nested"]?["inner_keep"].intValue, 1)
    }

    func testFromFoundation() throws {
        let raw: [String: Any] = [
            "n": 42,
            "s": "x",
            "arr": [1, 2, 3],
            "obj": ["k": true],
        ]
        let v = try JSONValue.from(raw)
        XCTAssertEqual(v["n"].intValue, 42)
        XCTAssertEqual(v["s"].stringValue, "x")
        XCTAssertEqual(v["arr"].arrayValue?.count, 3)
        XCTAssertEqual(v["obj"]?["k"].boolValue, true)
    }

    func testDecodePrimitives() throws {
        XCTAssertEqual(try JSONValue.decode("true"), .bool(true))
        XCTAssertEqual(try JSONValue.decode("\"hello\""), .string("hello"))
        XCTAssertEqual(try JSONValue.decode("null"), .null)
        XCTAssertEqual(try JSONValue.decode("12345"), .int(12345))
    }
}
