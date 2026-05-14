import XCTest
@testable import AIAgent

/// In-memory transport for driving the client from tests. Lines the
/// client writes go into `written`; tests can `feed(_:)` lines back
/// into the client's read stream.
final class TestTransport: JsonRpcTransport, @unchecked Sendable {
    private let lock = NSLock()
    private var readContinuation: AsyncThrowingStream<String, Error>.Continuation?
    private var written: [String] = []
    private var writeWaiters: [CheckedContinuation<String, Never>] = []
    private var closed = false

    func readLines() -> AsyncThrowingStream<String, Error> {
        return AsyncThrowingStream { cont in
            self.lock.lock()
            self.readContinuation = cont
            self.lock.unlock()
        }
    }

    func write(_ line: String) async throws {
        lock.lock()
        if !writeWaiters.isEmpty {
            let waiter = writeWaiters.removeFirst()
            lock.unlock()
            waiter.resume(returning: line)
        } else {
            written.append(line)
            lock.unlock()
        }
    }

    func close() async {
        lock.lock()
        closed = true
        let cont = readContinuation
        readContinuation = nil
        let waiters = writeWaiters
        writeWaiters.removeAll()
        lock.unlock()
        cont?.finish()
        for w in waiters { w.resume(returning: "") }
    }

    /// Feed a single line into the client's input.
    func feed(_ line: String) {
        lock.lock()
        let cont = readContinuation
        lock.unlock()
        cont?.yield(line)
    }

    /// Wait for and consume the next line written by the client.
    func nextWritten() async -> String {
        await withCheckedContinuation { (cont: CheckedContinuation<String, Never>) in
            lock.lock()
            if !written.isEmpty {
                let v = written.removeFirst()
                lock.unlock()
                cont.resume(returning: v)
                return
            }
            writeWaiters.append(cont)
            lock.unlock()
        }
    }
}

final class JsonRpcTests: XCTestCase {

    func testCallSuccessRoundTrip() async throws {
        let transport = TestTransport()
        let client = JsonRpcClient()
        await client.attach(transport)

        async let result = client.call("ping", params: .object(["foo": .string("bar")]))

        let written = await transport.nextWritten()
        let req = try JSONValue.decode(written)
        XCTAssertEqual(req["jsonrpc"].stringValue, "2.0")
        XCTAssertEqual(req["method"].stringValue, "ping")
        XCTAssertEqual(req["params"]?["foo"].stringValue, "bar")
        let id = req["id"].intValue!

        let response: JSONValue = .object([
            "jsonrpc": .string("2.0"),
            "id": .int(id),
            "result": .object(["pong": .bool(true)]),
        ])
        transport.feed(try response.encodedString())

        let r = try await result
        XCTAssertEqual(r["pong"].boolValue, true)
        await client.close()
    }

    func testCallErrorResponse() async throws {
        let transport = TestTransport()
        let client = JsonRpcClient()
        await client.attach(transport)

        async let result = client.call("agent.run", params: .object(["prompt": .string("hi")]))

        let written = await transport.nextWritten()
        let req = try JSONValue.decode(written)
        let id = req["id"].intValue!

        let response: JSONValue = .object([
            "jsonrpc": .string("2.0"),
            "id": .int(id),
            "error": .object([
                "code": .int(-32002),
                "message": .string("already running"),
            ]),
        ])
        transport.feed(try response.encodedString())

        do {
            _ = try await result
            XCTFail("expected AgentBusy")
        } catch let e as AgentBusy {
            XCTAssertEqual(e.code, -32002)
            XCTAssertEqual(e.message, "already running")
        }
        await client.close()
    }

    func testNotificationDispatch() async throws {
        let transport = TestTransport()
        let client = JsonRpcClient()
        await client.attach(transport)

        let received = Sink<String>()
        await client.setNotificationHandler("stream.delta") { params in
            received.send(params["text"].stringValue ?? "")
        }

        let payload: JSONValue = .object([
            "jsonrpc": .string("2.0"),
            "method": .string("stream.delta"),
            "params": .object(["text": .string("hello"), "turn": .int(1)]),
        ])
        transport.feed(try payload.encodedString())

        let text = try await received.firstWithTimeout(seconds: 1.0)
        XCTAssertEqual(text, "hello")
        await client.close()
    }

    func testRequestHandlerInvocation() async throws {
        let transport = TestTransport()
        let client = JsonRpcClient()
        await client.attach(transport)

        await client.setRequestHandler("tool.execute") { params in
            let name = params["name"].stringValue ?? ""
            return .object(["content": .string("ran \(name)"), "is_error": .bool(false)])
        }

        let payload: JSONValue = .object([
            "jsonrpc": .string("2.0"),
            "id": .int(99),
            "method": .string("tool.execute"),
            "params": .object(["name": .string("read_file"), "args": .object([:])]),
        ])
        transport.feed(try payload.encodedString())

        let response = await transport.nextWritten()
        let r = try JSONValue.decode(response)
        XCTAssertEqual(r["id"].intValue, 99)
        XCTAssertEqual(r["result"]?["content"].stringValue, "ran read_file")
        await client.close()
    }

    func testCallTimeout() async throws {
        let transport = TestTransport()
        let client = JsonRpcClient()
        await client.attach(transport)

        async let result: JSONValue = client.call("slow", timeout: 0.1)

        // Drain the outgoing message so the test doesn't leak the writer.
        _ = await transport.nextWritten()

        do {
            _ = try await result
            XCTFail("expected timeout")
        } catch let e as AgentError {
            XCTAssertTrue(e.message.contains("timeout"))
        }
        await client.close()
    }
}

/// Minimal thread-safe single-shot sink used by the notification test.
final class Sink<T: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var value: T?
    private var waiters: [CheckedContinuation<T, Never>] = []

    func send(_ v: T) {
        lock.lock()
        if !waiters.isEmpty {
            let w = waiters.removeFirst()
            lock.unlock()
            w.resume(returning: v)
            return
        }
        value = v
        lock.unlock()
    }

    func firstWithTimeout(seconds: Double) async throws -> T {
        return try await withThrowingTaskGroup(of: T.self) { group in
            group.addTask {
                await withCheckedContinuation { (cont: CheckedContinuation<T, Never>) in
                    self.lock.lock()
                    if let v = self.value {
                        self.value = nil
                        self.lock.unlock()
                        cont.resume(returning: v)
                        return
                    }
                    self.waiters.append(cont)
                    self.lock.unlock()
                }
            }
            group.addTask {
                try await Task.sleep(nanoseconds: UInt64(seconds * 1_000_000_000))
                throw AgentError("sink timeout")
            }
            let first = try await group.next()!
            group.cancelAll()
            return first
        }
    }
}
