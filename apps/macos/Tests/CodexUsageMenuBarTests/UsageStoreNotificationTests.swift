import Foundation
import Testing
@testable import CodexUsageMenuBar

@Suite("Transient account notifications")
@MainActor
struct UsageStoreNotificationTests {
  @Test("Success and error notifications dismiss independently")
  func dismissesNotifications() async throws {
    let f = try DesktopStoreFixture(); defer { f.remove() }
    let store = f.store(notice: .milliseconds(120), error: .milliseconds(120)); defer { store.shutdown() }
    try await store.reloadCatalog()
    store.setNativeDefault(accountID: "work")
    try await waitForDesktop { !store.isManagingAccounts }
    #expect(store.accountManagementNotice != nil)
    try await waitForDesktop { store.accountManagementNotice == nil }
    await f.backend.rejectMutations()
    #expect(store.renameWorkspace(accountID: "work", workspaceName: "Team"))
    try await waitForDesktop { !store.isManagingAccounts }
    #expect(store.accountManagementError != nil)
    try await waitForDesktop { store.accountManagementError == nil }
  }

  @Test("A newer notice gets a fresh display interval")
  func newerNotice() async throws {
    let f = try DesktopStoreFixture(); defer { f.remove() }
    let store = f.store(notice: .milliseconds(400)); defer { store.shutdown() }
    try await store.reloadCatalog()
    store.setNativeDefault(accountID: "work")
    try await waitForDesktop { !store.isManagingAccounts }
    try await Task.sleep(for: .milliseconds(250))
    store.setNativeDefault(accountID: "work")
    try await waitForDesktop { !store.isManagingAccounts }
    try await Task.sleep(for: .milliseconds(200))
    #expect(store.accountManagementNotice != nil)
    store.clearAccountManagementNotice()
    #expect(store.accountManagementNotice == nil)
  }
}
