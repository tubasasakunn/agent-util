import XCTest
@testable import AIAgent

final class JSONValueTests: XCTestCase {
    func testLiteralConversion() {
        let obj: JSONValue = [
            "name": "Alice",
            "age": 30,
            "active": true,
            "scores": [1, 2, 3],
            "meta": ["role": "admin"],
            "empty": nil,
        ]
        XCTAssertEqual(obj["name"]?.stringValue, "Alice")
        XCTAssertEqual(obj["age"]?.intValue, 30)
        XCTAssertEqual(obj["active"]?.boolValue, true)
        XCTAssertEqual(obj["scores"]?.arrayValue?.count, 3)
        XCTAssertEqual(obj["scores"]?[1]?.intValue, 2)
        XCTAssertEqual(obj["meta"]?["role"]?.stringValue, "admin")
        XCTAssertEqual(obj["empty"]?.isNull, true)
    }

    func testFromAny() {
        let raw: [String: Any] = [
            "a": 1,
            "b": "hi",
            "c": [true, false] as [Any],
            "d": NSNull(),
        ]
        let v = JSONValue.from(raw)
        XCTAssertEqual(v["a"]?.intValue, 1)
        XCTAssertEqual(v["b"]?.stringValue, "hi")
        XCTAssertEqual(v["c"]?.arrayValue?.count, 2)
        XCTAssertEqual(v["c"]?[0]?.boolValue, true)
        XCTAssertEqual(v["d"]?.isNull, true)
    }

    func testRoundTripJSON() throws {
        let original: JSONValue = ["x": 1, "y": ["a", "b"], "z": nil]
        let encoder = JSONEncoder()
        let data = try encoder.encode(original)
        let decoded = try JSONDecoder().decode(JSONValue.self, from: data)
        XCTAssertEqual(decoded["x"]?.intValue, 1)
        XCTAssertEqual(decoded["y"]?.arrayValue?.count, 2)
        XCTAssertEqual(decoded["z"]?.isNull, true)
    }

    func testToRaw() {
        let v: JSONValue = ["a": 1, "b": [2, 3]]
        guard let dict = v.toRaw() as? [String: Any] else {
            XCTFail("toRaw should return dictionary")
            return
        }
        XCTAssertNotNil(dict["a"])
    }
}
