import Foundation

/// ツール実行結果の返り値型。
public enum ToolReturn: Sendable {
    /// 単純な文字列を返す。
    case text(String)
    /// 構造化レスポンス (content, is_error, metadata)。
    case structured(content: String, isError: Bool = false, metadata: [String: JSONValue]? = nil)

    func toResult() -> JSONValue {
        switch self {
        case .text(let s):
            return .object([
                "content": .string(s),
                "is_error": .bool(false),
            ])
        case .structured(let content, let isError, let metadata):
            var out: [String: JSONValue] = [
                "content": .string(content),
                "is_error": .bool(isError),
            ]
            if let metadata = metadata {
                out["metadata"] = .object(metadata)
            }
            return .object(out)
        }
    }
}

public typealias ToolHandler = @Sendable (JSONValue) async throws -> ToolReturn

/// Agentに登録可能なツール定義。
///
/// 関数本体 (`handler`) + メタデータ (name/description/parameters) を保持する。
public struct Tool: Sendable {
    public let name: String
    public let description: String
    public let parameters: JSONValue
    public let readOnly: Bool
    public let handler: ToolHandler

    public init(
        name: String,
        description: String = "",
        parameters: JSONValue = .object([
            "type": .string("object"),
            "properties": .object([:]),
            "additionalProperties": .bool(false),
        ]),
        readOnly: Bool = false,
        handler: @escaping ToolHandler
    ) {
        self.name = name
        self.description = description
        self.parameters = parameters
        self.readOnly = readOnly
        self.handler = handler
    }

    func toProtocolDict() -> JSONValue {
        .object([
            "name": .string(name),
            "description": .string(description),
            "parameters": parameters,
            "read_only": .bool(readOnly),
        ])
    }
}

// MARK: - 内部: ToolDefinitionレジストリ

actor ToolRegistry {
    private var tools: [String: Tool] = [:]
    private(set) var stats: [String: (success: Int, error: Int)] = [:]

    func register(_ tool: Tool) {
        tools[tool.name] = tool
        if stats[tool.name] == nil { stats[tool.name] = (0, 0) }
    }

    func registerAll(_ tools: [Tool]) {
        for t in tools { register(t) }
    }

    func get(_ name: String) -> Tool? { tools[name] }

    func names() -> [String] { Array(tools.keys).sorted() }

    func recordSuccess(_ name: String) {
        var s = stats[name] ?? (0, 0)
        s.success += 1
        stats[name] = s
    }

    func recordError(_ name: String) {
        var s = stats[name] ?? (0, 0)
        s.error += 1
        stats[name] = s
    }
}

// MARK: - ツール結果の正規化

func coerceToolResult(_ raw: ToolReturn) -> JSONValue {
    raw.toResult()
}
