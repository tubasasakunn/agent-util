/// A first-class `Sendable` representation of any JSON value.
///
/// `JSONSerialization` returns `Any`, which is awkward to thread through
/// async / `Sendable` boundaries. ``JSONValue`` lets the SDK keep its
/// public API strict-concurrency-friendly while still passing arbitrary
/// payloads to/from the Go core.
///
/// Construct via the standard literal protocols:
///
/// ```swift
/// let schema: JSONValue = [
///     "type": "object",
///     "properties": ["path": ["type": "string"]],
///     "required": ["path"]
/// ]
/// ```
import Foundation

public enum JSONValue: Sendable, Equatable {
    case null
    case bool(Bool)
    case int(Int64)
    case double(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])
}

extension JSONValue {
    /// Convert a Foundation value (as produced by `JSONSerialization`) into a
    /// ``JSONValue``. Throws ``AgentError`` if `any` contains a non-JSON type.
    public static func from(_ any: Any?) throws -> JSONValue {
        guard let any else { return .null }
        if any is NSNull { return .null }
        if let b = any as? Bool { return .bool(b) }
        if let n = any as? NSNumber {
            // NSNumber spans bool/int/double on Apple platforms; tell them apart.
            #if canImport(Darwin)
            let typeCh = String(cString: n.objCType)
            if typeCh == "c" || typeCh == "B" { return .bool(n.boolValue) }
            #endif
            if CFNumberIsFloatType(n) {
                return .double(n.doubleValue)
            }
            return .int(n.int64Value)
        }
        if let i = any as? Int { return .int(Int64(i)) }
        if let i = any as? Int64 { return .int(i) }
        if let d = any as? Double { return .double(d) }
        if let s = any as? String { return .string(s) }
        if let arr = any as? [Any?] {
            return .array(try arr.map { try JSONValue.from($0) })
        }
        if let dict = any as? [String: Any?] {
            var out: [String: JSONValue] = [:]
            out.reserveCapacity(dict.count)
            for (k, v) in dict {
                out[k] = try JSONValue.from(v)
            }
            return .object(out)
        }
        if let dict = any as? [String: Any] {
            var out: [String: JSONValue] = [:]
            out.reserveCapacity(dict.count)
            for (k, v) in dict {
                out[k] = try JSONValue.from(v)
            }
            return .object(out)
        }
        throw AgentError("JSONValue.from: unsupported type \(type(of: any))")
    }

    /// Project to the Foundation tree that `JSONSerialization.data(withJSONObject:)`
    /// accepts. `null` becomes `NSNull()`.
    public func toFoundation() -> Any {
        switch self {
        case .null: return NSNull()
        case .bool(let b): return b
        case .int(let i): return NSNumber(value: i)
        case .double(let d): return NSNumber(value: d)
        case .string(let s): return s
        case .array(let arr): return arr.map { $0.toFoundation() }
        case .object(let obj):
            var out: [String: Any] = [:]
            out.reserveCapacity(obj.count)
            for (k, v) in obj { out[k] = v.toFoundation() }
            return out
        }
    }

    /// Encode this value to a compact UTF-8 JSON string.
    public func encodedString() throws -> String {
        let foundation = self.toFoundation()
        let data = try JSONSerialization.data(
            withJSONObject: foundation,
            options: [.fragmentsAllowed, .withoutEscapingSlashes]
        )
        return String(data: data, encoding: .utf8) ?? ""
    }

    /// Decode a JSON-encoded UTF-8 string into a ``JSONValue``.
    public static func decode(_ text: String) throws -> JSONValue {
        guard let data = text.data(using: .utf8) else {
            throw AgentError("JSONValue.decode: invalid UTF-8")
        }
        return try decode(data)
    }

    public static func decode(_ data: Data) throws -> JSONValue {
        let parsed = try JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed])
        return try JSONValue.from(parsed)
    }
}

// MARK: - Convenience accessors

extension JSONValue {
    public var isNull: Bool { if case .null = self { return true } else { return false } }

    public var stringValue: String? {
        if case .string(let s) = self { return s } else { return nil }
    }

    public var boolValue: Bool? {
        if case .bool(let b) = self { return b } else { return nil }
    }

    public var intValue: Int64? {
        switch self {
        case .int(let i): return i
        case .double(let d): return Int64(d)
        default: return nil
        }
    }

    public var doubleValue: Double? {
        switch self {
        case .double(let d): return d
        case .int(let i): return Double(i)
        default: return nil
        }
    }

    public var arrayValue: [JSONValue]? {
        if case .array(let a) = self { return a } else { return nil }
    }

    public var objectValue: [String: JSONValue]? {
        if case .object(let o) = self { return o } else { return nil }
    }

    public subscript(key: String) -> JSONValue? {
        get {
            if case .object(let o) = self { return o[key] } else { return nil }
        }
        set {
            guard case .object(var o) = self else { return }
            o[key] = newValue
            self = .object(o)
        }
    }

    /// Recursively drop `.null` entries from objects, mirroring the
    /// `omitempty` behaviour of the Go core when serialising configs.
    public func stripNulls() -> JSONValue {
        switch self {
        case .object(let dict):
            var out: [String: JSONValue] = [:]
            out.reserveCapacity(dict.count)
            for (k, v) in dict {
                if case .null = v { continue }
                out[k] = v.stripNulls()
            }
            return .object(out)
        case .array(let arr):
            return .array(arr.map { $0.stripNulls() })
        default:
            return self
        }
    }
}

// MARK: - Optional convenience

/// Accessors on `Optional<JSONValue>` so call sites don't have to chase a
/// double-optional (`raw["x"]?.intValue` would otherwise be `Int64??`).
extension Optional where Wrapped == JSONValue {
    public var isNull: Bool {
        switch self {
        case .none: return true
        case .some(let v): return v.isNull
        }
    }
    public var stringValue: String? { self?.stringValue }
    public var boolValue: Bool? { self?.boolValue }
    public var intValue: Int64? { self?.intValue }
    public var doubleValue: Double? { self?.doubleValue }
    public var arrayValue: [JSONValue]? { self?.arrayValue }
    public var objectValue: [String: JSONValue]? { self?.objectValue }
}

// MARK: - Literal conformances

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
        dict.reserveCapacity(elements.count)
        for (k, v) in elements { dict[k] = v }
        self = .object(dict)
    }
}
