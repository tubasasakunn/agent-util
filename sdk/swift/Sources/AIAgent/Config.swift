/// Configuration types for `agent.configure`.
///
/// Mirrors `pkg/protocol.AgentConfigureParams` and the nested config
/// structs. The wire keys are `snake_case` so they pass straight through
/// to the Go core without translation, matching the Python and JS SDKs.
///
/// JSON-Schema sources (docs/schemas/*.json):
///
/// * `AgentConfigureParams.json`
/// * `DelegateConfig.json`
/// * `CoordinatorConfig.json`
/// * `CompactionConfig.json`
/// * `PermissionConfig.json`
/// * `GuardsConfig.json`
/// * `VerifyConfig.json`
/// * `ToolScopeConfig.json`
/// * `ReminderConfig.json`
/// * `StreamingConfig.json`
///
/// `nil` fields are stripped before serialisation so they behave like
/// Go's `omitempty` (the core keeps existing defaults).
import Foundation

public struct DelegateConfig: Sendable, Equatable {
    public var enabled: Bool?
    public var maxChars: Int?
    public init(enabled: Bool? = nil, maxChars: Int? = nil) {
        self.enabled = enabled
        self.maxChars = maxChars
    }
}

public struct CoordinatorConfig: Sendable, Equatable {
    public var enabled: Bool?
    public var maxChars: Int?
    public init(enabled: Bool? = nil, maxChars: Int? = nil) {
        self.enabled = enabled
        self.maxChars = maxChars
    }
}

public struct CompactionConfig: Sendable, Equatable {
    public var enabled: Bool?
    public var budgetMaxChars: Int?
    public var keepLast: Int?
    public var targetRatio: Double?
    /// `""` or `"llm"`.
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
}

public struct PermissionConfig: Sendable, Equatable {
    public var enabled: Bool?
    public var deny: [String]?
    public var allow: [String]?
    public init(enabled: Bool? = nil, deny: [String]? = nil, allow: [String]? = nil) {
        self.enabled = enabled
        self.deny = deny
        self.allow = allow
    }
}

public struct GuardsConfig: Sendable, Equatable {
    public var input: [String]?
    public var toolCall: [String]?
    public var output: [String]?
    public init(input: [String]? = nil, toolCall: [String]? = nil, output: [String]? = nil) {
        self.input = input
        self.toolCall = toolCall
        self.output = output
    }
}

public struct VerifyConfig: Sendable, Equatable {
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
}

public struct ToolScopeConfig: Sendable, Equatable {
    public var maxTools: Int?
    public var includeAlways: [String]?
    public init(maxTools: Int? = nil, includeAlways: [String]? = nil) {
        self.maxTools = maxTools
        self.includeAlways = includeAlways
    }
}

public struct ReminderConfig: Sendable, Equatable {
    public var threshold: Int?
    public var content: String?
    public init(threshold: Int? = nil, content: String? = nil) {
        self.threshold = threshold
        self.content = content
    }
}

public struct StreamingConfig: Sendable, Equatable {
    public var enabled: Bool?
    public var contextStatus: Bool?
    public init(enabled: Bool? = nil, contextStatus: Bool? = nil) {
        self.enabled = enabled
        self.contextStatus = contextStatus
    }
}

public struct AgentConfig: Sendable, Equatable {
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
        streaming: StreamingConfig? = nil
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
    }

    /// Project to the JSON-RPC `params` value.
    ///
    /// `nil` fields are omitted entirely, matching how the Go core treats
    /// absent fields (keep existing defaults). Nested config blocks are
    /// recursively cleaned the same way.
    public func toParams() -> JSONValue {
        var dict: [String: JSONValue] = [:]

        addInt(&dict, "max_turns", maxTurns)
        addString(&dict, "system_prompt", systemPrompt)
        addInt(&dict, "token_limit", tokenLimit)
        addString(&dict, "work_dir", workDir)

        if let delegate {
            dict["delegate"] = encodeDelegate(delegate)
        }
        if let coordinator {
            dict["coordinator"] = encodeCoordinator(coordinator)
        }
        if let compaction {
            dict["compaction"] = encodeCompaction(compaction)
        }
        if let permission {
            dict["permission"] = encodePermission(permission)
        }
        if let guards {
            dict["guards"] = encodeGuards(guards)
        }
        if let verify {
            dict["verify"] = encodeVerify(verify)
        }
        if let toolScope {
            dict["tool_scope"] = encodeToolScope(toolScope)
        }
        if let reminder {
            dict["reminder"] = encodeReminder(reminder)
        }
        if let streaming {
            dict["streaming"] = encodeStreaming(streaming)
        }
        return .object(dict)
    }
}

// MARK: - Internal encoders

private func addBool(_ dict: inout [String: JSONValue], _ key: String, _ v: Bool?) {
    if let v { dict[key] = .bool(v) }
}
private func addInt(_ dict: inout [String: JSONValue], _ key: String, _ v: Int?) {
    if let v { dict[key] = .int(Int64(v)) }
}
private func addDouble(_ dict: inout [String: JSONValue], _ key: String, _ v: Double?) {
    if let v { dict[key] = .double(v) }
}
private func addString(_ dict: inout [String: JSONValue], _ key: String, _ v: String?) {
    if let v { dict[key] = .string(v) }
}
private func addStringArray(_ dict: inout [String: JSONValue], _ key: String, _ v: [String]?) {
    if let v { dict[key] = .array(v.map { .string($0) }) }
}

private func encodeDelegate(_ c: DelegateConfig) -> JSONValue {
    var d: [String: JSONValue] = [:]
    addBool(&d, "enabled", c.enabled)
    addInt(&d, "max_chars", c.maxChars)
    return .object(d)
}

private func encodeCoordinator(_ c: CoordinatorConfig) -> JSONValue {
    var d: [String: JSONValue] = [:]
    addBool(&d, "enabled", c.enabled)
    addInt(&d, "max_chars", c.maxChars)
    return .object(d)
}

private func encodeCompaction(_ c: CompactionConfig) -> JSONValue {
    var d: [String: JSONValue] = [:]
    addBool(&d, "enabled", c.enabled)
    addInt(&d, "budget_max_chars", c.budgetMaxChars)
    addInt(&d, "keep_last", c.keepLast)
    addDouble(&d, "target_ratio", c.targetRatio)
    addString(&d, "summarizer", c.summarizer)
    return .object(d)
}

private func encodePermission(_ c: PermissionConfig) -> JSONValue {
    var d: [String: JSONValue] = [:]
    addBool(&d, "enabled", c.enabled)
    addStringArray(&d, "deny", c.deny)
    addStringArray(&d, "allow", c.allow)
    return .object(d)
}

private func encodeGuards(_ c: GuardsConfig) -> JSONValue {
    var d: [String: JSONValue] = [:]
    addStringArray(&d, "input", c.input)
    addStringArray(&d, "tool_call", c.toolCall)
    addStringArray(&d, "output", c.output)
    return .object(d)
}

private func encodeVerify(_ c: VerifyConfig) -> JSONValue {
    var d: [String: JSONValue] = [:]
    addStringArray(&d, "verifiers", c.verifiers)
    addInt(&d, "max_step_retries", c.maxStepRetries)
    addInt(&d, "max_consecutive_failures", c.maxConsecutiveFailures)
    return .object(d)
}

private func encodeToolScope(_ c: ToolScopeConfig) -> JSONValue {
    var d: [String: JSONValue] = [:]
    addInt(&d, "max_tools", c.maxTools)
    addStringArray(&d, "include_always", c.includeAlways)
    return .object(d)
}

private func encodeReminder(_ c: ReminderConfig) -> JSONValue {
    var d: [String: JSONValue] = [:]
    addInt(&d, "threshold", c.threshold)
    addString(&d, "content", c.content)
    return .object(d)
}

private func encodeStreaming(_ c: StreamingConfig) -> JSONValue {
    var d: [String: JSONValue] = [:]
    addBool(&d, "enabled", c.enabled)
    addBool(&d, "context_status", c.contextStatus)
    return .object(d)
}
