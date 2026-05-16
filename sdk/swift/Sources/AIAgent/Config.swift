import Foundation

// MARK: - サブコンフィグ群 (docs/schemas/*.json を反映)

public struct DelegateConfig: Sendable, Codable {
    public var enabled: Bool?
    public var maxChars: Int?

    public init(enabled: Bool? = nil, maxChars: Int? = nil) {
        self.enabled = enabled
        self.maxChars = maxChars
    }

    enum CodingKeys: String, CodingKey {
        case enabled
        case maxChars = "max_chars"
    }
}

public struct CoordinatorConfig: Sendable, Codable {
    public var enabled: Bool?
    public var maxChars: Int?

    public init(enabled: Bool? = nil, maxChars: Int? = nil) {
        self.enabled = enabled
        self.maxChars = maxChars
    }

    enum CodingKeys: String, CodingKey {
        case enabled
        case maxChars = "max_chars"
    }
}

public struct CompactionConfig: Sendable, Codable {
    public var enabled: Bool?
    public var budgetMaxChars: Int?
    public var keepLast: Int?
    public var targetRatio: Double?
    public var summarizer: String?

    public init(
        enabled: Bool? = nil,
        budgetMaxChars: Int? = nil,
        keepLast: Int? = nil,
        targetRatio: Double? = nil,
        summarizer: String? = nil
    ) {
        self.enabled = enabled
        self.budgetMaxChars = budgetMaxChars
        self.keepLast = keepLast
        self.targetRatio = targetRatio
        self.summarizer = summarizer
    }

    enum CodingKeys: String, CodingKey {
        case enabled
        case budgetMaxChars = "budget_max_chars"
        case keepLast = "keep_last"
        case targetRatio = "target_ratio"
        case summarizer
    }
}

public struct PermissionConfig: Sendable, Codable {
    public var enabled: Bool?
    public var deny: [String]?
    public var allow: [String]?

    public init(enabled: Bool? = nil, deny: [String]? = nil, allow: [String]? = nil) {
        self.enabled = enabled
        self.deny = deny
        self.allow = allow
    }
}

public struct GuardsConfig: Sendable, Codable {
    public var input: [String]?
    public var toolCall: [String]?
    public var output: [String]?

    public init(input: [String]? = nil, toolCall: [String]? = nil, output: [String]? = nil) {
        self.input = input
        self.toolCall = toolCall
        self.output = output
    }

    enum CodingKeys: String, CodingKey {
        case input
        case toolCall = "tool_call"
        case output
    }
}

public struct VerifyConfig: Sendable, Codable {
    public var verifiers: [String]?
    public var maxStepRetries: Int?
    public var maxConsecutiveFailures: Int?

    public init(
        verifiers: [String]? = nil,
        maxStepRetries: Int? = nil,
        maxConsecutiveFailures: Int? = nil
    ) {
        self.verifiers = verifiers
        self.maxStepRetries = maxStepRetries
        self.maxConsecutiveFailures = maxConsecutiveFailures
    }

    enum CodingKeys: String, CodingKey {
        case verifiers
        case maxStepRetries = "max_step_retries"
        case maxConsecutiveFailures = "max_consecutive_failures"
    }
}

public struct ToolScopeConfig: Sendable, Codable {
    public var maxTools: Int?
    public var includeAlways: [String]?

    public init(maxTools: Int? = nil, includeAlways: [String]? = nil) {
        self.maxTools = maxTools
        self.includeAlways = includeAlways
    }

    enum CodingKeys: String, CodingKey {
        case maxTools = "max_tools"
        case includeAlways = "include_always"
    }
}

public struct ReminderConfig: Sendable, Codable {
    public var threshold: Int?
    public var content: String?

    public init(threshold: Int? = nil, content: String? = nil) {
        self.threshold = threshold
        self.content = content
    }
}

public struct StreamingConfig: Sendable, Codable {
    public var enabled: Bool?
    public var contextStatus: Bool?

    public init(enabled: Bool? = nil, contextStatus: Bool? = nil) {
        self.enabled = enabled
        self.contextStatus = contextStatus
    }

    enum CodingKeys: String, CodingKey {
        case enabled
        case contextStatus = "context_status"
    }
}

public struct LoopConfig: Sendable, Codable {
    public var type: String

    public init(type: String = "react") {
        self.type = type
    }
}

public struct RouterConfig: Sendable, Codable {
    public var endpoint: String?
    public var model: String?
    public var apiKey: String?

    public init(endpoint: String? = nil, model: String? = nil, apiKey: String? = nil) {
        self.endpoint = endpoint
        self.model = model
        self.apiKey = apiKey
    }

    enum CodingKeys: String, CodingKey {
        case endpoint
        case model
        case apiKey = "api_key"
    }
}

public struct JudgeConfig: Sendable, Codable {
    public var name: String

    public init(name: String = "") {
        self.name = name
    }
}

/// メインLLMドライバの設定。
/// `mode = .remote` を指定すると、すべての ChatCompletion 呼び出しが
/// `llm.execute` 経由でラッパーに委譲され、任意 API 形式 (Anthropic /
/// Bedrock / ollama / mock 等) に変換できる。
public struct LLMConfig: Sendable, Codable {
    public enum Mode: String, Sendable, Codable {
        case http
        case remote
    }

    public var mode: Mode?
    public var timeoutSeconds: Int?

    public init(mode: Mode? = nil, timeoutSeconds: Int? = nil) {
        self.mode = mode
        self.timeoutSeconds = timeoutSeconds
    }

    enum CodingKeys: String, CodingKey {
        case mode
        case timeoutSeconds = "timeout_seconds"
    }
}

// MARK: - CoreAgentConfig (agent.configure RPCのパラメータ)

/// `agent.configure` JSON-RPCに渡すコア設定。
/// 高レベル `AgentConfig` がバイナリ設定と挙動設定を集約し、内部でこれに変換される。
public struct CoreAgentConfig: Sendable {
    public var maxTurns: Int?
    public var systemPrompt: String?
    public var tokenLimit: Int?
    public var workDir: String?

    public var delegate: DelegateConfig?
    public var coordinator: CoordinatorConfig?
    public var compaction: CompactionConfig?
    public var permission: PermissionConfig?
    public var guards: GuardsConfig?
    public var verify: VerifyConfig?
    public var toolScope: ToolScopeConfig?
    public var reminder: ReminderConfig?
    public var streaming: StreamingConfig?
    public var loop: LoopConfig?
    public var router: RouterConfig?
    public var judge: JudgeConfig?
    public var llm: LLMConfig?

    public init(
        maxTurns: Int? = nil,
        systemPrompt: String? = nil,
        tokenLimit: Int? = nil,
        workDir: String? = nil,
        delegate: DelegateConfig? = nil,
        coordinator: CoordinatorConfig? = nil,
        compaction: CompactionConfig? = nil,
        permission: PermissionConfig? = nil,
        guards: GuardsConfig? = nil,
        verify: VerifyConfig? = nil,
        toolScope: ToolScopeConfig? = nil,
        reminder: ReminderConfig? = nil,
        streaming: StreamingConfig? = nil,
        loop: LoopConfig? = nil,
        router: RouterConfig? = nil,
        judge: JudgeConfig? = nil,
        llm: LLMConfig? = nil
    ) {
        self.maxTurns = maxTurns
        self.systemPrompt = systemPrompt
        self.tokenLimit = tokenLimit
        self.workDir = workDir
        self.delegate = delegate
        self.coordinator = coordinator
        self.compaction = compaction
        self.permission = permission
        self.guards = guards
        self.verify = verify
        self.toolScope = toolScope
        self.reminder = reminder
        self.streaming = streaming
        self.loop = loop
        self.router = router
        self.judge = judge
        self.llm = llm
    }

    /// JSON-RPC `params` 辞書へ変換 (`None` は省略=omitempty)。
    public func toParams() -> JSONValue {
        var obj: [String: JSONValue] = [:]
        if let v = maxTurns { obj["max_turns"] = .int(Int64(v)) }
        if let v = systemPrompt { obj["system_prompt"] = .string(v) }
        if let v = tokenLimit { obj["token_limit"] = .int(Int64(v)) }
        if let v = workDir { obj["work_dir"] = .string(v) }
        if let v = delegate { obj["delegate"] = encodeToJSON(v) }
        if let v = coordinator { obj["coordinator"] = encodeToJSON(v) }
        if let v = compaction { obj["compaction"] = encodeToJSON(v) }
        if let v = permission { obj["permission"] = encodeToJSON(v) }
        if let v = guards { obj["guards"] = encodeToJSON(v) }
        if let v = verify { obj["verify"] = encodeToJSON(v) }
        if let v = toolScope { obj["tool_scope"] = encodeToJSON(v) }
        if let v = reminder { obj["reminder"] = encodeToJSON(v) }
        if let v = streaming { obj["streaming"] = encodeToJSON(v) }
        if let v = loop { obj["loop"] = encodeToJSON(v) }
        if let v = router { obj["router"] = encodeToJSON(v) }
        if let v = judge { obj["judge"] = encodeToJSON(v) }
        if let v = llm { obj["llm"] = encodeToJSON(v) }
        return .object(obj)
    }
}

/// Codable準拠オブジェクトをJSONValueに変換 (`nil` キーを除去)。
func encodeToJSON<T: Encodable>(_ value: T) -> JSONValue {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    guard let data = try? encoder.encode(value),
          let json = try? JSONDecoder().decode(JSONValue.self, from: data) else {
        return .null
    }
    return stripNull(json)
}

/// 値が `.null` のオブジェクトキーを再帰的に削除する。
func stripNull(_ value: JSONValue) -> JSONValue {
    switch value {
    case .object(let dict):
        var out: [String: JSONValue] = [:]
        for (k, v) in dict {
            if case .null = v { continue }
            out[k] = stripNull(v)
        }
        return .object(out)
    case .array(let arr):
        return .array(arr.map { stripNull($0) })
    default:
        return value
    }
}
