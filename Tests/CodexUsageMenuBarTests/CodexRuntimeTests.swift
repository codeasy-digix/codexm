import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Bundled Codex runtime")
struct CodexRuntimeTests {
  @Test("Reuses an existing Codex login without asking the user to sign in again")
  func reusesExistingLogin() throws {
    let fixture = try Fixture(authenticated: true)
    defer { fixture.remove() }

    let runtime = try fixture.locator.locate()

    #expect(runtime.executableURL == fixture.executable)
    #expect(runtime.codexHomeURL == fixture.systemCodexHome)
    #expect(runtime.environment["CODEX_HOME"] == fixture.systemCodexHome.path)
    #expect(runtime.environment["PATH"] == "/usr/bin:/bin:/usr/sbin:/sbin")
  }

  @Test("Uses an app-owned Codex home only when no existing login is available")
  func usesAppOwnedHomeWithoutLogin() throws {
    let fixture = try Fixture(authenticated: false)
    defer { fixture.remove() }

    let runtime = try fixture.locator.locate()
    let expected = fixture.applicationSupport.appending(
      path: "CodexHome",
      directoryHint: .isDirectory
    )

    #expect(runtime.codexHomeURL == expected)
    #expect(FileManager.default.fileExists(atPath: expected.path))
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
      applicationSupportURL: applicationSupport,
      systemCodexHomeURL: systemCodexHome
    )
  }

  func remove() {
    try? FileManager.default.removeItem(at: root)
  }
}
