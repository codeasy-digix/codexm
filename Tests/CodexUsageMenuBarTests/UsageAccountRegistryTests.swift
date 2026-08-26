import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Usage account registry")
struct UsageAccountRegistryTests {
  @Test("Creates, commits, reloads, and removes an isolated managed Codex home")
  func managedAccountLifecycle() throws {
    let root = temporaryRoot()
    defer { try? FileManager.default.removeItem(at: root) }
    let registry = UsageAccountRegistry(applicationSupportURL: root)

    var account = try registry.beginManagedAccount()
    let pendingHome = try registry.pendingCodexHomeURL(for: account)
    let config = try String(contentsOf: pendingHome.appending(path: "config.toml"), encoding: .utf8)
    #expect(config == "cli_auth_credentials_store = \"file\"\n")

    account.displayName = "업무 계정"
    account.lastKnownEmail = "work@example.com"
    account.lastKnownPlanType = "business"
    try registry.commitPendingAccount(account)

    let loaded = try registry.loadAccounts()
    #expect(loaded.first?.id == UsageAccount.systemDefaultID)
    #expect(loaded.count == 2)
    #expect(loaded[1].id == account.id)
    #expect(loaded[1].displayName == account.displayName)
    #expect(loaded[1].lastKnownEmail == account.lastKnownEmail)
    #expect(loaded[1].lastKnownPlanType == account.lastKnownPlanType)

    let managedHome = try registry.managedCodexHomeURL(for: account)
    #expect(FileManager.default.fileExists(atPath: managedHome.path))
    #expect(!FileManager.default.fileExists(atPath: pendingHome.path))

    try registry.removeManagedAccount(account)
    #expect(try registry.loadAccounts().map(\.id) == [UsageAccount.systemDefaultID])
    #expect(!FileManager.default.fileExists(atPath: managedHome.path))
  }

  @Test("Migrates the legacy app-owned Codex home as a managed account")
  func migratesLegacyHome() throws {
    let root = temporaryRoot()
    defer { try? FileManager.default.removeItem(at: root) }
    let legacyHome = root.appending(path: "CodexHome", directoryHint: .isDirectory)
    try FileManager.default.createDirectory(at: legacyHome, withIntermediateDirectories: true)
    try Data("{}".utf8).write(to: legacyHome.appending(path: "auth.json"))

    let registry = UsageAccountRegistry(applicationSupportURL: root)
    let accounts = try registry.loadAccounts()

    #expect(accounts.count == 2)
    #expect(accounts[1].isManaged)
    #expect(accounts[1].displayName == "이전 앱 계정")
    #expect(!FileManager.default.fileExists(atPath: legacyHome.path))
    #expect(
      FileManager.default.fileExists(
        atPath: try registry.managedCodexHomeURL(for: accounts[1]).appending(path: "auth.json").path
      )
    )
  }

  @Test("Removes an unfinished staged login on the next launch")
  func removesAbandonedPendingLogin() throws {
    let root = temporaryRoot()
    defer { try? FileManager.default.removeItem(at: root) }
    let registry = UsageAccountRegistry(applicationSupportURL: root)
    let account = try registry.beginManagedAccount()
    let pendingHome = try registry.pendingCodexHomeURL(for: account)
    try Data("token-placeholder".utf8).write(to: pendingHome.appending(path: "auth.json"))

    let loaded = try registry.loadAccounts()

    #expect(loaded.map(\.id) == [UsageAccount.systemDefaultID])
    #expect(!FileManager.default.fileExists(atPath: pendingHome.path))
  }

  private func temporaryRoot() -> URL {
    FileManager.default.temporaryDirectory.appending(
      path: "UsageAccountRegistryTests-\(UUID().uuidString)",
      directoryHint: .isDirectory
    )
  }
}
