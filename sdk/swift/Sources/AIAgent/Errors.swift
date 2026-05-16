import Foundation

// MARK: - エラーカテゴリ (意味付き enum)

/// JSON-RPC エラーコードを Swift らしい意味付きケースで扱うための列挙型。
///
/// `AgentError.kind` から取得できる。`switch error.kind { ... }` で
/// 個別ハンドリングできる。生コード番号 (-32xxx) を直接扱う必要はない。
public enum AgentErrorKind: Sendable, Equatable {
    /// ツール名が未登録 (-32000)
    case toolNotFound
    /// ツールの handler が例外を投げた (-32001)
    case toolExecutionFailed
    /// `agent.run` 実行中に別の `run` を呼んだ (-32002)
    case agentBusy
    /// `agent.abort` で中断された (-32003)
    case aborted
    /// 履歴やメッセージがサイズ上限を超過 (-32004)
    case messageTooLarge
    /// 入力 / ツール呼び出し / 出力ガードが拒否した (-32005)
    case guardDenied
    /// トリップワイヤーガードが発火した (-32006)
    case tripwireTriggered
    /// `max_turns` 到達 (内部的には -32603 + "max turns" メッセージ)。
    ///
    /// Phase 1-2 で `AgentResult(reason: "max_turns")` に切り替わる予定だが、
    /// 旧バイナリ互換のためにエラーケースは残す。
    case maxTurnsReached
    /// プロトコル違反 / JSON 不正 / SDK 内部矛盾など、上記以外のすべて
    case other(code: Int?)

    /// 人間可読な短いラベル。`localizedDescription` の prefix にも使う。
    public var label: String {
        switch self {
        case .toolNotFound: return "tool not found"
        case .toolExecutionFailed: return "tool execution failed"
        case .agentBusy: return "agent busy"
        case .aborted: return "aborted"
        case .messageTooLarge: return "message too large"
        case .guardDenied: return "guard denied"
        case .tripwireTriggered: return "tripwire triggered"
        case .maxTurnsReached: return "max turns reached"
        case .other(let code):
            if let code = code { return "RPC error (code \(code))" }
            return "agent error"
        }
    }

    /// 元の JSON-RPC エラーコード。`.other` 以外は固定値。
    public var rpcCode: Int? {
        switch self {
        case .toolNotFound: return -32000
        case .toolExecutionFailed: return -32001
        case .agentBusy: return -32002
        case .aborted: return -32003
        case .messageTooLarge: return -32004
        case .guardDenied: return -32005
        case .tripwireTriggered: return -32006
        case .maxTurnsReached: return -32603
        case .other(let code): return code
        }
    }
}

// MARK: - AgentError 基底クラス

/// ai-agent SDK の基底エラー。
///
/// `LocalizedError` 準拠なので `error.localizedDescription` で有意な文字列が
/// 得られる (`The operation couldn't be completed.` のような汎用文字列ではなく)。
///
/// パターンマッチ例:
///
/// ```swift
/// do {
///     try await agent.input("...")
/// } catch let err as AgentError {
///     switch err.kind {
///     case .toolNotFound:        // ツール未登録
///     case .guardDenied:         // ガード拒否
///     case .maxTurnsReached:     // ターン上限到達
///     default:                   print(err.localizedDescription)
///     }
/// }
/// ```
public class AgentError: LocalizedError, CustomStringConvertible, @unchecked Sendable {
    public let message: String
    public let code: Int?
    public let data: JSONValue?
    public let kind: AgentErrorKind

    public init(
        _ message: String,
        code: Int? = nil,
        data: JSONValue? = nil,
        kind: AgentErrorKind? = nil
    ) {
        self.message = message
        self.code = code
        self.data = data
        self.kind = kind ?? Self.deriveKind(code: code, message: message)
    }

    /// `code` / `message` から推定する。明示指定が無いときに `init` から呼ばれる。
    private static func deriveKind(code: Int?, message: String) -> AgentErrorKind {
        guard let code = code else { return .other(code: nil) }
        switch code {
        case -32000: return .toolNotFound
        case -32001: return .toolExecutionFailed
        case -32002: return .agentBusy
        case -32003: return .aborted
        case -32004: return .messageTooLarge
        case -32005: return .guardDenied
        case -32006: return .tripwireTriggered
        case -32603:
            // Go コアからの "max turns reached" を識別する
            if message.lowercased().contains("max turns") {
                return .maxTurnsReached
            }
            return .other(code: code)
        default:
            return .other(code: code)
        }
    }

    // MARK: LocalizedError

    public var errorDescription: String? {
        var parts: [String] = [kind.label]
        if !message.isEmpty, message != kind.label { parts.append(message) }
        if let code = code, kind.rpcCode != code {
            parts.append("[rpc=\(code)]")
        }
        return parts.joined(separator: ": ")
    }

    public var failureReason: String? { message.isEmpty ? nil : message }
    public var recoverySuggestion: String? { Self.recoveryHint(for: kind) }

    private static func recoveryHint(for kind: AgentErrorKind) -> String? {
        switch kind {
        case .toolNotFound:
            return "registerTools(...) でツールを登録しているか確認してください。"
        case .toolExecutionFailed:
            return "ツール handler 内の例外を確認し、必要に応じて try/catch で握ってください。"
        case .agentBusy:
            return "前回の input(...) / run(...) が完了するのを待ってから次を呼んでください。"
        case .aborted:
            return "agent.abort(reason:) で意図的に中断されました。"
        case .guardDenied:
            return "GuardDenied として捕捉すると decision/reason が取れます。"
        case .tripwireTriggered:
            return "TripwireTriggered として捕捉してください。重大なガード発火です。"
        case .maxTurnsReached:
            return "maxTurns を増やすか、judge / toolBudget で早期終了を仕込んでください。"
        case .messageTooLarge:
            return "compaction を有効にするか、入力サイズを削減してください。"
        case .other:
            return nil
        }
    }

    // MARK: CustomStringConvertible (description は引き続きデバッグ向け)

    public var description: String {
        var parts = ["AgentError(\(kind.label))"]
        if let code = code { parts.append("code=\(code)") }
        if !message.isEmpty { parts.append("message=\"\(message)\"") }
        return parts.joined(separator: " ")
    }
}

/// `agent.run` 実行中に別の `run` を呼んだ際に発生 (-32002)。
public final class AgentBusy: AgentError, @unchecked Sendable {}

/// `agent.abort` で中断された際に発生 (-32003)。
public final class AgentAborted: AgentError, @unchecked Sendable {}

/// ツール関連エラー (-32000 / -32001)。
public final class ToolError: AgentError, @unchecked Sendable {}

/// 入力 / ツール呼び出し / 出力ガードが拒否した際に発生 (-32005)。
public class GuardDenied: AgentError, @unchecked Sendable {
    public let decision: String
    public let reason: String

    public init(
        _ message: String,
        decision: String = "deny",
        reason: String = "",
        code: Int? = nil,
        data: JSONValue? = nil,
        kind: AgentErrorKind = .guardDenied
    ) {
        self.decision = decision
        self.reason = reason
        super.init(message, code: code, data: data, kind: kind)
    }

    public override var errorDescription: String? {
        var parts = ["[\(decision)] \(message)"]
        if !reason.isEmpty { parts.append("reason: \(reason)") }
        return parts.joined(separator: " — ")
    }
}

/// トリップワイヤーガードが発火した際に発生 (-32006)。
public final class TripwireTriggered: GuardDenied, @unchecked Sendable {
    public init(_ message: String, reason: String = "", code: Int? = nil, data: JSONValue? = nil) {
        super.init(
            message,
            decision: "tripwire",
            reason: reason,
            code: code,
            data: data,
            kind: .tripwireTriggered
        )
    }

    public override var errorDescription: String? {
        var parts = ["[tripwire] \(message)"]
        if !reason.isEmpty { parts.append("reason: \(reason)") }
        return parts.joined(separator: " — ")
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
    static let internalError = -32603
}

/// JSON-RPC エラーレスポンスから最も具体的な SDK 例外を生成する。
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
