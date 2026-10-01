import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Panel refresh flow")
@MainActor
struct UsageStoreRefreshTests {
  @Test("Opening during an individual read coalesces a follow-up for every connection")
  func queuesAllConnections() async throws {
    let fixture = try MockAppServerFixture(slowRateLimitResponse: true)
    let suite = "UsageStoreRefreshTests-\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: suite))
    defer {
      defaults.removePersistentDomain(forName: suite)
      fixture.remove()
    }
    let registry = UsageAccountRegistry(applicationSupportURL: fixture.rootURL.appending(path: "Registry"))
    let managed = try registry.beginManagedAccount()
    try registry.commitPendingAccount(managed)
    let home = fixture.rootURL.appending(path: "DefaultHome")
    try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
    try Data(#"{"tokens":{"account_id":"test-workspace"}}"#.utf8).write(to: home.appending(path: "auth.json"))
    let client = CodexAppServerClient(timeout: .seconds(3), environment: fixture.environment)
    defer { client.shutdown() }
    let store = UsageStore(registry: registry,
      runtimeLocator: CodexRuntimeLocator(defaults: defaults, environment: fixture.environment,
        runtimeURLOverride: fixture.executableURL, applicationSupportURL: fixture.rootURL,
        systemCodexHomeURL: home), client: client,
      automaticScheduleStore: AutomaticUsageScheduleStore(defaults: defaults),
      refreshSchedule: UsageRefreshSchedule(defaults: defaults))
    #expect(store.canRequestManualRefresh)
    store.refresh(accountID: UsageAccount.systemDefaultID)
    #expect(!store.canRequestManualRefresh)
    store.refreshAll()
    store.refreshAll()
    store.refreshAll()
    let clock = ContinuousClock()
    let deadline = clock.now.advanced(by: .seconds(4))
    while !store.canRequestManualRefresh, clock.now < deadline {
      try await Task.sleep(for: .milliseconds(10))
    }
    #expect(store.canRequestManualRefresh)
    #expect(try fixture.rateLimitReadCount() == 3) // individual + one all-account pass
    #expect(store.accountStates.allSatisfy {
      if case .loaded = $0.state { return true }
      return false
    })
    store.refreshAll() // the five-second debounce prevents a duplicate pass
    try await Task.sleep(for: .milliseconds(30))
    #expect(try fixture.rateLimitReadCount() == 3)
  }
}
