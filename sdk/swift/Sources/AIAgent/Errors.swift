import Foundation

/// ai-agent SDKの基底エラー。
public class AgentError: Error, @unchecked Sendable, CustomStringConvertible {
    public let message: String
    public let code: Int?
    public let data: JSONValue?

    public init(_ message: String, code: Int? = nil, data: JSONValue? = nil) {
        self.message = message
        self.code = code
        self.data = data
    }

    public var description: String {
        if let code = code {
            return "AgentError(code=\(code)): \(message)"
        }
        return "AgentError: \(message)"
    }
}

/// agent.run実行中に別のrunを呼んだ際に発生 (-32002)。
public final class AgentBusy: AgentError, @unchecked Sendable {}

/// agent.abortで中断された際に発生 (-32003)。
public final class AgentAborted: AgentError, @unchecked Sendable {}

/// ツール関連エラー (-32000 / -32001)。
public final class ToolError: AgentError, @unchecked Sendable {}

/// 入力/ツール呼出し/出力ガードが拒否した際に発生 (-32005)。
public class GuardDenied: AgentError, @unchecked Sendable {
    public let decision: String
    public let reason: String

    public init(
        _ message: String,
        decision: String = "deny",
        reason: String = "",
        code: Int? = nil,
        data: JSONValue? = nil
    ) {
        self.decision = decision
        self.reason = reason
        super.init(message, code: code, data: data)
    }

    public override var description: String {
        var parts = ["[\(decision)] \(message)"]
        if !reason.isEmpty { parts.append("reason: \(reason)") }
        return parts.joined(separator: " — ")
    }
}

/// トリップワイヤーガードが発火した際に発生 (-32006)。
public final class TripwireTriggered: GuardDenied, @unchecked Sendable {
    public init(_ message: String, reason: String = "", code: Int? = nil, data: JSONValue? = nil) {
        super.init(message, decision: "tripwire", reason: reason, code: code, data: data)
    }
}

// MARK: - エラーコード→クラスへのマッピング

enum RpcErrorCode {
    static let toolNotFound = -32000
    static let toolExecFailed = -32001
    static let agentBusy = -32002
    static let aborted = -32003
    static let messageTooLarge = -32004
    static let guardDenied = -32005
    static let tripwire = -32006
}

/// JSON-RPCエラーレスポンスから最も具体的なSDK例外を生成する。
func fromRpcError(code: Int, message: String, data: JSONValue?) -> AgentError {
    switch code {
    case RpcErrorCode.tripwire:
        let reason = data?["reason"]?.stringValue ?? message
        return TripwireTriggered(message, reason: reason, code: code, data: data)
    case RpcErrorCode.guardDenied:
        let reason = data?["reason"]?.stringValue ?? message
        let decision = data?["decision"]?.stringValue ?? "deny"
        return GuardDenied(message, decision: decision, reason: reason, code: code, data: data)
    case RpcErrorCode.toolNotFound, RpcErrorCode.toolExecFailed:
        return ToolError(message, code: code, data: data)
    case RpcErrorCode.agentBusy:
        return AgentBusy(message, code: code, data: data)
    case RpcErrorCode.aborted:
        return AgentAborted(message, code: code, data: data)
    default:
        return AgentError(message, code: code, data: data)
    }
}
