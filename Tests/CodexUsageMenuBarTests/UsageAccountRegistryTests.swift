import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Usage account registry")
struct UsageAccountRegistryTests {
  @Test("Identifies the same account without retaining its raw identifier")
  func accountIdentityFingerprint() throws {
    let root = temporaryRoot()
    defer { try? FileManager.default.removeItem(at: root) }
    let firstHome = root.appending(path: "first", directoryHint: .isDirectory)
    let secondHome = root.appending(path: "second", directoryHint: .isDirectory)
    try FileManager.default.createDirectory(at: firstHome, withIntermediateDirectories: true)
    try FileManager.default.createDirectory(at: secondHome, withIntermediateDirectories: true)
    try Data(#"{"tokens":{"account_id":"Workspace-123"}}"#.utf8)
      .write(to: firstHome.appending(path: "auth.json"))
    try Data(#"{"tokens":{"account_id":"workspace-123"}}"#.utf8)
      .write(to: secondHome.appending(path: "auth.json"))

    let reader = CodexAccountIdentityReader()
    let first = try #require(reader.fingerprint(codexHomeURL: firstHome))
    let second = try #require(reader.fingerprint(codexHomeURL: secondHome))

    #expect(first == second)
    #expect(first != "workspace-123")
    #expect(first.count == 64)
  }

  @Test("Allows one login in different workspaces while blocking exact duplicates")
  func duplicateAccountMatching() {
    let exact = CodexAccountIdentity(
      fingerprint: "ACCOUNT-A",
      email: "Owner@Example.com",
      planType: "team"
    )
    let sameWorkspace = CodexAccountIdentity(
      fingerprint: "account-a",
      email: "owner@example.com",
      planType: "business"
    )
    let anotherWorkspace = CodexAccountIdentity(
      fingerprint: "account-b",
      email: "owner@example.com",
      planType: "business"
    )
    let anotherMember = CodexAccountIdentity(
      fingerprint: "account-a",
      email: "member@example.com",
      planType: "business"
    )
    let fallback = CodexAccountIdentity(
      fingerprint: nil,
      email: " owner@example.com ",
      planType: "self_serve_business_usage_based"
    )

    #expect(exact.matches(sameWorkspace))
    #expect(!exact.matches(anotherWorkspace))
    #expect(!exact.matches(anotherMember))
    #expect(exact.matches(fallback))
  }

  @Test("Uses a user-defined workspace name while keeping a short local reference")
  func workspaceDisplayName() {
    var account = UsageAccount(
      id: UUID().uuidString,
      kind: .managed,
      displayName: nil,
      lastKnownEmail: "owner@example.com",
      lastKnownPlanType: "business",
      lastKnownWorkspaceFingerprint:
        "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
      createdAt: Date()
    )

    #expect(account.workspaceReference == "ABCDEF12")
    #expect(account.workspaceDisplayLabel == "워크스페이스 #ABCDEF12")
    #expect(account.workspaceReference?.contains("34567890") == false)

    account.workspaceName = "  개발팀  "
    #expect(account.normalizedWorkspaceName == "개발팀")
    #expect(account.workspaceDisplayLabel == "워크스페이스 개발팀")
    #expect(account.workspaceReference == "ABCDEF12")
  }

  @Test("Loads account records written before workspace metadata existed")
  func decodesLegacyAccountWithoutWorkspaceFingerprint() throws {
    let data = Data(
      """
      {
        "id": "00000000-0000-0000-0000-000000000001",
        "kind": "managed",
        "displayName": null,
        "lastKnownEmail": "owner@example.com",
        "lastKnownPlanType": "business",
        "createdAt": 0
      }
      """.utf8
    )

    let account = try JSONDecoder().decode(UsageAccount.self, from: data)

    #expect(account.lastKnownWorkspaceFingerprint == nil)
    #expect(account.workspaceName == nil)
    #expect(account.workspaceReference == nil)
  }

  @Test("Loads a version-one registry before workspace display names existed")
  func loadsLegacyRegistryPayload() throws {
    let root = temporaryRoot()
    defer { try? FileManager.default.removeItem(at: root) }
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    try Data(
      """
      {
        "version": 1,
        "accounts": [{
          "id": "00000000-0000-0000-0000-000000000001",
          "kind": "managed",
          "displayName": null,
          "lastKnownEmail": "owner@example.com",
          "lastKnownPlanType": "business",
          "createdAt": "2026-08-27T00:00:00Z"
        }]
      }
      """.utf8
    ).write(to: root.appending(path: "accounts.json"))

    let accounts = try UsageAccountRegistry(applicationSupportURL: root).loadAccounts()

    #expect(accounts.count == 2)
    #expect(accounts[0].isSystemDefault)
    #expect(accounts[0].workspaceName == nil)
    #expect(accounts[1].lastKnownEmail == "owner@example.com")
    #expect(accounts[1].workspaceName == nil)
  }

  @Test("Persists workspace names for both default and managed connections")
  func persistsWorkspaceNames() throws {
    let root = temporaryRoot()
    defer { try? FileManager.default.removeItem(at: root) }
    let registry = UsageAccountRegistry(applicationSupportURL: root)

    var defaultAccount = try #require(registry.loadAccounts().first)
    defaultAccount.workspaceName = "  개인  "
    try registry.updateAccount(defaultAccount)

    var managedAccount = try registry.beginManagedAccount()
    managedAccount.workspaceName = "개발팀"
    try registry.commitPendingAccount(managedAccount)

    let restored = try registry.loadAccounts()
    #expect(restored[0].normalizedWorkspaceName == "개인")
    #expect(restored[0].workspaceDisplayLabel == "워크스페이스 개인")
    #expect(restored[1].normalizedWorkspaceName == "개발팀")
    #expect(restored[1].workspaceDisplayLabel == "워크스페이스 개발팀")

    defaultAccount = restored[0]
    defaultAccount.workspaceName = "   "
    try registry.updateAccount(defaultAccount)
    #expect(try registry.loadAccounts()[0].workspaceName == nil)
  }

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
    account.lastKnownWorkspaceFingerprint =
      "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
    try registry.commitPendingAccount(account)

    let loaded = try registry.loadAccounts()
    #expect(loaded.first?.id == UsageAccount.systemDefaultID)
    #expect(loaded.count == 2)
    #expect(loaded[1].id == account.id)
    #expect(loaded[1].displayName == account.displayName)
    #expect(loaded[1].lastKnownEmail == account.lastKnownEmail)
    #expect(loaded[1].lastKnownPlanType == account.lastKnownPlanType)
    #expect(
      loaded[1].lastKnownWorkspaceFingerprint == account.lastKnownWorkspaceFingerprint
    )

    let managedHome = try registry.managedCodexHomeURL(for: account)
    #expect(FileManager.default.fileExists(atPath: managedHome.path))
    #expect(!FileManager.default.fileExists(atPath: pendingHome.path))

    try registry.removeManagedAccount(account)
    #expect(try registry.loadAccounts().map(\.id) == [UsageAccount.systemDefaultID])
    #expect(!FileManager.default.fileExists(atPath: managedHome.path))
  }

  @Test("Keeps workspaces from one ChatGPT login in separate Codex homes")
  func separatesWorkspaceSessions() throws {
    let root = temporaryRoot()
    defer { try? FileManager.default.removeItem(at: root) }
    let registry = UsageAccountRegistry(applicationSupportURL: root)

    let first = try registry.beginManagedAccount()
    let firstPendingHome = try registry.pendingCodexHomeURL(for: first)
    try Data(#"{"tokens":{"account_id":"workspace-a"}}"#.utf8)
      .write(to: firstPendingHome.appending(path: "auth.json"))
    try registry.commitPendingAccount(first)

    let second = try registry.beginManagedAccount()
    let secondPendingHome = try registry.pendingCodexHomeURL(for: second)
    try Data(#"{"tokens":{"account_id":"workspace-b"}}"#.utf8)
      .write(to: secondPendingHome.appending(path: "auth.json"))
    try registry.commitPendingAccount(second)

    let firstHome = try registry.managedCodexHomeURL(for: first)
    let secondHome = try registry.managedCodexHomeURL(for: second)
    let reader = CodexAccountIdentityReader()

    #expect(firstHome != secondHome)
    #expect(
      reader.fingerprint(codexHomeURL: firstHome)
        != reader.fingerprint(codexHomeURL: secondHome)
    )
    #expect(try registry.loadAccounts().filter(\.isManaged).count == 2)
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
