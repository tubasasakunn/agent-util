import Foundation

/// 会話メッセージをTF-IDFでインデックス化して検索する。
///
/// 外部依存なし。埋め込みAPIが使えない環境でも動作するRAG用検索器。
public actor MessageIndex {
    private struct Document {
        var role: String
        var content: String
        var meta: [String: JSONValue]
        var id: Int
    }

    private var docs: [Document] = []
    private var tfs: [[String: Double]] = []
    private var idf: [String: Double] = [:]
    private var dirty: Bool = false

    public init() {}

    public func add(role: String, content: String, meta: [String: JSONValue] = [:]) {
        let doc = Document(role: role, content: content, meta: meta, id: docs.count)
        docs.append(doc)
        tfs.append(Self.computeTF(content))
        dirty = true
    }

    public func count() -> Int { docs.count }

    public func search(_ query: String, topK: Int = 5) -> [SearchHit] {
        rebuildIdfIfNeeded()
        let qTF = Self.computeTF(query)
        guard !qTF.isEmpty else { return [] }

        var scores: [(Double, Int)] = []
        for (i, tf) in tfs.enumerated() {
            let keys = Set(qTF.keys).union(tf.keys)
            var score = 0.0
            for t in keys {
                let q = qTF[t] ?? 0
                let d = tf[t] ?? 0
                let i = idf[t] ?? 1
                score += q * d * i
            }
            if score > 0 { scores.append((score, i)) }
        }
        scores.sort { $0.0 > $1.0 }
        return scores.prefix(topK).map { (score, i) in
            let d = docs[i]
            return SearchHit(role: d.role, content: d.content, score: score, id: d.id)
        }
    }

    public func allMessages() -> [IndexedMessage] {
        docs.map { IndexedMessage(role: $0.role, content: $0.content, meta: $0.meta) }
    }

    /// スナップショットを返し、別のactorで `restore(from:)` を呼ぶことでコピーできる。
    public func snapshot() -> MessageIndexSnapshot {
        MessageIndexSnapshot(docs: docs.map {
            IndexedMessage(role: $0.role, content: $0.content, meta: $0.meta)
        })
    }

    public func restore(from snapshot: MessageIndexSnapshot) {
        docs = []
        tfs = []
        idf = [:]
        dirty = false
        for entry in snapshot.docs {
            add(role: entry.role, content: entry.content, meta: entry.meta)
        }
    }

    private func rebuildIdfIfNeeded() {
        guard dirty else { return }
        let n = docs.count
        var df: [String: Int] = [:]
        for tf in tfs {
            for t in tf.keys { df[t, default: 0] += 1 }
        }
        var newIdf: [String: Double] = [:]
        for (t, d) in df {
            newIdf[t] = log(Double(n + 1) / Double(d + 1)) + 1
        }
        idf = newIdf
        dirty = false
    }

    // MARK: - TF / トークナイザ

    private static func tokenize(_ text: String) -> [String] {
        let separators = CharacterSet(charactersIn: " \t\n\r.,;:!?、。！？[]()（）「」『』")
        let lower = text.lowercased()
        let phrases = lower.components(separatedBy: separators).filter { !$0.isEmpty }
        var tokens: [String] = []
        for phrase in phrases {
            tokens.append(phrase)
            // CJK文字は単字も追加
            for ch in phrase {
                if isCJK(ch) {
                    tokens.append(String(ch))
                }
            }
        }
        return tokens
    }

    private static func isCJK(_ ch: Character) -> Bool {
        for scalar in ch.unicodeScalars {
            let v = scalar.value
            if (0x4E00...0x9FFF).contains(v) { return true }   // 漢字
            if (0x3040...0x309F).contains(v) { return true }   // ひらがな
            if (0x30A0...0x30FF).contains(v) { return true }   // カタカナ
        }
        return false
    }

    private static func computeTF(_ text: String) -> [String: Double] {
        let tokens = tokenize(text)
        guard !tokens.isEmpty else { return [:] }
        var counts: [String: Int] = [:]
        for t in tokens { counts[t, default: 0] += 1 }
        let total = Double(tokens.count)
        var tf: [String: Double] = [:]
        for (t, c) in counts { tf[t] = Double(c) / total }
        return tf
    }
}

public struct SearchHit: Sendable, Equatable {
    public let role: String
    public let content: String
    public let score: Double
    public let id: Int
}

public struct IndexedMessage: Sendable {
    public let role: String
    public let content: String
    public let meta: [String: JSONValue]

    public init(role: String, content: String, meta: [String: JSONValue] = [:]) {
        self.role = role
        self.content = content
        self.meta = meta
    }
}

/// `MessageIndex` のスナップショット (Sendable)。actorを跨いで状態を移送するために使う。
public struct MessageIndexSnapshot: Sendable {
    public let docs: [IndexedMessage]

    public init(docs: [IndexedMessage]) {
        self.docs = docs
    }
}
