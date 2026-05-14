/// Exception types for the ai-agent Swift SDK.
///
/// The SDK wraps low-level transport / RPC errors into a small hierarchy
/// so that user code can use `catch let e as AgentError` for "anything
/// from the SDK" and still match more precise subclasses for known
/// JSON-RPC error codes.
///
/// Mapping to the JSON-RPC errors defined in `pkg/protocol/errors.go`:
///
/// * `-32700` Parse error            -> ``AgentError``
/// * `-32600` Invalid request        -> ``AgentError``
/// * `-32601` Method not found       -> ``AgentError``
/// * `-32602` Invalid params         -> ``AgentError``
/// * `-32603` Internal error         -> ``AgentError``
/// * `-32000` Tool not found         -> ``ToolError``
/// * `-32001` Tool execution failed  -> ``ToolError``
/// * `-32002` Agent already running  -> ``AgentBusy``
/// * `-32003` Aborted                -> ``AgentAborted``
/// * `-32004` Message too large      -> ``AgentError``
///
/// Guard "deny"/"tripwire" decisions surface as ``GuardDenied`` from
/// ``Agent/run(_:options:)`` when the input guard rejects a prompt.
import Foundation

public class AgentError: Error, CustomStringConvertible, @unchecked Sendable {
    public let message: String
    public let code: Int?
    public let data: JSONValue?

    public init(_ message: String, code: Int? = nil, data: JSONValue? = nil) {
        self.message = message
        self.code = code
        self.data = data
    }

    public var description: String { message }
}

public final class AgentBusy: AgentError, @unchecked Sendable {}
public final class AgentAborted: AgentError, @unchecked Sendable {}
public final class ToolError: AgentError, @unchecked Sendable {}

public final class GuardDenied: AgentError, @unchecked Sendable {
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
}

/// Build the most specific ``AgentError`` subclass for a JSON-RPC error code.
public func agentErrorFromRpc(code: Int, message: String, data: JSONValue? = nil) -> AgentError {
    switch code {
    case -32000, -32001:
        return ToolError(message, code: code, data: data)
    case -32002:
        return AgentBusy(message, code: code, data: data)
    case -32003:
        return AgentAborted(message, code: code, data: data)
    default:
        return AgentError(message, code: code, data: data)
    }
}
