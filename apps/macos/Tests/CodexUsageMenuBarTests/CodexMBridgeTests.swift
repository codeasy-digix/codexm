import Foundation
import Testing
@testable import CodexUsageMenuBar

struct CodexMBridgeTests {
  @Test("An explicit missing backend never falls through to the installed product")
  func explicitMissing() {
    #expect(throws: (any Error).self) {
      try CodexMLocator(environment: ["CODEXM_BINARY": "/nonexistent/codexm"]).locate()
    }
  }

  @Test("Only official HTTPS sign-in destinations can be opened", arguments: [
    "http://auth.openai.com/login", "https://auth.openai.com.evil.invalid/", "file:///etc/passwd",
    "https://owner@auth.openai.com/login", "https://chatgpt.com:1234/login", "javascript:alert(1)"])
  func rejectsLoginDestinations(_ url: String) {
    #expect(throws: (any Error).self) { try CodexLoginChallenge.loginURL(url) }
  }

  @Test("Terminal command quoting treats user paths as data")
  func terminalQuoting() throws {
    let input = "a'\";$(touch SHOULD_NOT_EXIST) `id`\nnext"
    let process = Process(), pipe = Pipe()
    process.executableURL = URL(fileURLWithPath: "/bin/sh")
    process.arguments = ["-c", "printf %s " + CodexMTerminal.shellQuote(input)]
    process.standardOutput = pipe
    try process.run()
    let output = pipe.fileHandleForReading.readDataToEndOfFile(); process.waitUntilExit()
    #expect(process.terminationStatus == 0)
    #expect(String(data: output, encoding: .utf8) == input)
    #expect(CodexMTerminal.appleScriptQuote("a\"\\\nb") == "\"a\\\"\\\\\\nb\"")
  }

  @Test("The real shared bridge preserves its connection after a rejected mutation",
    .enabled(if: ProcessInfo.processInfo.environment["CODEXM_TEST_BINARY"] != nil))
  func realBridge() async throws {
    let root = FileManager.default.temporaryDirectory.appending(path: "CodexMBridgeTests-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }
    let config = root.appending(path: "config.toml")
    let native = root.appending(path: "native")
    try FileManager.default.createDirectory(at: native, withIntermediateDirectories: true)
    let contents = """
    [runtime]
    shared_codex_home = '\(native.path)'
    accounts_dir = '\(root.appending(path: "accounts").path)'
    [storage]
    state_dir = '\(root.appending(path: "state").path)'
    """
    try contents.write(to: config, atomically: true, encoding: .utf8)
    var env = ProcessInfo.processInfo.environment
    env["CODEXM_BINARY"] = env["CODEXM_TEST_BINARY"]
    env["CODEXM_CONFIG"] = config.path
    env["CODEX_HOME"] = "/never/read/inherited/account"
    let backend = CodexMBridge(locator: .init(environment: env))
    defer { backend.shutdown() }
    let catalog: [DesktopAccount] = try await backend.call("accounts.catalog")
    #expect(catalog.count == 1)
    #expect(catalog.first?.name == "default")
    do {
      let _: Bool = try await backend.call("accounts.presentation", ["order": .strings(["missing"])])
      Issue.record("Invalid order accepted")
    } catch CodexMBridgeError.rejected { }
    let after: [DesktopAccount] = try await backend.call("accounts.catalog")
    #expect(after.first?.revision == catalog.first?.revision)
    backend.shutdown()
    do { let _: [DesktopAccount] = try await backend.call("accounts.catalog"); Issue.record("Restarted stopped bridge") }
    catch CodexMBridgeError.disconnected { }
  }
}
