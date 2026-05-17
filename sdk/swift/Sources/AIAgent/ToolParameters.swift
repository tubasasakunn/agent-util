import Foundation

// MARK: - ToolParameter DSL (D4)
//
// JSON Schema を手書きしないで Tool.parameters を組み立てるための薄い DSL。
// Python の `@tool` デコレータ / JS の Zod スキーマに近い使い心地を目指す。
//
// 例:
//
//   let tool = Tool(
//       name: "fetch",
//       description: "Fetch a URL",
//       parameters: ToolParameters {
//           StringParam("url").description("HTTPS URL").required()
//           IntParam("timeoutMs").description("ms").default_(5000)
//       }
//   ) { args in .text("ok") }
//
// 出力される JSON Schema:
//   {
//     "type": "object",
//     "properties": {
//       "url":       {"type": "string", "description": "HTTPS URL"},
//       "timeoutMs": {"type": "integer", "description": "ms", "default": 5000}
//     },
//     "required": ["url"],
//     "additionalProperties": false
//   }
//
// Swift Macros (@AgentTool) による関数シグネチャ自動抽出は別タスク。

// MARK: - パラメータ仕様

/// 1 つのプロパティの JSON Schema 部分。
public struct ToolParameterSpec: Sendable {
    public let name: String
    public var schema: JSONValue
    public var isRequired: Bool

    public init(name: String, schema: JSONValue, isRequired: Bool = false) {
        self.name = name
        self.schema = schema
        self.isRequired = isRequired
    }

    /// `description` を schema に追加した複製を返す (chainable)。
    public func description(_ desc: String) -> ToolParameterSpec {
        var dict = schema.objectValue ?? [:]
        dict["description"] = .string(desc)
        var copy = self
        copy.schema = .object(dict)
        return copy
    }

    /// `default` を schema に追加した複製を返す (chainable)。
    public func default_(_ value: JSONValue) -> ToolParameterSpec {
        var dict = schema.objectValue ?? [:]
        dict["default"] = value
        var copy = self
        copy.schema = .object(dict)
        return copy
    }

    /// `enum: [...]` を schema に追加した複製を返す。
    public func `enum`(_ values: [JSONValue]) -> ToolParameterSpec {
        var dict = schema.objectValue ?? [:]
        dict["enum"] = .array(values)
        var copy = self
        copy.schema = .object(dict)
        return copy
    }

    /// 必須にした複製を返す。
    public func required(_ value: Bool = true) -> ToolParameterSpec {
        var copy = self
        copy.isRequired = value
        return copy
    }
}

// MARK: - ファクトリ

/// `{ "type": "string" }` を生成する。
public func StringParam(_ name: String) -> ToolParameterSpec {
    ToolParameterSpec(name: name, schema: .object(["type": .string("string")]))
}

/// `{ "type": "integer" }` を生成する。
public func IntParam(_ name: String) -> ToolParameterSpec {
    ToolParameterSpec(name: name, schema: .object(["type": .string("integer")]))
}

/// `{ "type": "number" }` を生成する。
public func NumberParam(_ name: String) -> ToolParameterSpec {
    ToolParameterSpec(name: name, schema: .object(["type": .string("number")]))
}

/// `{ "type": "boolean" }` を生成する。
public func BoolParam(_ name: String) -> ToolParameterSpec {
    ToolParameterSpec(name: name, schema: .object(["type": .string("boolean")]))
}

/// 配列。itemsType (string/integer 等) で要素の型を指定する。
public func ArrayParam(_ name: String, itemsType: String = "string") -> ToolParameterSpec {
    ToolParameterSpec(
        name: name,
        schema: .object([
            "type": .string("array"),
            "items": .object(["type": .string(itemsType)]),
        ])
    )
}

/// 任意の JSON object。`properties` を子 DSL で組みたい場合に使う。
public func ObjectParam(
    _ name: String,
    @ToolParametersBuilder _ build: () -> [ToolParameterSpec] = { [] }
) -> ToolParameterSpec {
    let children = build()
    var properties: [String: JSONValue] = [:]
    var required: [JSONValue] = []
    for c in children {
        properties[c.name] = c.schema
        if c.isRequired { required.append(.string(c.name)) }
    }
    var schema: [String: JSONValue] = [
        "type": .string("object"),
        "properties": .object(properties),
        "additionalProperties": .bool(false),
    ]
    if !required.isEmpty { schema["required"] = .array(required) }
    return ToolParameterSpec(name: name, schema: .object(schema))
}

// MARK: - result builder

/// `ToolParameters { ... }` の中で `ToolParameterSpec` をリスト化する builder。
@resultBuilder
public enum ToolParametersBuilder {
    public static func buildBlock(_ specs: ToolParameterSpec...) -> [ToolParameterSpec] {
        Array(specs)
    }

    public static func buildArray(_ specs: [[ToolParameterSpec]]) -> [ToolParameterSpec] {
        specs.flatMap { $0 }
    }

    public static func buildOptional(_ specs: [ToolParameterSpec]?) -> [ToolParameterSpec] {
        specs ?? []
    }

    public static func buildEither(first specs: [ToolParameterSpec]) -> [ToolParameterSpec] {
        specs
    }

    public static func buildEither(second specs: [ToolParameterSpec]) -> [ToolParameterSpec] {
        specs
    }
}

// MARK: - エントリーポイント

/// 子パラメータ仕様から JSON Schema (object) を生成して `JSONValue` を返す。
///
/// `Tool(parameters:)` の引数として直接渡せる。
public func ToolParameters(
    additionalProperties: Bool = false,
    @ToolParametersBuilder _ build: () -> [ToolParameterSpec]
) -> JSONValue {
    let children = build()
    var properties: [String: JSONValue] = [:]
    var required: [JSONValue] = []
    for c in children {
        properties[c.name] = c.schema
        if c.isRequired { required.append(.string(c.name)) }
    }
    var schema: [String: JSONValue] = [
        "type": .string("object"),
        "properties": .object(properties),
        "additionalProperties": .bool(additionalProperties),
    ]
    if !required.isEmpty { schema["required"] = .array(required) }
    return .object(schema)
}
