import Foundation

let jsonRpcVersion = "2.0"

// MARK: - 内部メッセージ型

struct JsonRpcMessage: Codable {
    let jsonrpc: String?
    let id: JSONValue?
    let method: String?
    let params: JSONValue?
    let result: JSONValue?
    let error: JsonRpcError?
}

struct JsonRpcError: Codable {
    let code: Int
    let message: String
    let data: JSONValue?
}

// MARK: - ハンドラ型

public typealias NotificationHandler = @Sendable (JSONValue) async -> Void
public typealias RequestHandler = @Sendable (JSONValue) async throws -> JSONValue

// MARK: - 内部状態のActor

actor JsonRpcState {
    var nextId: Int = 0
    var pending: [Int: CheckedContinuation<JSONValue, Error>] = [:]
    var notifHandlers: [String: NotificationHandler] = [:]
    var requestHandlers: [String: RequestHandler] = [:]
    var closed: Bool = false
    var stderrBuffer: String = ""

    func allocateId() -> Int {
        nextId += 1
        return nextId
    }

    func registerPending(id: Int, continuation: CheckedContinuation<JSONValue, Error>) {
        pending[id] = continuation
    }

    func popPending(id: Int) -> CheckedContinuation<JSONValue, Error>? {
        pending.removeValue(forKey: id)
    }

    func setNotificationHandler(_ method: String, _ handler: @escaping NotificationHandler) {
        notifHandlers[method] = handler
    }

    func setRequestHandler(_ method: String, _ handler: @escaping RequestHandler) {
        requestHandlers[method] = handler
    }

    func getNotificationHandler(_ method: String) -> NotificationHandler? {
        notifHandlers[method]
    }

    func getRequestHandler(_ method: String) -> RequestHandler? {
        requestHandlers[method]
    }

    func markClosed() {
        closed = true
        for (_, cont) in pending {
            cont.resume(throwing: AgentError("connection closed"))
        }
        pending.removeAll()
    }

    func isClosed() -> Bool { closed }

    func appendStderr(_ s: String) {
        stderrBuffer.append(s)
    }

    func getStderr() -> String { stderrBuffer }
}

// MARK: - JsonRpcClient

/// 改行区切りJSON-RPC 2.0クライアント (stdio over subprocess)。
///
/// `connectSubprocess(...)` でGoバイナリ (`agent --rpc`) を起動し、stdin/stdoutで通信する。
///
/// ### デバッグ通信トレース (D5)
///
/// 環境変数 `AGENT_RPC_TRACE=1` を立てると、送信/受信した JSON メッセージを
/// stderr に `>>` / `<<` プレフィックス付きで出力する。camelCase の Swift 値が
/// 裏で snake_case に変換されるのを目視できる。
public final class JsonRpcClient: @unchecked Sendable {
    private let state = JsonRpcState()
    private let traceEnabled: Bool = ProcessInfo.processInfo.environment["AGENT_RPC_TRACE"] == "1"
    // `Process` 型は iOS には存在せず、Mac Catalyst では @available(unavailable) で
    // マークされる。共通コードとして AnyObject で保持し、操作は KVC / NSInvocation
    // 経由で行うことで、macOS / Mac Catalyst / iOS の 3 プラットフォームすべてで
    // コンパイルを通す (iOS では connectSubprocess が実行時に失敗する)。
    private var process: AnyObject?
    private var stdin: FileHandle?
    private var stdout: FileHandle?
    private var stderr: FileHandle?
    private let writeLock = NSLock()
    private var readerTask: Task<Void, Never>?
    private var stderrTask: Task<Void, Never>?

    public init() {}

    // MARK: - ライフサイクル

    /// バイナリを起動して読み込みループを開始する。
    public func connectSubprocess(
        _ binaryPath: String,
        args: [String] = ["--rpc"],
        env: [String: String]? = nil,
        cwd: String? = nil
    ) throws {
        // Process / NSTask クラスをランタイムで取得 (iOS には存在しない)。
        guard let cls = NSClassFromString("NSTask") as? NSObject.Type
            ?? NSClassFromString("NSConcreteTask") as? NSObject.Type else {
            throw AgentError(
                "Subprocess support is unavailable on this platform "
                + "(NSTask not present). ai-agent requires macOS or Mac Catalyst."
            )
        }

        var fullEnv = ProcessInfo.processInfo.environment
        if let env = env {
            for (k, v) in env { fullEnv[k] = v }
        }

        let stdinPipe = Pipe()
        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()

        // Mac Catalyst では Process.executableURL / currentDirectoryURL / run() が
        // @available(macCatalyst, unavailable) でマークされているため、Swift の
        // 可用性チェックを回避して Objective-C ランタイム経由で呼び出す。
        let process: NSObject = cls.init()
        process.setValue(URL(fileURLWithPath: binaryPath), forKey: "executableURL")
        process.setValue(args, forKey: "arguments")
        process.setValue(fullEnv, forKey: "environment")
        if let cwd = cwd {
            process.setValue(URL(fileURLWithPath: cwd), forKey: "currentDirectoryURL")
        }
        process.setValue(stdinPipe, forKey: "standardInput")
        process.setValue(stdoutPipe, forKey: "standardOutput")
        process.setValue(stderrPipe, forKey: "standardError")

        do {
            try runProcessViaRuntime(process)
        } catch CocoaError.fileNoSuchFile {
            throw AgentError(
                "Agent binary not found: \(binaryPath)\n"
                + "  Build it first:  go build -o agent ./cmd/agent/"
            )
        } catch {
            throw AgentError("failed to launch \(binaryPath): \(error)")
        }

        self.process = process
        self.stdin = stdinPipe.fileHandleForWriting
        self.stdout = stdoutPipe.fileHandleForReading
        self.stderr = stderrPipe.fileHandleForReading

        startReaderLoop()
        startStderrLoop()
    }

    /// NSTask の `launchAndReturnError:` を ObjC ランタイム経由で呼ぶ。
    private func runProcessViaRuntime(_ process: NSObject) throws {
        let sel = Selector(("launchAndReturnError:"))
        if process.responds(to: sel),
           let method = class_getInstanceMethod(type(of: process), sel) {
            typealias LaunchFn = @convention(c) (
                AnyObject, Selector, AutoreleasingUnsafeMutablePointer<NSError?>
            ) -> Bool
            let imp = method_getImplementation(method)
            let fn = unsafeBitCast(imp, to: LaunchFn.self)
            var nsErr: NSError? = nil
            if !fn(process, sel, &nsErr) {
                if let e = nsErr { throw e }
                throw AgentError("launchAndReturnError: failed")
            }
            return
        }
        // 古い launch() にフォールバック (返り値なし)
        process.perform(Selector(("launch")))
    }

    public func close() async {
        // stdinを閉じてピアにEOFを伝える。
        if let stdin = stdin {
            try? stdin.close()
            self.stdin = nil
        }

        if let process = process {
            // graceful waitを5秒。blocking waitUntilExitはDispatchQueueに逃がす。
            _ = await withTimeoutOrNil(seconds: 5.0) {
                await self.waitForExit(process)
            }
            if Self.processIsRunning(process) {
                _ = process.perform(Selector(("terminate")))
                _ = await withTimeoutOrNil(seconds: 2.0) {
                    await self.waitForExit(process)
                }
                if Self.processIsRunning(process) {
                    _ = process.perform(Selector(("interrupt")))
                    _ = await withTimeoutOrNil(seconds: 1.0) {
                        await self.waitForExit(process)
                    }
                }
            }
            self.process = nil
        }

        readerTask?.cancel()
        stderrTask?.cancel()
        readerTask = nil
        stderrTask = nil

        await state.markClosed()
    }

    /// Blocking `process.waitUntilExit()` を別スレッドに逃がして async から待つ。
    private func waitForExit(_ process: AnyObject) async {
        await withCheckedContinuation { (cont: CheckedContinuation<Void, Never>) in
            DispatchQueue.global().async {
                _ = process.perform(Selector(("waitUntilExit")))
                cont.resume()
            }
        }
    }

    private static func processIsRunning(_ process: AnyObject) -> Bool {
        (process.value(forKey: "isRunning") as? Bool) ?? false
    }

    public var stderrOutput: String {
        get async { await state.getStderr() }
    }

    // MARK: - ハンドラ登録

    public func setNotificationHandler(_ method: String, _ handler: @escaping NotificationHandler) async {
        await state.setNotificationHandler(method, handler)
    }

    public func setRequestHandler(_ method: String, _ handler: @escaping RequestHandler) async {
        await state.setRequestHandler(method, handler)
    }

    // MARK: - RPCプリミティブ

    /// JSON-RPCリクエストを送信し、結果を待つ。
    public func call(
        _ method: String,
        params: JSONValue = .object([:]),
        timeout: Duration? = nil
    ) async throws -> JSONValue {
        guard stdin != nil else { throw AgentError("not connected") }
        let id = await state.allocateId()

        let timeoutTask: Task<Void, Never>?
        if let timeout = timeout {
            timeoutTask = Task { [state] in
                try? await Task.sleep(for: timeout)
                if let cont = await state.popPending(id: id) {
                    cont.resume(throwing: AgentError(
                        "RPC timeout waiting for \(method) (id=\(id))"
                    ))
                }
            }
        } else {
            timeoutTask = nil
        }
        defer { timeoutTask?.cancel() }

        let message: JSONValue = .object([
            "jsonrpc": .string(jsonRpcVersion),
            "method": .string(method),
            "params": params,
            "id": .int(Int64(id)),
        ])

        return try await withCheckedThrowingContinuation { (cont: CheckedContinuation<JSONValue, Error>) in
            // 必ず「先に登録 → 後に送信」の順番で行う。
            // (送信前にpendingにエントリを置かないと、即座に応答が返ってきた場合に取りこぼす)
            Task { [state] in
                await state.registerPending(id: id, continuation: cont)
                do {
                    try writeMessage(message)
                } catch {
                    if let popped = await state.popPending(id: id) {
                        popped.resume(throwing: AgentError("failed to send \(method): \(error)"))
                    }
                }
            }
        }
    }

    /// 通知 (id なし、応答待ちなし) を送信する。
    public func notify(_ method: String, params: JSONValue = .object([:])) throws {
        guard stdin != nil else { throw AgentError("not connected") }
        let message: JSONValue = .object([
            "jsonrpc": .string(jsonRpcVersion),
            "method": .string(method),
            "params": params,
        ])
        try writeMessage(message)
    }

    // MARK: - 内部: 書き込み

    private func writeMessage(_ message: JSONValue) throws {
        guard let stdin = stdin else { throw AgentError("stdin closed") }
        var data = try JSONSerialization.data(
            withJSONObject: message.toRaw(),
            options: [.fragmentsAllowed]
        )
        data.append(0x0A)  // newline

        if traceEnabled, let line = String(data: data, encoding: .utf8) {
            FileHandle.standardError.write(Data((">> " + line).utf8))
        }

        writeLock.lock()
        defer { writeLock.unlock() }
        do {
            try stdin.write(contentsOf: data)
        } catch {
            throw AgentError("write failed: \(error)")
        }
    }

    // MARK: - 内部: 読み込み

    private func startReaderLoop() {
        guard let stdout = stdout else { return }
        let (stream, continuation) = AsyncStream<Data>.makeStream(bufferingPolicy: .unbounded)
        // EOF を検知したらハンドラを外して以後の空読みを止める。
        stdout.readabilityHandler = { handle in
            let data = handle.availableData
            if data.isEmpty {
                handle.readabilityHandler = nil
                continuation.finish()
            } else {
                continuation.yield(data)
            }
        }
        readerTask = Task.detached { [weak self] in
            await self?.consumeReadStream(stream)
        }
    }

    private func consumeReadStream(_ stream: AsyncStream<Data>) async {
        var buffer = Data()
        for await chunk in stream {
            buffer.append(chunk)
            while let nl = buffer.firstIndex(of: 0x0A) {
                let lineData = buffer.subdata(in: 0..<nl)
                buffer.removeSubrange(0...nl)
                if lineData.isEmpty { continue }
                await dispatchLine(lineData)
            }
        }
        await state.markClosed()
    }

    private func dispatchLine(_ data: Data) async {
        if traceEnabled, let line = String(data: data, encoding: .utf8) {
            FileHandle.standardError.write(Data(("<< " + line + "\n").utf8))
        }
        let message: JSONValue
        do {
            let raw = try JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed])
            message = JSONValue.from(raw)
        } catch {
            return
        }

        // レスポンス: methodなし & idあり
        let method = message["method"]?.stringValue
        let id = message["id"]?.intValue

        if method == nil, let id = id {
            if let cont = await state.popPending(id: id) {
                if let err = message["error"], !err.isNull {
                    let code = err["code"]?.intValue ?? RpcErrorCode.toolNotFound
                    let msg = err["message"]?.stringValue ?? "unknown error"
                    let data = err["data"]
                    cont.resume(throwing: fromRpcError(code: code, message: msg, data: data))
                } else {
                    cont.resume(returning: message["result"] ?? .null)
                }
            }
            return
        }

        guard let method = method else { return }
        let params = message["params"] ?? .object([:])

        if id == nil {
            // 通知
            if let handler = await state.getNotificationHandler(method) {
                await handler(params)
            }
            return
        }

        // core -> wrapper リクエスト
        guard let handler = await state.getRequestHandler(method) else {
            await sendError(id: id!, code: -32601, message: "method not found: \(method)")
            return
        }

        do {
            let result = try await handler(params)
            let response: JSONValue = .object([
                "jsonrpc": .string(jsonRpcVersion),
                "id": .int(Int64(id!)),
                "result": result,
            ])
            try writeMessage(response)
        } catch let agentErr as AgentError {
            await sendError(id: id!, code: agentErr.code ?? -32603, message: agentErr.message)
        } catch {
            await sendError(id: id!, code: -32603, message: "\(error)")
        }
    }

    private func sendError(id: Int, code: Int, message: String) async {
        let response: JSONValue = .object([
            "jsonrpc": .string(jsonRpcVersion),
            "id": .int(Int64(id)),
            "error": .object([
                "code": .int(Int64(code)),
                "message": .string(message),
            ]),
        ])
        try? writeMessage(response)
    }

    // MARK: - stderrドレイン

    private func startStderrLoop() {
        guard let stderr = stderr else { return }
        let (stream, continuation) = AsyncStream<Data>.makeStream(bufferingPolicy: .unbounded)
        stderr.readabilityHandler = { handle in
            let data = handle.availableData
            if data.isEmpty {
                handle.readabilityHandler = nil
                continuation.finish()
            } else {
                continuation.yield(data)
            }
        }
        stderrTask = Task.detached { [weak self] in
            for await chunk in stream {
                if let s = String(data: chunk, encoding: .utf8) {
                    await self?.state.appendStderr(s)
                }
            }
        }
    }
}

// MARK: - タイムアウトユーティリティ

func withTimeoutOrNil<T: Sendable>(
    seconds: Double,
    operation: @escaping @Sendable () async -> T
) async -> T? {
    await withTaskGroup(of: T?.self) { group in
        group.addTask {
            await operation()
        }
        group.addTask {
            try? await Task.sleep(nanoseconds: UInt64(seconds * 1_000_000_000))
            return nil
        }
        let result = await group.next() ?? nil
        group.cancelAll()
        return result
    }
}
