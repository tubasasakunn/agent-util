// swift-tools-version:5.9
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
            path: "Sources/AIAgent"
        ),
        .testTarget(
            name: "AIAgentTests",
            dependencies: ["AIAgent"],
            path: "Tests/AIAgentTests"
        ),
    ]
)
