import Foundation

// MARK: - ガード

public enum GuardStage: String, Sendable {
    case input
    case toolCall = "tool_call"
    case output
}

public enum GuardDecision: String, Sendable {
    case allow
    case deny
    case tripwire
}

/// ガード呼び出し時の入力パラメータ。
public struct GuardInput: Sendable {
    public let input: String
    public let toolName: String
    public let args: JSONValue
    public let output: String
}

/// ガード判定の結果。`decision` だけでなく、拒否時の `reason` を必ず添える。
///
/// タプル `(GuardDecision, String)` だと型から意味が読めなかったため struct 化した
/// (D3)。タプル互換のため `GuardOutcome(.allow, "")` の二引数 init も提供する。
public struct GuardOutcome: Sendable, Equatable {
    public let decision: GuardDecision
    public let reason: String

    public init(decision: GuardDecision, reason: String = "") {
        self.decision = decision
        self.reason = reason
    }

    /// 旧 API のタプル互換のためのショートハンド。`GuardOutcome(.deny, "blocked")` 等。
    public init(_ decision: GuardDecision, _ reason: String = "") {
        self.decision = decision
        self.reason = reason
    }

    // ありがちなショートカット
    public static let allow = GuardOutcome(decision: .allow, reason: "")
    public static func deny(_ reason: String) -> GuardOutcome {
        GuardOutcome(decision: .deny, reason: reason)
    }
    public static func tripwire(_ reason: String) -> GuardOutcome {
        GuardOutcome(decision: .tripwire, reason: reason)
    }
}

/// `GuardHandler` の新しい (struct ベースの) シグネチャ。
public typealias GuardHandler = @Sendable (GuardInput) async throws -> GuardOutcome

/// 入力/ツール呼び出し/出力のステージで動作するガード。
public struct GuardSpec: Sendable {
    public let name: String
    public let stage: GuardStage
    public let handler: GuardHandler

    public init(name: String, stage: GuardStage, handler: @escaping GuardHandler) {
        self.name = name
        self.stage = stage
        self.handler = handler
    }

    public static func input(
        name: String,
        _ fn: @Sendable @escaping (String) async throws -> GuardOutcome
    ) -> GuardSpec {
        GuardSpec(name: name, stage: .input) { input in
            try await fn(input.input)
        }
    }

    public static func toolCall(
        name: String,
        _ fn: @Sendable @escaping (String, JSONValue) async throws -> GuardOutcome
    ) -> GuardSpec {
        GuardSpec(name: name, stage: .toolCall) { input in
            try await fn(input.toolName, input.args)
        }
    }

    public static func output(
        name: String,
        _ fn: @Sendable @escaping (String) async throws -> GuardOutcome
    ) -> GuardSpec {
        GuardSpec(name: name, stage: .output) { input in
            try await fn(input.output)
        }
    }

    func toProtocolDict() -> JSONValue {
        .object([
            "name": .string(name),
            "stage": .string(stage.rawValue),
        ])
    }
}

// MARK: - ベリファイア

/// Verifier の判定結果。タプル `(Bool, String)` から struct 化した (D3)。
public struct VerifierOutcome: Sendable, Equatable {
    public let passed: Bool
    public let summary: String

    public init(passed: Bool, summary: String = "") {
        self.passed = passed
        self.summary = summary
    }

    /// 旧 API のタプル互換用ショートハンド。`VerifierOutcome(true, "ok")` 等。
    public init(_ passed: Bool, _ summary: String = "") {
        self.passed = passed
        self.summary = summary
    }

    public static let pass = VerifierOutcome(passed: true, summary: "")
    public static func fail(_ summary: String) -> VerifierOutcome {
        VerifierOutcome(passed: false, summary: summary)
    }
}

public typealias VerifierHandler = @Sendable (_ toolName: String, _ args: JSONValue, _ result: String) async throws -> VerifierOutcome

public struct Verifier: Sendable {
    public let name: String
    public let handler: VerifierHandler

    public init(name: String, handler: @escaping VerifierHandler) {
        self.name = name
        self.handler = handler
    }

    func toProtocolDict() -> JSONValue {
        .object(["name": .string(name)])
    }
}

// MARK: - ジャッジ (ゴール達成判定)

/// Judge の判定結果。「終了するか」と「理由」を返す。
public struct JudgeOutcome: Sendable, Equatable {
    public let terminate: Bool
    public let reason: String

    public init(terminate: Bool, reason: String = "") {
        self.terminate = terminate
        self.reason = reason
    }

    /// 旧 API のタプル互換用ショートハンド。
    public init(_ terminate: Bool, _ reason: String = "") {
        self.terminate = terminate
        self.reason = reason
    }

    public static let `continue` = JudgeOutcome(terminate: false, reason: "")
    public static func done(_ reason: String = "") -> JudgeOutcome {
        JudgeOutcome(terminate: true, reason: reason)
    }
}

public typealias JudgeHandler = @Sendable (_ response: String, _ turn: Int) async throws -> JudgeOutcome

// MARK: - レジストリ

actor GuardRegistry {
    private var guards: [String: GuardSpec] = [:]

    func register(_ g: GuardSpec) { guards["\(g.name)/\(g.stage.rawValue)"] = g }

    func get(name: String, stage: String) -> GuardSpec? {
        guards["\(name)/\(stage)"]
    }

    func registered() -> [String] { Array(guards.keys).sorted() }
}

actor VerifierRegistry {
    private var verifiers: [String: Verifier] = [:]

    func register(_ v: Verifier) { verifiers[v.name] = v }

    func get(_ name: String) -> Verifier? { verifiers[name] }

    func registered() -> [String] { Array(verifiers.keys).sorted() }
}

actor JudgeRegistry {
    private var judges: [String: JudgeHandler] = [:]

    func register(name: String, handler: @escaping JudgeHandler) {
        judges[name] = handler
    }

    func get(_ name: String) -> JudgeHandler? { judges[name] }

    func registered() -> [String] { Array(judges.keys).sorted() }
}
