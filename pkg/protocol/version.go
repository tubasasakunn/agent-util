package protocol

// LibraryVersion は ai-agent ライブラリ/プロトコル契約のセマンティックバージョン。
//
// JSON-RPC 仕様自体のバージョン（"2.0"）を表す Version 定数とは
// 別物である。Version は JSON-RPC の wire protocol、LibraryVersion は
// このリポジトリ全体のリリースバージョンを示す。
//
// SDK ホップで一致を保証するため pkg/protocol を真実の源とする。
// バージョン更新時は docs/VERSIONING.md の「バージョン同期」表にある
// 全ての箇所を同じ値に更新すること:
//
//   - pkg/protocol/version.go (本ファイル)
//   - sdk/python/pyproject.toml
//   - sdk/js/package.json
//   - sdk/js-browser/package.json
//   - README.md のバッジ
//   - CLAUDE.md の `## バージョン` セクション
//   - CHANGELOG.md の見出し
//
// Swift SDK (sdk/swift) は SwiftPM の慣習に従い Git tag (v0.2.0 等) で
// バージョン管理する。Package.swift にはバージョン値を書かない。
const LibraryVersion = "0.2.0"
