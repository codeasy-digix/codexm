// swift-tools-version: 6.0

import PackageDescription

let package = Package(
  name: "CodexUsageMenuBar",
  platforms: [
    .macOS(.v13)
  ],
  products: [
    .executable(name: "CodexUsageMenuBar", targets: ["CodexUsageMenuBar"])
  ],
  targets: [
    .executableTarget(
      name: "CodexUsageMenuBar",
      path: "Sources/CodexUsageMenuBar"
    ),
    .testTarget(
      name: "CodexUsageMenuBarTests",
      dependencies: ["CodexUsageMenuBar"],
      path: "Tests/CodexUsageMenuBarTests"
    ),
  ]
)
