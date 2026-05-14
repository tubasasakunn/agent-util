/// `AIAgent` — Swift SDK for the ai-agent Go harness.
///
/// Thin JSON-RPC 2.0 client over stdio. See `README.md` for Quickstart.
///
/// The public surface mirrors the sibling SDKs at `sdk/python/` and
/// `sdk/js/`, so the same `agent --rpc` binary can be driven from any of
/// the three languages.
import Foundation

/// Library version. Must stay in sync with:
///
/// * `pkg/protocol/version.go` (`protocol.LibraryVersion`)
/// * `sdk/python/pyproject.toml`
/// * `sdk/js/package.json`
/// * the project `README.md` badge
/// * the `## バージョン` section of `CLAUDE.md`
/// * the matching `CHANGELOG.md` heading
public enum AIAgent {
    public static let version: String = "0.1.0"
}
