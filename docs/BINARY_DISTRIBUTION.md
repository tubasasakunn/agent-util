# ai-agent バイナリ配布レシピ (F5)

Swift SDK / Python SDK / JS SDK のいずれを使う場合も、最終的に
`./agent --rpc` を起動する Go バイナリが必要になる。利用者向けにこの
バイナリをどう同梱・配布するかの実用パターンをまとめる。

## ビルド

```bash
# シングルプラットフォーム (自分の Mac 用)
go build -o agent ./cmd/agent/

# Apple Silicon + Intel の Universal Binary
GOOS=darwin GOARCH=arm64 go build -o agent-arm64 ./cmd/agent/
GOOS=darwin GOARCH=amd64 go build -o agent-x86_64 ./cmd/agent/
lipo -create -output agent agent-arm64 agent-x86_64
file agent
# → Mach-O universal binary with 2 architectures
```

`-trimpath` と `-ldflags "-s -w"` を付けるとサイズが半分以下になる:

```bash
go build -trimpath -ldflags "-s -w" -o agent ./cmd/agent/
```

## CI で配布バイナリを作る (GitHub Actions 例)

```yaml
# .github/workflows/release.yml
name: release
on:
  push:
    tags: ["v*"]

jobs:
  build:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"

      - name: Build universal binary
        run: |
          GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" \
              -o dist/agent-arm64 ./cmd/agent/
          GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w" \
              -o dist/agent-x86_64 ./cmd/agent/
          lipo -create -output dist/agent dist/agent-arm64 dist/agent-x86_64
          chmod +x dist/agent

      - name: Codesign (ad-hoc, no Apple Developer ID)
        run: codesign --force --sign - dist/agent

      - name: Upload to release
        uses: softprops/action-gh-release@v2
        with:
          files: dist/agent
```

公式 Developer ID 証明書があれば `codesign --sign "Developer ID Application: ..."`
を使う。アプリ単体配布なら ad-hoc 署名でも Gatekeeper を通せる
(初回起動時に右クリック「開く」)。

## Swift アプリへの同梱パターン

### A. Resources に同梱して初回起動で展開

XCode プロジェクトの **Bundle Resources** に `agent` バイナリを追加し、
初回起動時に Application Support にコピー + chmod する:

```swift
import Foundation

enum AgentBinary {
    /// `~/Library/Application Support/<bundle id>/agent` のフルパスを返す。
    /// 必要なら Resources から展開して chmod する。
    static func ensureInstalled() throws -> String {
        let fm = FileManager.default
        let appSupport = try fm.url(
            for: .applicationSupportDirectory,
            in: .userDomainMask,
            appropriateFor: nil,
            create: true
        )
        let bundleID = Bundle.main.bundleIdentifier ?? "ai-agent-app"
        let dir = appSupport.appendingPathComponent(bundleID, isDirectory: true)
        try? fm.createDirectory(at: dir, withIntermediateDirectories: true)

        let dst = dir.appendingPathComponent("agent")
        guard let src = Bundle.main.url(forResource: "agent", withExtension: nil) else {
            throw NSError(domain: "AgentBinary", code: 1,
                          userInfo: [NSLocalizedDescriptionKey: "agent resource not found in bundle"])
        }

        // バージョンが変わった可能性があるので毎回上書きコピー
        if fm.fileExists(atPath: dst.path) {
            try? fm.removeItem(at: dst)
        }
        try fm.copyItem(at: src, to: dst)

        // 実行権限を付与 (rwxr-xr-x)
        try fm.setAttributes(
            [.posixPermissions: 0o755],
            ofItemAtPath: dst.path
        )

        return dst.path
    }
}

// 使用側
let path = try AgentBinary.ensureInstalled()
let agent = Agent(config: AgentConfig(binary: path))
try await agent.start()
```

### B. ユーザに `~/.local/bin/agent` を要求

開発者向けツールなら同梱せず、起動時に "Install agent to /usr/local/bin/agent"
を案内する方が簡潔。`AgentConfig.binary: "/usr/local/bin/agent"` で固定する。

## アーキテクチャ判定

ユニバーサルではなく単一アーキを配るときの確認:

```swift
#if arch(arm64)
let resourceName = "agent-arm64"
#elseif arch(x86_64)
let resourceName = "agent-x86_64"
#else
#error("unsupported architecture")
#endif
```

## Sandbox / Hardened Runtime

App Sandbox を有効にした Mac アプリから `Process` (NSTask) を使う場合、
**`com.apple.security.inherit` または `com.apple.security.temporary-exception.unix-domain-socket`** が必要なケースがある。子プロセスとの双方向 pipe 通信は
Hardened Runtime + Sandbox の組み合わせで動作するが、`get-task-allow=NO` の
リリースビルドではデバッガアタッチが効かないので、`SLLM_ENDPOINT` の HTTP 不通や
RPC タイムアウトが何由来か追いにくい。`AGENT_RPC_TRACE=1` で stderr 監視する
のが手っ取り早い (D5)。

## iOS / Mac Catalyst

`Process` は iOS では未対応。Swift SDK は `NSClassFromString("NSTask")`
ランタイム経由で macOS / Mac Catalyst で動作するが、iOS では
`connectSubprocess` が実行時エラーになる (F3 の制約)。

iOS で使いたい場合は **Go バイナリの代わりに HTTP ベースのリモート ai-agent
サーバ** を立てて、`AgentConfig.binary` を使わずに自前で JSON-RPC over WebSocket
を実装する設計に切り替える必要がある (将来サポート予定)。

## 配布チェックリスト

- [ ] `go build` に `-trimpath -ldflags "-s -w"` を付けたか
- [ ] Apple Silicon と Intel 両方をユニバーサル化したか (または明示的に判別)
- [ ] `codesign --force --sign -` (または Developer ID) で署名済みか
- [ ] バイナリと SDK のバージョンが一致しているか
      (`AgentConfig.versionCheck: .strict` でハンドシェイク)
- [ ] 初回起動で Application Support にコピー + chmod 755 されるか
- [ ] App Sandbox 設定が `Process` 起動を許可しているか
- [ ] `AGENT_RPC_TRACE=1` で通信ログを取れる経路を提供しているか
