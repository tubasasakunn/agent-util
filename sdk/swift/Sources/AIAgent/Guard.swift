/// Guard / verifier helpers.
///
/// A guard is a wrapper-side function the core invokes via `guard.execute`
/// to decide whether a prompt / tool call / output should be allowed,
/// denied, or should trip the safety wire (immediate stop).
///
/// A verifier is a wrapper-side function the core invokes via
/// `verifier.execute` after a tool produced a result, returning
/// `{passed, summary}`.
///
/// ```swift
/// let noSecrets = Guard.input(name: "no_secrets") { input in
///     if input.lowercased().contains("password") {
///         return GuardResult(decision: .deny, reason: "looks like a secret")
///     }
///     return GuardResult(decision: .allow)
/// }
/// ```
import Foundation

public enum GuardStage: String, Sendable, Equatable {
    case input
    case toolCall = "tool_call"
    case output
}

public enum GuardDecision: String, Sendable, Equatable {
    case allow
    case deny
    case tripwire
}

public struct GuardResult: Sendable, Equatable {
    public var decision: GuardDecision
    public var reason: String

    public init(decision: GuardDecision, reason: String = "") {
        self.decision = decision
        self.reason = reason
    }
}

public typealias InputGuardFn = @Sendable (String) async throws -> GuardResult
public typealias ToolCallGuardFn = @Sendable (String, JSONValue) async throws -> GuardResult
public typealias OutputGuardFn = @Sendable (String) async throws -> GuardResult

/// Wrapper-side invocation context passed to a guard.
public struct GuardCallContext: Sendable {
    public let input: String
    public let toolName: String
    public let args: JSONValue
    public let output: String
}

public typealias GuardCall = @Sendable (GuardCallContext) async throws -> GuardResult

public struct Guard: Sendable {
    public let name: String
    public let stage: GuardStage
    public let call: GuardCall

    public init(name: String, stage: GuardStage, call: @escaping GuardCall) {
        self.name = name
        self.stage = stage
        self.call = call
    }

    /// Build an `input`-stage guard.
    public static func input(name: String, fn: @escaping InputGuardFn) -> Guard {
        Guard(name: name, stage: .input) { ctx in
            try await normalise(fn(ctx.input))
        }
    }

    /// Build a `tool_call`-stage guard.
    public static func toolCall(name: String, fn: @escaping ToolCallGuardFn) -> Guard {
        Guard(name: name, stage: .toolCall) { ctx in
            try await normalise(fn(ctx.toolName, ctx.args))
        }
    }

    /// Build an `output`-stage guard.
    public static func output(name: String, fn: @escaping OutputGuardFn) -> Guard {
        Guard(name: name, stage: .output) { ctx in
            try await normalise(fn(ctx.output))
        }
    }

    /// Internal: project to the wire format used by `guard.register`.
    public func toWire() -> JSONValue {
        return .object([
            "name": .string(name),
            "stage": .string(stage.rawValue),
        ])
    }
}

private func normalise(_ result: GuardResult) -> GuardResult {
    return GuardResult(decision: result.decision, reason: result.reason)
}

// MARK: - Verifier

public struct VerifierResult: Sendable, Equatable {
    public var passed: Bool
    public var summary: String

    public init(passed: Bool, summary: String = "") {
        self.passed = passed
        self.summary = summary
    }
}

public typealias VerifierFn = @Sendable (String, JSONValue, String) async throws -> VerifierResult

public struct Verifier: Sendable {
    public let name: String
    public let call: VerifierFn

    public init(name: String, fn: @escaping VerifierFn) {
        self.name = name
        self.call = fn
    }

    /// Internal: project to the wire format used by `verifier.register`.
    public func toWire() -> JSONValue {
        return .object(["name": .string(name)])
    }
}
