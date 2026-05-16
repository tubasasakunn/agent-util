// swift-tools-version:5.9
//
// SwiftPM が外部リポジトリから依存解決する際は、リポジトリのルートに
// Package.swift が必要なため、本ファイルを置いている。
// 実体ソースは sdk/swift/ 配下にある。
//
// 利用側 (Package.swift の dependencies):
//   .package(url: "https://github.com/tubasasakunn/agent-util.git", from: "0.2.1"),
// ターゲット依存:
//   .product(name: "AIAgent", package: "agent-util"),

import PackageDescription

let package = Package(
    name: "AIAgent",
    platforms: [
        .macOS(.v13),
    ],
    products: [
        .library(name: "AIAgent", targets: ["AIAgent"]),
    ],
    targets: [
        .target(
            name: "AIAgent",
            path: "sdk/swift/Sources/AIAgent"
        ),
        .testTarget(
            name: "AIAgentTests",
            dependencies: ["AIAgent"],
            path: "sdk/swift/Tests/AIAgentTests"
        ),
    ]
)
