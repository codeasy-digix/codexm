import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Panel refresh flow")
@MainActor
struct UsageStoreRefreshTests {
  @Test("Default workspace names survive reads and restart and follow the live login")
  func defaultWorkspaceNamesFollowLogin() async throws {
    let setup = try WorkspaceTestSetup()
    defer { setup.remove() }
    let store = setup.makeStore()
    try await readDefault(store)
    #expect(store.renameWorkspace(accountID: UsageAccount.systemDefaultID, workspaceName: " Personal "))
    #expect(store.accountStates[0].account.workspaceName == "Personal")
    try await readDefault(store)
    #expect(store.accountStates[0].account.workspaceName == "Personal")

    let restarted = setup.makeStore()
    #expect(restarted.accountStates[0].account.workspaceName == nil)
    try await readDefault(restarted)
    #expect(restarted.accountStates[0].account.workspaceName == "Personal")

    try setup.setWorkspace("work")
    try await readDefault(restarted)
    #expect(restarted.accountStates[0].account.workspaceName == nil)
    #expect(restarted.renameWorkspace(accountID: UsageAccount.systemDefaultID, workspaceName: "Work"))
    try setup.setWorkspace("personal")
    try await readDefault(restarted)
    #expect(restarted.accountStates[0].account.workspaceName == "Personal")
    #expect(restarted.renameWorkspace(accountID: UsageAccount.systemDefaultID, workspaceName: ""))
    try await readDefault(restarted)
    #expect(restarted.accountStates[0].account.workspaceName == nil)
    try setup.setWorkspace("work")
    try await readDefault(restarted)
    #expect(restarted.accountStates[0].account.workspaceName == "Work")
  }

  @Test("A default rename cannot be saved against a login that changed while editing")
  func rejectsStaleDefaultRename() async throws {
    let setup = try WorkspaceTestSetup()
    defer { setup.remove() }
    var managed = try setup.registry.beginManagedAccount()
    managed.workspaceName = "Additional"
    try setup.registry.commitPendingAccount(managed)
    let store = setup.makeStore()
    try await readDefault(store)
    #expect(store.renameWorkspace(accountID: UsageAccount.systemDefaultID, workspaceName: "Personal"))
    try setup.setWorkspace("work")
    #expect(!store.renameWorkspace(accountID: UsageAccount.systemDefaultID, workspaceName: "Wrong workspace"))
    #expect(store.accountStates[0].account.workspaceName == nil)
    #expect(store.accountManagementError != nil)
    #expect(store.accountStates[1].account.workspaceName == "Additional")
    try await readDefault(store)
    #expect(store.accountStates[0].account.workspaceName == nil)
    #expect(store.renameWorkspace(accountID: UsageAccount.systemDefaultID, workspaceName: "Work"))
    try setup.setWorkspace("personal")
    try await readDefault(store)
    #expect(store.accountStates[0].account.workspaceName == "Personal")
  }

  @Test("A rename made during a status read is not overwritten by its completion")
  func preservesRenameDuringRead() async throws {
    let setup = try WorkspaceTestSetup(slowResponse: true)
    defer { setup.remove() }
    let store = setup.makeStore()
    try await readDefault(store)
    store.refresh(accountID: UsageAccount.systemDefaultID)
    try await waitForReads(2, setup: setup)
    #expect(store.renameWorkspace(accountID: UsageAccount.systemDefaultID, workspaceName: "Latest name"))
    try await waitForIdle(store)
    #expect(store.accountStates[0].account.workspaceName == "Latest name")
  }

  @Test("An old login's response and notification cannot label a newly selected workspace")
  func discardsStaleDefaultRead() async throws {
    let setup = try WorkspaceTestSetup(slowResponse: true, sendNotification: true)
    defer { setup.remove() }
    let store = setup.makeStore()
    try await readDefault(store)
    #expect(store.renameWorkspace(accountID: UsageAccount.systemDefaultID, workspaceName: "Personal"))
    store.refresh(accountID: UsageAccount.systemDefaultID)
    try await waitForReads(2, setup: setup)
    try setup.setWorkspace("work")
    try await waitForIdle(store)
    #expect(store.accountStates[0].account.workspaceName == nil)
    #expect(store.accountStates[0].state == .loading)
    try await readDefault(store)
    #expect(store.accountStates[0].account.workspaceName == nil)
    #expect(store.renameWorkspace(accountID: UsageAccount.systemDefaultID, workspaceName: "Work"))
    try setup.setWorkspace("personal")
    try await readDefault(store)
    #expect(store.accountStates[0].account.workspaceName == "Personal")
  }

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

  private func readDefault(_ store: UsageStore) async throws {
    store.refresh(accountID: UsageAccount.systemDefaultID)
    try await waitForIdle(store)
    guard case .loaded = store.accountStates.first?.state else {
      Issue.record("Expected the current default login to load")
      return
    }
  }

  private func waitForIdle(_ store: UsageStore) async throws {
    let clock = ContinuousClock()
    let deadline = clock.now.advanced(by: .seconds(4))
    while !store.canRequestManualRefresh, clock.now < deadline {
      try await Task.sleep(for: .milliseconds(10))
    }
    #expect(store.canRequestManualRefresh)
  }

  private func waitForReads(_ count: Int, setup: WorkspaceTestSetup) async throws {
    let clock = ContinuousClock()
    let deadline = clock.now.advanced(by: .seconds(4))
    while ((try? setup.fixture.rateLimitReadCount()) ?? 0) < count, clock.now < deadline {
      try await Task.sleep(for: .milliseconds(5))
    }
    #expect(try setup.fixture.rateLimitReadCount() >= count)
  }
}

@MainActor
private struct WorkspaceTestSetup {
  let fixture: MockAppServerFixture
  let registry: UsageAccountRegistry
  let defaults: UserDefaults
  let suite: String
  let home: URL
  let client: CodexAppServerClient

  init(slowResponse: Bool = false, sendNotification: Bool = false) throws {
    fixture = try MockAppServerFixture(slowRateLimitResponse: slowResponse,
      sendNotification: sendNotification)
    suite = "DefaultWorkspaceTests-\(UUID().uuidString)"
    defaults = try #require(UserDefaults(suiteName: suite))
    registry = UsageAccountRegistry(applicationSupportURL: fixture.rootURL.appending(path: "Registry"))
    home = fixture.rootURL.appending(path: "DefaultHome")
    client = CodexAppServerClient(timeout: .seconds(3), environment: fixture.environment)
    try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
    try setWorkspace("personal")
  }

  func setWorkspace(_ identifier: String) throws {
    let data = try JSONSerialization.data(withJSONObject: ["tokens": ["account_id": identifier]])
    try data.write(to: home.appending(path: "auth.json"), options: .atomic)
  }

  func makeStore() -> UsageStore {
    UsageStore(registry: registry,
      runtimeLocator: CodexRuntimeLocator(defaults: defaults, environment: fixture.environment,
        runtimeURLOverride: fixture.executableURL, applicationSupportURL: fixture.rootURL,
        systemCodexHomeURL: home), client: client,
      automaticScheduleStore: AutomaticUsageScheduleStore(defaults: defaults),
      refreshSchedule: UsageRefreshSchedule(defaults: defaults))
  }

  func remove() {
    client.shutdown()
    defaults.removePersistentDomain(forName: suite)
    fixture.remove()
  }
}
