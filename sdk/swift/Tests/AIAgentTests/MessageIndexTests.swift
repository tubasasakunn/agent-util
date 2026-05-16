import XCTest
@testable import AIAgent

final class MessageIndexTests: XCTestCase {
    func testEmptySearch() async {
        let idx = MessageIndex()
        let hits = await idx.search("anything")
        XCTAssertEqual(hits.count, 0)
    }

    func testJapaneseSearch() async {
        let idx = MessageIndex()
        await idx.add(role: "user", content: "東京の天気はどうですか")
        await idx.add(role: "assistant", content: "東京は晴れです")
        await idx.add(role: "user", content: "明日の予定は")
        let hits = await idx.search("東京", topK: 2)
        XCTAssertGreaterThan(hits.count, 0)
        XCTAssertTrue(hits[0].content.contains("東京"))
    }

    func testSnapshotAndRestore() async {
        let original = MessageIndex()
        await original.add(role: "user", content: "hello")
        await original.add(role: "assistant", content: "world")
        let snap = await original.snapshot()
        XCTAssertEqual(snap.docs.count, 2)

        let restored = MessageIndex()
        await restored.restore(from: snap)
        let hits = await restored.search("hello")
        XCTAssertGreaterThan(hits.count, 0)
    }

    func testRestoreReplacesExisting() async {
        let idx = MessageIndex()
        await idx.add(role: "user", content: "discard me")
        let snap = MessageIndexSnapshot(docs: [IndexedMessage(role: "user", content: "new content")])
        await idx.restore(from: snap)
        let all = await idx.allMessages()
        XCTAssertEqual(all.count, 1)
        XCTAssertEqual(all[0].content, "new content")
    }
}
