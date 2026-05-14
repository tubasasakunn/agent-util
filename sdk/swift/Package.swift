// swift-tools-version:5.9
//
// Swift Package manifest for the ai-agent Swift SDK.
//
// Mirrors the protocol exposed by the Go core (`pkg/protocol/methods.go`)
// and the sibling SDKs under `sdk/python/` and `sdk/js/`. See README.md
// for usage.

import PackageDescription

let package = Package(
    name: "AIAgent",
    platforms: [
        .macOS(.v13),
    ],
    products: [
        .library(
            name: "AIAgent",
            targets: ["AIAgent"]
        ),
    ],
    targets: [
        .target(
            name: "AIAgent",
            path: "Sources/AIAgent"
        ),
        .testTarget(
            name: "AIAgentTests",
            dependencies: ["AIAgent"],
            path: "Tests/AIAgentTests"
        ),
    ],
    swiftLanguageVersions: [.v5]
)
