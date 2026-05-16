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

public typealias GuardHandler = @Sendable (GuardInput) async throws -> (GuardDecision, String)

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

    public static func input(name: String, _ fn: @Sendable @escaping (String) async throws -> (GuardDecision, String)) -> GuardSpec {
        GuardSpec(name: name, stage: .input) { input in
            try await fn(input.input)
        }
    }

    public static func toolCall(name: String, _ fn: @Sendable @escaping (String, JSONValue) async throws -> (GuardDecision, String)) -> GuardSpec {
        GuardSpec(name: name, stage: .toolCall) { input in
            try await fn(input.toolName, input.args)
        }
    }

    public static func output(name: String, _ fn: @Sendable @escaping (String) async throws -> (GuardDecision, String)) -> GuardSpec {
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

public typealias VerifierHandler = @Sendable (_ toolName: String, _ args: JSONValue, _ result: String) async throws -> (Bool, String)

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

public typealias JudgeHandler = @Sendable (_ response: String, _ turn: Int) async throws -> (Bool, String)

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
