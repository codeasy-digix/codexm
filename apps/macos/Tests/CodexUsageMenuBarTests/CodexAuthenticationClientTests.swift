import AppKit
import Foundation
import SwiftUI
import Testing

@testable import CodexUsageMenuBar

@Suite("Browser and device sign-in", .serialized)
struct CodexAuthenticationClientTests {
  @Test("Parses separate browser and device challenges without inventing a browser code")
  func parsesChallenges() throws {
    let browser = try CodexLoginChallenge.parse([
      "type": "chatgpt", "loginId": "browser-id", "authUrl": "https://auth.openai.com/oauth/authorize"
    ], method: .browser)
    #expect(browser == .browser(BrowserLoginInfo(loginId: "browser-id",
      authorizationURL: URL(string: "https://auth.openai.com/oauth/authorize")!)))
    let device = try CodexLoginChallenge.parse([
      "type": "chatgptDeviceCode", "loginId": "device-id",
      "verificationUrl": "https://auth.openai.com/codex/device", "userCode": "TEST-1234"
    ], method: .deviceCode)
    #expect(device == .deviceCode(DeviceLoginInfo(loginId: "device-id",
      verificationURL: URL(string: "https://auth.openai.com/codex/device")!, userCode: "TEST-1234")))
    #expect(throws: CodexUsageError.invalidResponse) {
      try CodexLoginChallenge.parse(["type": "chatgptDeviceCode", "loginId": "id"], method: .browser)
    }
  }

  @Test("Rejects unsafe authorization URLs", arguments: ["file:///tmp/login", "javascript:alert(1)",
    "http://auth.openai.com/login", "https://auth.openai.com.attacker.test/login",
    "https://user:password@auth.openai.com/login", "https://chatgpt.com:9999/login"])
  func rejectsUnsafeURLs(_ value: String) {
    #expect(throws: CodexUsageError.invalidResponse) {
      try CodexLoginChallenge.parse(["type": "chatgpt", "loginId": "id", "authUrl": value], method: .browser)
    }
  }

  @Test("Requests the selected official flow and keeps its dedicated Codex home", arguments: CodexLoginMethod.allCases)
  func selectedFlow(_ method: CodexLoginMethod) async throws {
    let fixture = try MockLoginFixture()
    defer { fixture.remove() }
    let challenges = LoginChallengeCollector()
    try await CodexAuthenticationClient(timeout: .seconds(3)).login(runtime: fixture.runtime, method: method) {
      challenges.append($0)
    }
    #expect(challenges.values.count == 1)
    #expect(challenges.values.first?.loginId == "test-login")
    #expect(try fixture.read("request-type") == method.requestType)
    #expect(try fixture.read("codex-home") == fixture.runtime.codexHomeURL.path)
    switch (method, challenges.values.first) {
    case (.browser, .browser), (.deviceCode, .deviceCode): break
    default: Issue.record("Wrong challenge for selected sign-in method")
    }
    await fixture.waitForExit()
    #expect(fixture.didStop)
  }

  @Test("Ignores completion notifications for a different login")
  func matchesLoginID() async throws {
    let fixture = try MockLoginFixture(scenario: "wrong-id")
    defer { fixture.remove() }
    try await CodexAuthenticationClient(timeout: .seconds(3)).login(runtime: fixture.runtime, method: .browser) { _ in }
    await fixture.waitForExit()
    #expect(fixture.didStop)
  }

  @Test("Returns login failure without adding an account")
  func loginFailure() async throws {
    let fixture = try MockLoginFixture(scenario: "failure")
    defer { fixture.remove() }
    await #expect(throws: CodexUsageError.notAuthenticated("Denied")) {
      try await CodexAuthenticationClient(timeout: .seconds(3)).login(runtime: fixture.runtime, method: .browser) { _ in }
    }
    await fixture.waitForExit()
    #expect(fixture.didStop)
  }

  @Test("Cancels a waiting browser flow and terminates its callback owner")
  func cancelsWaitingLogin() async throws {
    let fixture = try MockLoginFixture(scenario: "wait")
    defer { fixture.remove() }
    let challenges = LoginChallengeCollector()
    let task = Task {
      try await CodexAuthenticationClient(timeout: .seconds(3)).login(runtime: fixture.runtime, method: .browser) {
        challenges.append($0)
      }
    }
    let deadline = ContinuousClock.now.advanced(by: .seconds(2))
    while challenges.values.isEmpty, ContinuousClock.now < deadline {
      try await Task.sleep(for: .milliseconds(10))
    }
    #expect(challenges.values.count == 1)
    task.cancel()
    await #expect(throws: CancellationError.self) { try await task.value }
    await fixture.waitForExit()
    #expect(fixture.didStop)
  }

  @Test("An already-cancelled task cannot launch or leave a waiting login process")
  func cancellationBeforeStart() async throws {
    let fixture = try MockLoginFixture(scenario: "wait")
    defer { fixture.remove() }
    let task = Task {
      withUnsafeCurrentTask { $0?.cancel() }
      try await CodexAuthenticationClient(timeout: .seconds(3)).login(runtime: fixture.runtime, method: .browser) { _ in }
    }
    await #expect(throws: CancellationError.self) { try await task.value }
    #expect(!FileManager.default.fileExists(atPath: fixture.root.appending(path: "request-type").path))
  }

  @Test("Times out and terminates an abandoned browser login")
  func timesOut() async throws {
    let fixture = try MockLoginFixture(scenario: "wait")
    defer { fixture.remove() }
    let challenges = LoginChallengeCollector()
    await #expect(throws: CodexUsageError.timedOut) {
      try await CodexAuthenticationClient(timeout: .seconds(1)).login(runtime: fixture.runtime, method: .browser) {
        challenges.append($0)
      }
    }
    #expect(challenges.values.count == 1)
    await fixture.waitForExit()
    #expect(fixture.didStop)
  }

  @Test("Starts and cancels the native browser flow without opening a browser or using existing credentials")
  func nativeBrowserChallenge() async throws {
    guard let runtimePath = ProcessInfo.processInfo.environment["CODEX_NATIVE_AUTH_SMOKE_RUNTIME"] else { return }
    let home = FileManager.default.temporaryDirectory.appending(path: "CodexNativeLoginSmoke-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: home) }
    let challenges = LoginChallengeCollector()
    let runtime = CodexRuntime(executableURL: URL(fileURLWithPath: runtimePath),
      environment: ["PATH": "/usr/bin:/bin", "CODEX_HOME": home.path], codexHomeURL: home)
    let task = Task {
      try await CodexAuthenticationClient(timeout: .seconds(10)).login(runtime: runtime, method: .browser) {
        challenges.append($0)
      }
    }
    let deadline = ContinuousClock.now.advanced(by: .seconds(8))
    while challenges.values.isEmpty, ContinuousClock.now < deadline {
      try await Task.sleep(for: .milliseconds(10))
    }
    // Never print the OAuth URL or its state/code challenge in test output.
    let hasBrowserChallenge: Bool
    if case .browser = challenges.values.first { hasBrowserChallenge = true } else { hasBrowserChallenge = false }
    #expect(hasBrowserChallenge)
    task.cancel()
    do {
      try await task.value
      Issue.record("A cancelled native login unexpectedly succeeded")
    } catch is CancellationError {
      // Expected: no browser was opened and no authorization was completed.
    } catch {
      Issue.record("Native sign-in failed before cancellation; private diagnostics omitted")
    }
    #expect(!FileManager.default.fileExists(atPath: home.appending(path: "auth.json").path))
  }
}

@Suite("Browser account-add flow")
@MainActor
struct BrowserAccountAddTests {
  @Test("Automatically registers a browser-authenticated workspace and rejects its duplicate without changing the default login")
  func addsAndRejectsDuplicate() async throws {
    let fixture = try MockLoginFixture()
    let suite = "BrowserAccountAddTests.\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: suite))
    let registry = UsageAccountRegistry(applicationSupportURL: fixture.root.appending(path: "Registry"))
    let defaultHome = fixture.root.appending(path: "DefaultHome")
    try FileManager.default.createDirectory(at: defaultHome, withIntermediateDirectories: true)
    let defaultAuth = Data(#"{"tokens":{"account_id":"unchanged-default"}}"#.utf8)
    try defaultAuth.write(to: defaultHome.appending(path: "auth.json"))
    let client = CodexAppServerClient(timeout: .seconds(3), environment: fixture.runtime.environment)
    var openedURLs: [URL] = []
    let store = UsageStore(registry: registry,
      runtimeLocator: CodexRuntimeLocator(defaults: defaults, environment: fixture.runtime.environment,
        runtimeURLOverride: fixture.runtime.executableURL, systemCodexHomeURL: defaultHome), client: client,
      authorizationPageOpener: { openedURLs.append($0); return true },
      automaticScheduleStore: AutomaticUsageScheduleStore(defaults: defaults),
      refreshSchedule: UsageRefreshSchedule(defaults: defaults))
    defer {
      store.cancelCurrentAuthentication()
      client.shutdown()
      defaults.removePersistentDomain(forName: suite)
      fixture.remove()
    }
    store.relogin(accountID: UsageAccount.systemDefaultID, method: .browser)
    #expect(!store.isAuthenticating)
    #expect(openedURLs.isEmpty)
    store.startAddingAccount(method: .browser)
    store.setPendingWorkspaceName("Development")
    try await waitForAuthentication(store)
    #expect(store.accountStates.count == 2)
    let added = try #require(store.accountStates.last?.account)
    #expect(added.isManaged)
    #expect(added.workspaceName == "Development")
    #expect(added.lastKnownEmail == "login-test@example.com")
    #expect(store.recentlyAddedAccountID == added.id)
    #expect(store.accountManagementNotice != nil)
    #expect(store.loginChallenge == nil)
    #expect(openedURLs.count == 1)
    #expect(FileManager.default.fileExists(atPath: try registry.managedCodexHomeURL(for: added).appending(path: "auth.json").path))
    #expect(try Data(contentsOf: defaultHome.appending(path: "auth.json")) == defaultAuth)

    store.startAddingAccount(method: .browser)
    try await waitForAuthentication(store)
    #expect(store.accountStates.count == 2)
    #expect(store.accountManagementError != nil)
    #expect(try FileManager.default.contentsOfDirectory(atPath: registry.stagingRootURL.path).isEmpty)
    #expect(try Data(contentsOf: defaultHome.appending(path: "auth.json")) == defaultAuth)
  }

  @Test("Keeps a failed browser launch cancellable and discards the pending connection")
  func canCancelAfterOpenFailure() async throws {
    let fixture = try MockLoginFixture(scenario: "wait")
    let suite = "BrowserAccountAddTests.cancel.\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: suite))
    let registry = UsageAccountRegistry(applicationSupportURL: fixture.root.appending(path: "Registry"))
    let store = UsageStore(registry: registry,
      runtimeLocator: CodexRuntimeLocator(defaults: defaults, environment: fixture.runtime.environment,
        runtimeURLOverride: fixture.runtime.executableURL,
        systemCodexHomeURL: fixture.root.appending(path: "NoDefault")), authorizationPageOpener: { _ in false },
      automaticScheduleStore: AutomaticUsageScheduleStore(defaults: defaults),
      refreshSchedule: UsageRefreshSchedule(defaults: defaults))
    defer {
      store.cancelCurrentAuthentication()
      defaults.removePersistentDomain(forName: suite)
      fixture.remove()
    }
    store.startAddingAccount(method: .browser)
    let deadline = ContinuousClock.now.advanced(by: .seconds(2))
    while store.loginChallenge == nil, ContinuousClock.now < deadline {
      try await Task.sleep(for: .milliseconds(10))
    }
    #expect(store.isAuthenticating)
    #expect(store.authenticationError == L10n.text("auth.browser_open_failed"))
    #expect(store.deviceLoginInfo == nil)
    #expect(store.authenticationInstruction == L10n.text("auth.browser_instruction_new"))
    for language in AppLanguage.allCases {
      try L10n.$languageOverride.withValue(language) {
        let view = MenuContentView(store: store, preferences: AppPreferences(defaults: defaults))
          .authenticationPanel.frame(width: 392).padding(14)
          .background(Color(nsColor: .windowBackgroundColor))
          .environment(\.locale, L10n.locale).environment(\.colorScheme, .light)
        let hosting = NSHostingView(rootView: view)
        hosting.frame = NSRect(origin: .zero, size: hosting.fittingSize)
        hosting.layoutSubtreeIfNeeded()
        let bitmap = try #require(hosting.bitmapImageRepForCachingDisplay(in: hosting.bounds))
        hosting.cacheDisplay(in: hosting.bounds, to: bitmap)
        #expect(hosting.bounds.width == 420)
        #expect(hosting.bounds.height <= 380)
        if let directory = ProcessInfo.processInfo.environment["CODEX_AUTH_LAYOUT_PREVIEW_DIR"] {
          try FileManager.default.createDirectory(atPath: directory, withIntermediateDirectories: true)
          let png = try #require(bitmap.representation(using: .png, properties: [:]))
          try png.write(to: URL(fileURLWithPath: directory).appending(path: "browser-\(language.rawValue).png"))
        }
      }
    }
    store.cancelCurrentAuthentication()
    await fixture.waitForExit()
    #expect(fixture.didStop)
    #expect(!store.isAuthenticating)
    #expect(store.loginChallenge == nil)
    #expect(store.authenticationMethod == nil)
    #expect(store.accountStates.count == 1)
    #expect(try FileManager.default.contentsOfDirectory(atPath: registry.stagingRootURL.path).isEmpty)
  }

  private func waitForAuthentication(_ store: UsageStore) async throws {
    let deadline = ContinuousClock.now.advanced(by: .seconds(4))
    while store.isAuthenticating, ContinuousClock.now < deadline {
      try await Task.sleep(for: .milliseconds(10))
    }
    #expect(!store.isAuthenticating)
    #expect(store.authenticationError == nil)
  }
}

private final class LoginChallengeCollector: @unchecked Sendable {
  private let lock = NSLock()
  private var stored: [CodexLoginChallenge] = []
  var values: [CodexLoginChallenge] { lock.withLock { stored } }
  func append(_ value: CodexLoginChallenge) { lock.withLock { stored.append(value) } }
}

private struct MockLoginFixture: Sendable {
  let root: URL
  let runtime: CodexRuntime
  var didStop: Bool { FileManager.default.fileExists(atPath: root.appending(path: "stopped").path) }

  init(scenario: String = "success") throws {
    root = FileManager.default.temporaryDirectory.appending(path: "CodexLoginTests-\(UUID().uuidString)")
    let home = root.appending(path: "CodexHome")
    try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
    let executable = root.appending(path: "mock-codex")
    try Data(Self.script.utf8).write(to: executable)
    try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: executable.path)
    runtime = CodexRuntime(executableURL: executable,
      environment: ["PATH": "/usr/bin:/bin", "CODEX_HOME": home.path,
        "MOCK_ROOT": root.path, "MOCK_SCENARIO": scenario], codexHomeURL: home)
  }
  func read(_ name: String) throws -> String { try String(contentsOf: root.appending(path: name), encoding: .utf8) }
  func remove() { try? FileManager.default.removeItem(at: root) }
  func waitForExit() async {
    let deadline = ContinuousClock.now.advanced(by: .seconds(2))
    while !didStop, ContinuousClock.now < deadline { try? await Task.sleep(for: .milliseconds(10)) }
  }

  private static let script = #"""
    #!/bin/sh
    trap 'printf stopped > "$MOCK_ROOT/stopped"; exit 0' TERM
    while IFS= read -r line; do
      line=$(printf '%s\n' "$line" | /usr/bin/sed 's#\\/#/#g')
      id=$(printf '%s\n' "$line" | /usr/bin/sed -nE 's/.*"id":([0-9]+).*/\1/p')
      case "$line" in
        *'"method":"initialize"'*)
          printf '{"id":%s,"result":{"userAgent":"mock"}}\n' "$id" ;;
        *'"method":"account/login/start"'*)
          printf '%s' "$CODEX_HOME" > "$MOCK_ROOT/codex-home"
          case "$line" in
            *chatgptDeviceCode*)
              printf chatgptDeviceCode > "$MOCK_ROOT/request-type"
              printf '{"id":%s,"result":{"type":"chatgptDeviceCode","loginId":"test-login","verificationUrl":"https://auth.openai.com/codex/device","userCode":"TEST-1234"}}\n' "$id" ;;
            *)
              printf chatgpt > "$MOCK_ROOT/request-type"
              printf '{"id":%s,"result":{"type":"chatgpt","loginId":"test-login","authUrl":"https://auth.openai.com/oauth/authorize"}}\n' "$id" ;;
          esac
          if [ "$MOCK_SCENARIO" = "wait" ]; then continue; fi
          if [ "$MOCK_SCENARIO" = "wrong-id" ]; then
            printf '{"method":"account/login/completed","params":{"loginId":"other-login","success":false,"error":"Unrelated"}}\n'
          fi
          if [ "$MOCK_SCENARIO" = "failure" ]; then
            printf '{"method":"account/login/completed","params":{"loginId":"test-login","success":false,"error":"Denied"}}\n'
          else
            printf '{"tokens":{"account_id":"synthetic-new-workspace"}}' > "$CODEX_HOME/auth.json"
            printf '{"method":"account/login/completed","params":{"loginId":"test-login","success":true}}\n'
          fi ;;
        *'"method":"account/read"'*)
          printf '{"id":%s,"result":{"account":{"type":"chatgpt","email":"login-test@example.com","planType":"plus"},"requiresOpenaiAuth":true}}\n' "$id" ;;
        *'"method":"account/rateLimits/read"'*)
          printf '{"id":%s,"result":{"rateLimits":{"primary":{"usedPercent":20,"windowDurationMins":300},"secondary":{"usedPercent":40,"windowDurationMins":10080}}}}\n' "$id" ;;
      esac
    done
    printf stopped > "$MOCK_ROOT/stopped"
    """#
}
