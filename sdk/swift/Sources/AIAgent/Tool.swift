/// `ToolDefinition` — declarative description of a wrapper-side tool that
/// the ``Agent`` registers with the Go core.
///
/// Unlike Python, Swift has only weak run-time type info, so callers must
/// pass a JSON Schema for the tool parameters explicitly. The handler
/// receives the parsed `args` value and returns either a plain `String`,
/// a structured ``ToolExecuteResult``, or any ``JSONValue`` that will be
/// coerced to a string.
///
/// ```swift
/// let readFile = Tool(
///     name: "read_file",
///     description: "Read a UTF-8 text file from the workspace.",
///     parameters: [
///         "type": "object",
///         "properties": ["path": ["type": "string"]],
///         "required": ["path"],
///         "additionalProperties": false,
///     ],
///     readOnly: true
/// ) { args in
///     let path = args["path"]?.stringValue ?? ""
///     return try String(contentsOfFile: path)
/// }
/// ```
import Foundation

/// Shape of the result the core expects (matches `ToolExecuteResult.json`).
public struct ToolExecuteResult: Sendable, Equatable {
    public var content: String
    public var isError: Bool
    public var metadata: JSONValue?

    public init(content: String, isError: Bool = false, metadata: JSONValue? = nil) {
        self.content = content
        self.isError = isError
        self.metadata = metadata
    }

    public func toWire() -> JSONValue {
        var d: [String: JSONValue] = [
            "content": .string(content),
            "is_error": .bool(isError),
        ]
        if let metadata { d["metadata"] = metadata }
        return .object(d)
    }
}

/// What a ``Tool/handler`` may return; non-`ToolExecuteResult` values are
/// coerced to a string-only result.
public enum ToolReturn: Sendable {
    case string(String)
    case structured(ToolExecuteResult)
    case json(JSONValue)
}

extension ToolReturn: ExpressibleByStringLiteral {
    public init(stringLiteral value: String) { self = .string(value) }
}

public typealias ToolHandler = @Sendable (JSONValue) async throws -> ToolReturn

public struct Tool: Sendable {
    public let name: String
    public let description: String
    public let parameters: JSONValue
    public let readOnly: Bool
    public let handler: ToolHandler

    public init(
        name: String,
        description: String,
        parameters: JSONValue = .object(["type": .string("object")]),
        readOnly: Bool = false,
        handler: @escaping ToolHandler
    ) {
        precondition(!name.isEmpty, "Tool name must not be empty")
        self.name = name
        self.description = description
        self.parameters = parameters
        self.readOnly = readOnly
        self.handler = handler
    }

    /// Internal: project to the wire format used by `tool.register`.
    public func toWire() -> JSONValue {
        return .object([
            "name": .string(name),
            "description": .string(description),
            "parameters": parameters,
            "read_only": .bool(readOnly),
        ])
    }
}

/// Internal: normalise a ``ToolReturn`` into a ``ToolExecuteResult``.
public func coerceToolResult(_ raw: ToolReturn) -> ToolExecuteResult {
    switch raw {
    case .string(let s):
        return ToolExecuteResult(content: s)
    case .structured(let r):
        return r
    case .json(let v):
        switch v {
        case .string(let s):
            return ToolExecuteResult(content: s)
        case .null:
            return ToolExecuteResult(content: "")
        default:
            // Best effort: stringify other JSON values.
            let text = (try? v.encodedString()) ?? ""
            return ToolExecuteResult(content: text)
        }
    }
}
