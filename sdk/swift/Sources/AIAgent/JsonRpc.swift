/// Internal JSON-RPC 2.0 client over newline-delimited JSON streams.
///
/// Mirrors `sdk/js/src/jsonrpc.ts` and `sdk/python/ai_agent/jsonrpc.py`.
/// The transport is abstracted via ``JsonRpcTransport`` so the client
/// works equally well against a real subprocess (``SubprocessTransport``)
/// and an in-memory pair of streams used by tests.
///
/// Supports both directions of the protocol:
///
/// * **wrapper -> core**: ``JsonRpcClient/call(_:params:timeout:)`` returns
///   the response result (or throws an ``AgentError``).
/// * **core -> wrapper**: handlers registered via
///   ``JsonRpcClient/setRequestHandler(_:_:)`` are invoked when the core
///   sends a request such as `tool.execute`, `guard.execute` or
///   `verifier.execute`. Handlers are async and return the result value.
/// * **core -> wrapper notifications**: handlers registered via
///   ``JsonRpcClient/setNotificationHandler(_:_:)`` for `stream.delta` /
///   `stream.end` / `context.status`.
import Foundation

public let JSONRPC_VERSION = "2.0"

public typealias NotificationHandler = @Sendable (JSONValue) async -> Void
public typealias RequestHandler = @Sendable (JSONValue) async throws -> JSONValue

/// Minimal stream contract the client needs.
///
/// `readLines()` yields one JSON-encoded message per item (no trailing
/// newline). `write(_:)` writes a single JSON message; the implementation
/// appends `\n`. `close()` flushes and tears down.
public protocol JsonRpcTransport: Sendable {
    func readLines() -> AsyncThrowingStream<String, Error>
    func write(_ line: String) async throws
    func close() async
}

// MARK: - LineReaderState

/// Lock-protected buffer that turns arbitrary `Data` chunks into
/// newline-delimited UTF-8 lines.
final class LineReaderState: @unchecked Sendable {
    private let lock = NSLock()
    private var buffer = Data()

    func consume(_ chunk: Data, yield: (String) -> Void) {
        var emit: [String] = []
        lock.lock()
        buffer.append(chunk)
        while let nlIdx = buffer.firstIndex(of: 0x0A) {
            let lineData = buffer.subdata(in: 0..<nlIdx)
            buffer = buffer.subdata(in: (nlIdx + 1)..<buffer.count)
            if !lineData.isEmpty, let line = String(data: lineData, encoding: .utf8) {
                emit.append(line)
            }
        }
        lock.unlock()
        for line in emit { yield(line) }
    }
}

// MARK: - SubprocessTransport

/// Spawns a child process and exposes its stdin/stdout as a
/// ``JsonRpcTransport``. Captures stderr into a shared buffer.
public final class SubprocessTransport: JsonRpcTransport, @unchecked Sendable {
    public enum StderrMode: Sendable {
        case inherit
        case pipe
        case ignore
    }

    private let process: Process
    private let stdinPipe: Pipe
    private let stdoutPipe: Pipe
    private let stderrPipe: Pipe?
    private let stderrLock = NSLock()
    private var stderrBuffer: [String] = []
    private let writeQueue = DispatchQueue(label: "ai-agent.subprocess.write")

    public init(
        binaryPath: String,
        args: [String] = ["--rpc"],
        env: [String: String]? = nil,
        cwd: String? = nil,
        stderr: StderrMode = .pipe
    ) throws {
        self.process = Process()
        self.stdinPipe = Pipe()
        self.stdoutPipe = Pipe()

        process.executableURL = URL(fileURLWithPath: binaryPath)
        process.arguments = args
        process.standardInput = stdinPipe
        process.standardOutput = stdoutPipe
        if let cwd {
            process.currentDirectoryURL = URL(fileURLWithPath: cwd)
        }

        // Build environment by inheriting the parent's and overlaying `env`.
        var fullEnv = ProcessInfo.processInfo.environment
        if let env {
            for (k, v) in env { fullEnv[k] = v }
        }
        process.environment = fullEnv

        switch stderr {
        case .inherit:
            self.stderrPipe = nil
        case .ignore:
            self.stderrPipe = nil
            // Discard by binding to /dev/null.
            let devnull = FileHandle(forWritingAtPath: "/dev/null")
            process.standardError = devnull
        case .pipe:
            let p = Pipe()
            self.stderrPipe = p
            process.standardError = p
        }

        try process.run()

        if let stderrPipe {
            stderrPipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
                guard let self else { return }
                let chunk = handle.availableData
                if chunk.isEmpty { return }
                if let s = String(data: chunk, encoding: .utf8) {
                    self.stderrLock.lock()
                    self.stderrBuffer.append(s)
                    self.stderrLock.unlock()
                }
            }
        }
    }

    public var capturedStderr: String {
        stderrLock.lock()
        defer { stderrLock.unlock() }
        return stderrBuffer.joined()
    }

    public func readLines() -> AsyncThrowingStream<String, Error> {
        let pipe = self.stdoutPipe
        return AsyncThrowingStream { continuation in
            let state = LineReaderState()
            let handle = pipe.fileHandleForReading
            handle.readabilityHandler = { fh in
                let chunk = fh.availableData
                if chunk.isEmpty {
                    continuation.finish()
                    return
                }
                state.consume(chunk) { line in
                    continuation.yield(line)
                }
            }
            continuation.onTermination = { _ in
                handle.readabilityHandler = nil
            }
        }
    }

    public func write(_ line: String) async throws {
        let payload = (line + "\n").data(using: .utf8) ?? Data()
        let handle = stdinPipe.fileHandleForWriting
        // Hop to a dedicated serial queue so concurrent callers can't interleave.
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            writeQueue.async {
                do {
                    if #available(macOS 10.15.4, *) {
                        try handle.write(contentsOf: payload)
                    } else {
                        handle.write(payload)
                    }
                    continuation.resume(returning: ())
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    public func close() async {
        // Close stdin so the peer gets EOF, then wait briefly for exit.
        do {
            if #available(macOS 10.15, *) {
                try stdinPipe.fileHandleForWriting.close()
            } else {
                stdinPipe.fileHandleForWriting.closeFile()
            }
        } catch {
            // already closed — fine.
        }

        if !process.isRunning { return }

        let exited = await waitForExit(timeout: 5.0)
        if exited { return }

        process.terminate() // SIGTERM
        let exitedAfterTerm = await waitForExit(timeout: 2.0)
        if exitedAfterTerm { return }

        // Last resort: SIGKILL — not available via Process directly on Linux,
        // but `interrupt()` sends SIGINT which is close enough as a fallback.
        #if canImport(Darwin)
        kill(process.processIdentifier, SIGKILL)
        #else
        process.interrupt()
        #endif
        _ = await waitForExit(timeout: 1.0)
    }

    private func waitForExit(timeout seconds: Double) async -> Bool {
        let deadline = Date().addingTimeInterval(seconds)
        while Date() < deadline {
            if !process.isRunning { return true }
            try? await Task.sleep(nanoseconds: 25_000_000) // 25 ms
        }
        return !process.isRunning
    }
}

// MARK: - JsonRpcClient internals

/// Thread-safe one-shot promise.
///
/// `call` registers a promise in the actor's `pending` map *before* it
/// awaits any send. When the read loop's dispatcher resolves the promise
/// concurrently the lock ensures the result is delivered to whichever
/// side gets there second — there's no awkward "the response arrived
/// before we had a chance to register a continuation" race.
final class JsonRpcPromise: @unchecked Sendable {
    private let lock = NSLock()
    private var result: Result<JSONValue, Error>?
    private var continuation: CheckedContinuation<JSONValue, Error>?

    func resolve(_ value: Result<JSONValue, Error>) {
        lock.lock()
        if let cont = continuation {
            continuation = nil
            lock.unlock()
            switch value {
            case .success(let v): cont.resume(returning: v)
            case .failure(let e): cont.resume(throwing: e)
            }
            return
        }
        if result == nil { result = value }
        lock.unlock()
    }

    func wait() async throws -> JSONValue {
        try await withCheckedThrowingContinuation { (cont: CheckedContinuation<JSONValue, Error>) in
            lock.lock()
            if let r = result {
                lock.unlock()
                switch r {
                case .success(let v): cont.resume(returning: v)
                case .failure(let e): cont.resume(throwing: e)
                }
                return
            }
            continuation = cont
            lock.unlock()
        }
    }
}

/// Async JSON-RPC 2.0 client.
///
/// One client speaks to one transport. The class is an `actor`, so all
/// mutable state (`pending`, handler tables, write serialisation) is
/// naturally protected from data races.
public actor JsonRpcClient {
    private var transport: JsonRpcTransport?
    private var readTask: Task<Void, Never>?
    private var closed = false

    private var nextId: Int64 = 0
    private var pending: [Int64: JsonRpcPromise] = [:]

    private var notificationHandlers: [String: NotificationHandler] = [:]
    private var requestHandlers: [String: RequestHandler] = [:]

    public init() {}

    // MARK: lifecycle

    /// Convenience: spawn `binaryPath` and attach the resulting transport.
    public func connectSubprocess(
        binaryPath: String,
        args: [String] = ["--rpc"],
        env: [String: String]? = nil,
        cwd: String? = nil,
        stderr: SubprocessTransport.StderrMode = .pipe
    ) async throws {
        let t = try SubprocessTransport(
            binaryPath: binaryPath,
            args: args,
            env: env,
            cwd: cwd,
            stderr: stderr
        )
        attach(t)
    }

    /// Attach to an existing transport (used in tests and by
    /// ``connectSubprocess(binaryPath:args:env:cwd:stderr:)``).
    public func attach(_ transport: JsonRpcTransport) {
        precondition(self.transport == nil, "JsonRpcClient already attached")
        self.transport = transport
        let stream = transport.readLines()
        self.readTask = Task { [weak self] in
            await self?.runReadLoop(stream)
        }
    }

    /// Tear down the transport and reject any in-flight calls.
    public func close() async {
        if closed { return }
        closed = true
        let t = self.transport
        self.transport = nil
        if let t {
            await t.close()
        }
        self.readTask?.cancel()
        await self.readTask?.value
        self.readTask = nil
        for promise in pending.values {
            promise.resolve(.failure(AgentError("connection closed")))
        }
        pending.removeAll()
    }

    /// Captured stderr from the underlying subprocess (when applicable).
    public var stderrOutput: String {
        if let t = transport as? SubprocessTransport {
            return t.capturedStderr
        }
        return ""
    }

    // MARK: handler registration

    public func setNotificationHandler(_ method: String, _ handler: @escaping NotificationHandler) {
        notificationHandlers[method] = handler
    }

    public func setRequestHandler(_ method: String, _ handler: @escaping RequestHandler) {
        requestHandlers[method] = handler
    }

    // MARK: RPC primitives

    /// Send a wrapper -> core request and await its result.
    ///
    /// Throws ``AgentError`` (or a subclass) on JSON-RPC error responses,
    /// or on transport / timeout failure.
    public func call(
        _ method: String,
        params: JSONValue = .object([:]),
        timeout: TimeInterval? = nil
    ) async throws -> JSONValue {
        guard let transport = self.transport else { throw AgentError("not connected") }
        nextId += 1
        let id = nextId

        let payload: JSONValue = .object([
            "jsonrpc": .string(JSONRPC_VERSION),
            "method": .string(method),
            "params": params,
            "id": .int(id),
        ])
        let encoded = try payload.encodedString()

        // Register the promise synchronously *before* awaiting the send so the
        // read loop's dispatcher can never miss a fast response.
        let promise = JsonRpcPromise()
        pending[id] = promise

        do {
            try await transport.write(encoded)
        } catch {
            pending.removeValue(forKey: id)
            throw AgentError("failed to send \(method): \(error)")
        }

        let value: JSONValue
        do {
            if let timeout {
                value = try await withThrowingTaskGroup(of: JSONValue.self) { group in
                    group.addTask { try await promise.wait() }
                    group.addTask {
                        try await Task.sleep(nanoseconds: UInt64(timeout * 1_000_000_000))
                        throw AgentError("timeout after \(Int(timeout * 1000))ms calling \(method)")
                    }
                    let first = try await group.next()!
                    group.cancelAll()
                    return first
                }
            } else {
                value = try await promise.wait()
            }
        } catch {
            pending.removeValue(forKey: id)
            throw error
        }
        pending.removeValue(forKey: id)
        return value
    }

    /// Send a notification (no `id`, no response expected).
    public func notify(_ method: String, params: JSONValue = .object([:])) async throws {
        guard let transport else { throw AgentError("not connected") }
        let payload: JSONValue = .object([
            "jsonrpc": .string(JSONRPC_VERSION),
            "method": .string(method),
            "params": params,
        ])
        try await transport.write(payload.encodedString())
    }

    // MARK: read loop

    private func runReadLoop(_ stream: AsyncThrowingStream<String, Error>) async {
        do {
            for try await line in stream {
                guard let message = try? JSONValue.decode(line) else {
                    // Peer MUST NOT send invalid JSON; ignore but don't crash.
                    continue
                }
                await dispatch(message)
            }
        } catch {
            // Transport died — fall through.
        }
        for promise in pending.values {
            promise.resolve(.failure(AgentError("connection closed")))
        }
        pending.removeAll()
    }

    private func dispatch(_ message: JSONValue) async {
        guard case .object(let dict) = message else { return }

        let method: String? = dict["method"].stringValue
        let idValue: JSONValue? = dict["id"]

        // Response: has id, no method.
        if method == nil, let idValue, !idValue.isNull {
            guard let id = idValue.intValue else { return }
            guard let promise = pending.removeValue(forKey: id) else { return }
            if let err = dict["error"], !err.isNull,
               case .object(let errObj) = err
            {
                let code = errObj["code"].intValue.map { Int($0) } ?? -32603
                let msg = errObj["message"].stringValue ?? "unknown error"
                let data = errObj["data"]
                promise.resolve(.failure(agentErrorFromRpc(code: code, message: msg, data: data)))
            } else {
                promise.resolve(.success(dict["result"] ?? .null))
            }
            return
        }

        guard let method else { return }
        let params = dict["params"] ?? .object([:])

        // Notification (no id). Dispatch in a detached task so a slow
        // handler can't block the read loop.
        if idValue == nil || idValue!.isNull {
            if let handler = notificationHandlers[method] {
                Task.detached { await handler(params) }
            }
            return
        }

        // core -> wrapper request. Same detached-dispatch policy.
        guard let handler = requestHandlers[method] else {
            await sendError(id: idValue!, code: -32601, message: "method not found: \(method)")
            return
        }
        let requestId = idValue!
        Task.detached { [weak self] in
            do {
                let result = try await handler(params)
                await self?.respondSuccess(id: requestId, result: result)
            } catch let e as AgentError {
                await self?.sendError(id: requestId, code: e.code ?? -32603, message: e.message)
            } catch {
                await self?.sendError(id: requestId, code: -32603, message: "\(error)")
            }
        }
    }

    private func respondSuccess(id: JSONValue, result: JSONValue) async {
        guard let transport else { return }
        let payload: JSONValue = .object([
            "jsonrpc": .string(JSONRPC_VERSION),
            "id": id,
            "result": result,
        ])
        try? await transport.write(payload.encodedString())
    }

    private func sendError(id: JSONValue, code: Int, message: String) async {
        guard let transport else { return }
        let payload: JSONValue = .object([
            "jsonrpc": .string(JSONRPC_VERSION),
            "id": id,
            "error": .object([
                "code": .int(Int64(code)),
                "message": .string(message),
            ]),
        ])
        try? await transport.write(payload.encodedString())
    }
}
