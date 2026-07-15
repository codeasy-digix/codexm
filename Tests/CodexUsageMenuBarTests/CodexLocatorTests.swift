import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Codex CLI discovery")
struct CodexLocatorTests {
  @Test("Honors an explicit CODEX_CLI_PATH")
  func explicitOverride() throws {
    let root = FileManager.default.temporaryDirectory
      .appending(path: UUID().uuidString, directoryHint: .isDirectory)
    let executable = root.appending(path: "codex")
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }

    try Data("#!/bin/sh\n".utf8).write(to: executable)
    try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: executable.path)

    let locator = CodexLocator(
      environment: ["CODEX_CLI_PATH": executable.path, "PATH": ""],
      homeDirectory: root
    )

    #expect(locator.locate()?.path == executable.path)
  }

  @Test("Finds Codex installed under nvm for Finder-launched apps")
  func nvmDiscovery() throws {
    let home = FileManager.default.temporaryDirectory
      .appending(path: UUID().uuidString, directoryHint: .isDirectory)
    let older = home.appending(path: ".nvm/versions/node/v20.1.0/bin/codex")
    let newer = home.appending(path: ".nvm/versions/node/v25.9.0/bin/codex")
    defer { try? FileManager.default.removeItem(at: home) }

    for executable in [older, newer] {
      try FileManager.default.createDirectory(
        at: executable.deletingLastPathComponent(),
        withIntermediateDirectories: true
      )
      try Data("#!/bin/sh\n".utf8).write(to: executable)
      try FileManager.default.setAttributes(
        [.posixPermissions: 0o755], ofItemAtPath: executable.path)
    }

    let locator = CodexLocator(environment: ["PATH": ""], homeDirectory: home)
    #expect(locator.locate()?.path == newer.path)
  }

  @Test("Preserves an nvm launcher symlink so adjacent node stays discoverable")
  func preservesNvmSymlink() throws {
    let home = FileManager.default.temporaryDirectory
      .appending(path: UUID().uuidString, directoryHint: .isDirectory)
    let bin = home.appending(path: ".nvm/versions/node/v25.9.0/bin")
    let script = home.appending(
      path: ".nvm/versions/node/v25.9.0/lib/node_modules/@openai/codex/bin/codex.js"
    )
    let launcher = bin.appending(path: "codex")
    defer { try? FileManager.default.removeItem(at: home) }

    try FileManager.default.createDirectory(
      at: script.deletingLastPathComponent(),
      withIntermediateDirectories: true
    )
    try FileManager.default.createDirectory(at: bin, withIntermediateDirectories: true)
    try Data("#!/usr/bin/env node\n".utf8).write(to: script)
    try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
    try FileManager.default.createSymbolicLink(at: launcher, withDestinationURL: script)

    let locator = CodexLocator(environment: ["PATH": ""], homeDirectory: home)
    #expect(locator.locate()?.path == launcher.path)
  }
}
