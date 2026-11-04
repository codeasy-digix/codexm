import Foundation
import Testing
@testable import CodexUsageMenuBar

@Suite("Shared desktop account backend")
@MainActor
struct UsageStoreRefreshTests {
  @Test("Names follow the login identity across default switches and app restarts")
  func defaultNames() async throws {
    let f = try DesktopStoreFixture(); defer { f.remove() }
    let store = f.store(); defer { store.shutdown() }
    try await store.reloadCatalog()
    #expect(store.renameWorkspace(accountID: "system-default", workspaceName: " Personal "))
    try await waitForDesktop { !store.isManagingAccounts }
    let restarted = f.store(); defer { restarted.shutdown() }
    try await restarted.reloadCatalog()
    #expect(restarted.accountStates[0].account.workspaceName == "Personal")
    await f.backend.selectDefault("bob")
    try await restarted.reloadCatalog()
    #expect(restarted.accountStates[0].account.workspaceName == "")
    await f.backend.selectDefault("alice")
    try await restarted.reloadCatalog()
    #expect(restarted.accountStates[0].account.workspaceName == "Personal")
  }

  @Test("A stale edit is rejected, and an old usage response never labels a new login")
  func rejectsStaleState() async throws {
    let f = try DesktopStoreFixture(); defer { f.remove() }
    let store = f.store(); defer { store.shutdown() }
    try await store.reloadCatalog()
    await f.backend.selectDefault("carol")
    #expect(store.renameWorkspace(accountID: "system-default", workspaceName: "Wrong"))
    try await waitForDesktop { !store.isManagingAccounts }
    #expect(store.accountManagementError != nil)
    #expect(store.accountStates[0].account.workspaceName == "")
    await f.backend.delayUsage()
    store.refresh(accountID: "system-default")
    try await waitForDesktop { await f.backend.usageReads == 1 }
    await f.backend.selectDefault("dave")
    try await waitForDesktop { store.canRequestManualRefresh }
    #expect(store.accountStates[0].account.lastKnownEmail == "dave@example.invalid")
    #expect(store.accountStates[0].state == .loading)
  }

  @Test("A rename during a usage read survives, and transient failure keeps only that login's last good usage")
  func namesAndLastGood() async throws {
    let f = try DesktopStoreFixture(); defer { f.remove() }
    let store = f.store(); defer { store.shutdown() }
    try await store.reloadCatalog(); await f.backend.delayUsage()
    store.refresh(accountID: "system-default")
    try await waitForDesktop { await f.backend.usageReads == 1 }
    #expect(store.renameWorkspace(accountID: "system-default", workspaceName: "Latest"))
    try await waitForDesktop { store.canRequestManualRefresh && !store.isManagingAccounts }
    #expect(store.accountStates[0].account.workspaceName == "Latest")
    guard case .loaded = store.accountStates[0].state else { Issue.record("Expected usage"); return }
    await f.backend.rejectUsage()
    store.refresh(accountID: "system-default")
    try await waitForDesktop { store.canRequestManualRefresh }
    guard case .loaded = store.accountStates[0].state else { Issue.record("Lost last good usage"); return }
    #expect(store.accountStates[0].lastRefreshError != nil)
    await f.backend.selectDefault("carol")
    try await store.reloadCatalog()
    #expect(store.accountStates[0].state == .loading)
    #expect(store.accountStates[0].lastRefreshError == nil)
  }

  @Test("Refresh-all coalesces behind an individual read and ignores rapid duplicate clicks")
  func coalescesRefresh() async throws {
    let f = try DesktopStoreFixture(); defer { f.remove() }
    let store = f.store(); defer { store.shutdown() }
    await f.backend.delayUsage()
    store.refresh(accountID: "system-default")
    try await waitForDesktop { await f.backend.usageReads == 1 }
    store.refreshAll(); store.refreshAll()
    try await waitForDesktop { store.canRequestManualRefresh }
    #expect(await f.backend.usageReads == 3)
    store.refreshAll()
    #expect(store.canRequestManualRefresh)
    #expect(await f.backend.usageReads == 3)
  }

  @Test("Default changes and removal go through shared backend with the viewed revision")
  func accountManagement() async throws {
    let f = try DesktopStoreFixture(); defer { f.remove() }
    let store = f.store(); defer { store.shutdown() }
    try await store.reloadCatalog()
    store.setNativeDefault(accountID: "work")
    try await waitForDesktop { !store.isManagingAccounts }
    #expect(store.accountStates[0].account.lastKnownEmail == "bob@example.invalid")
    store.removeManagedAccount(accountID: "work")
    try await waitForDesktop { !store.isManagingAccounts }
    #expect(store.accountStates.count == 1)
    #expect(await f.backend.calls.contains("accounts.remove"))
  }

  @Test("Cancel before a login challenge arrives cancels exactly that login once")
  func earlyLoginCancellation() async throws {
    let f = try DesktopStoreFixture(); defer { f.remove() }
    let store = f.store(); defer { store.shutdown() }
    await f.backend.delayLogin()
    store.startAddingAccount(method: .browser, nickname: "new-login")
    try await waitForDesktop { await f.backend.calls.contains("login.start") }
    store.cancelCurrentAuthentication()
    try await waitForDesktop { !store.isAuthenticating }
    #expect(await f.backend.canceledLogins == 1)
    #expect(store.loginChallenge == nil)
    #expect(store.authenticationError == nil)
    #expect(await f.backend.calls.filter { $0 == "login.start" }.count == 1)
  }
}
