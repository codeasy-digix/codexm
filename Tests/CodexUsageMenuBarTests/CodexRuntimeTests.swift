import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Bundled Codex runtime")
struct CodexRuntimeTests {
  @Test("Exposes the existing Codex home only as read-only identity access")
  func readsExistingLoginIdentity() throws {
    let fixture = try Fixture(authenticated: true)
    defer { fixture.remove() }

    let access = try fixture.locator.locateSystemCodexHomeIdentity()

    #expect(access.codexHomeURL == fixture.systemCodexHome)
  }

  @Test("Keeps the default account disconnected when the machine has no Codex login")
  func requiresExistingDefaultLogin() throws {
    let fixture = try Fixture(authenticated: false)
    defer { fixture.remove() }

    #expect(throws: CodexRuntimeError.defaultCodexHomeUnavailable) {
      try fixture.locator.locateSystemCodexHomeIdentity()
    }
  }

  @Test("Runs the default connection only from its app-owned isolated Codex home")
  func usesIsolatedDefaultAccountHome() throws {
    let fixture = try Fixture(authenticated: true)
    defer { fixture.remove() }
    let registry = UsageAccountRegistry(applicationSupportURL: fixture.applicationSupport)
    let isolatedHome = try registry.systemDefaultCodexHomeURL()

    let runtime = try fixture.locator.locateManagedAccount(codexHomeURL: isolatedHome)

    #expect(runtime.executableURL == fixture.executable)
    #expect(runtime.codexHomeURL == isolatedHome)
    #expect(runtime.codexHomeURL != fixture.systemCodexHome)
    #expect(runtime.environment["CODEX_HOME"] == isolatedHome.path)
    #expect(runtime.environment["PATH"] == "/usr/bin:/bin:/usr/sbin:/sbin")
  }

  @Test("Uses an explicitly managed Codex home for an added account")
  func usesManagedAccountHome() throws {
    let fixture = try Fixture(authenticated: false)
    defer { fixture.remove() }
    let registry = UsageAccountRegistry(applicationSupportURL: fixture.applicationSupport)
    let account = try registry.beginManagedAccount()
    let home = try registry.pendingCodexHomeURL(for: account)

    let runtime = try fixture.locator.locateManagedAccount(codexHomeURL: home)

    #expect(runtime.codexHomeURL == home)
    #expect(runtime.environment["CODEX_HOME"] == home.path)
    #expect(FileManager.default.fileExists(atPath: home.appending(path: "config.toml").path))
  }
}

private struct Fixture {
  let root: URL
  let executable: URL
  let applicationSupport: URL
  let systemCodexHome: URL
  let defaults: UserDefaults
  let locator: CodexRuntimeLocator

  init(authenticated: Bool) throws {
    root = FileManager.default.temporaryDirectory.appending(
      path: UUID().uuidString,
      directoryHint: .isDirectory
    )
    executable = root.appending(path: "CodexRuntime")
    applicationSupport = root.appending(path: "Application Support/Codex Usage")
    systemCodexHome = root.appending(path: ".codex", directoryHint: .isDirectory)

    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    try Data("native runtime".utf8).write(to: executable)
    try FileManager.default.setAttributes(
      [.posixPermissions: 0o755],
      ofItemAtPath: executable.path
    )
    if authenticated {
      try FileManager.default.createDirectory(
        at: systemCodexHome,
        withIntermediateDirectories: true
      )
      try Data("{}".utf8).write(to: systemCodexHome.appending(path: "auth.json"))
    }

    let suiteName = "CodexRuntimeTests.\(UUID().uuidString)"
    defaults = UserDefaults(suiteName: suiteName)!
    defaults.removePersistentDomain(forName: suiteName)
    locator = CodexRuntimeLocator(
      defaults: defaults,
      environment: ["UNRELATED": "preserved"],
      runtimeURLOverride: executable,
      systemCodexHomeURL: systemCodexHome
    )
  }

  func remove() {
    try? FileManager.default.removeItem(at: root)
  }
}
