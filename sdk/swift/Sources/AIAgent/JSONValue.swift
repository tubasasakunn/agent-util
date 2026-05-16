import Foundation

/// 動的JSON値を表現する列挙体。Codable準拠で、JSON-RPCのparams/resultやツール引数を扱う。
public enum JSONValue: Sendable, Equatable {
    case null
    case bool(Bool)
    case int(Int64)
    case double(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    // MARK: - リテラル風コンストラクタ

    public static func number(_ value: Double) -> JSONValue { .double(value) }
    public static func number(_ value: Int) -> JSONValue { .int(Int64(value)) }

    // MARK: - 取得アクセサ

    public var stringValue: String? {
        if case .string(let s) = self { return s }
        return nil
    }

    public var intValue: Int? {
        switch self {
        case .int(let n): return Int(n)
        case .double(let d): return Int(d)
        default: return nil
        }
    }

    public var doubleValue: Double? {
        switch self {
        case .int(let n): return Double(n)
        case .double(let d): return d
        default: return nil
        }
    }

    public var boolValue: Bool? {
        if case .bool(let b) = self { return b }
        return nil
    }

    public var arrayValue: [JSONValue]? {
        if case .array(let a) = self { return a }
        return nil
    }

    public var objectValue: [String: JSONValue]? {
        if case .object(let o) = self { return o }
        return nil
    }

    public var isNull: Bool {
        if case .null = self { return true }
        return false
    }

    public subscript(key: String) -> JSONValue? {
        if case .object(let dict) = self {
            return dict[key]
        }
        return nil
    }

    public subscript(index: Int) -> JSONValue? {
        if case .array(let arr) = self, index >= 0, index < arr.count {
            return arr[index]
        }
        return nil
    }

    // MARK: - 任意の値からの生成

    public static func from(_ value: Any?) -> JSONValue {
        guard let value = value else { return .null }
        if let v = value as? JSONValue { return v }
        if value is NSNull { return .null }
        // NSNumber を Bool/Int/Double に正しく振り分ける。
        // `as? Bool` は NSNumber(0/1) にもマッチするため、必ず objCType を見る。
        if let n = value as? NSNumber {
            let type = String(cString: n.objCType)
            switch type {
            case "c", "B":
                return .bool(n.boolValue)
            case "f", "d":
                return .double(n.doubleValue)
            default:
                return .int(n.int64Value)
            }
        }
        if let v = value as? Bool { return .bool(v) }
        if let v = value as? Int64 { return .int(v) }
        if let v = value as? Int { return .int(Int64(v)) }
        if let v = value as? Double { return .double(v) }
        if let v = value as? Float { return .double(Double(v)) }
        if let v = value as? String { return .string(v) }
        if let v = value as? [Any?] { return .array(v.map { JSONValue.from($0) }) }
        if let v = value as? [String: Any?] {
            var dict: [String: JSONValue] = [:]
            for (k, val) in v { dict[k] = JSONValue.from(val) }
            return .object(dict)
        }
        return .string(String(describing: value))
    }

    /// JSON互換のSwiftプリミティブ (NSNull/Bool/Int/Double/String/Array/Dictionary) に変換する。
    public func toRaw() -> Any {
        switch self {
        case .null: return NSNull()
        case .bool(let b): return b
        case .int(let n): return n
        case .double(let d): return d
        case .string(let s): return s
        case .array(let arr): return arr.map { $0.toRaw() }
        case .object(let obj):
            var dict: [String: Any] = [:]
            for (k, v) in obj { dict[k] = v.toRaw() }
            return dict
        }
    }
}

extension JSONValue: Codable {
    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let b = try? container.decode(Bool.self) {
            self = .bool(b)
        } else if let n = try? container.decode(Int64.self) {
            self = .int(n)
        } else if let d = try? container.decode(Double.self) {
            self = .double(d)
        } else if let s = try? container.decode(String.self) {
            self = .string(s)
        } else if let a = try? container.decode([JSONValue].self) {
            self = .array(a)
        } else if let o = try? container.decode([String: JSONValue].self) {
            self = .object(o)
        } else {
            throw DecodingError.dataCorruptedError(
                in: container,
                debugDescription: "Cannot decode JSONValue"
            )
        }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .null: try container.encodeNil()
        case .bool(let b): try container.encode(b)
        case .int(let n): try container.encode(n)
        case .double(let d): try container.encode(d)
        case .string(let s): try container.encode(s)
        case .array(let a): try container.encode(a)
        case .object(let o): try container.encode(o)
        }
    }
}

extension JSONValue: ExpressibleByNilLiteral {
    public init(nilLiteral: ()) { self = .null }
}

extension JSONValue: ExpressibleByBooleanLiteral {
    public init(booleanLiteral value: Bool) { self = .bool(value) }
}

extension JSONValue: ExpressibleByIntegerLiteral {
    public init(integerLiteral value: Int64) { self = .int(value) }
}

extension JSONValue: ExpressibleByFloatLiteral {
    public init(floatLiteral value: Double) { self = .double(value) }
}

extension JSONValue: ExpressibleByStringLiteral {
    public init(stringLiteral value: String) { self = .string(value) }
}

extension JSONValue: ExpressibleByArrayLiteral {
    public init(arrayLiteral elements: JSONValue...) { self = .array(elements) }
}

extension JSONValue: ExpressibleByDictionaryLiteral {
    public init(dictionaryLiteral elements: (String, JSONValue)...) {
        var dict: [String: JSONValue] = [:]
        for (k, v) in elements { dict[k] = v }
        self = .object(dict)
    }
}
