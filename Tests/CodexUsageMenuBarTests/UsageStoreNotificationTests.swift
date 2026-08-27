import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Transient account notifications")
struct UsageStoreNotificationTests {
  @Test("Automatically dismisses a successful connection notice")
  @MainActor
  func automaticallyDismissesNotice() async throws {
    let fixture = try NotificationFixture(managedAccountCount: 1)
    defer { fixture.remove() }
    let store = UsageStore(
      registry: fixture.registry,
      noticeDismissDelay: .milliseconds(40),
      errorDismissDelay: .milliseconds(80)
    )
    let accountID = try #require(store.accountStates.first(where: { $0.account.isManaged })?.id)

    store.removeManagedAccount(accountID: accountID)

    #expect(store.accountManagementNotice != nil)
    try await Task.sleep(for: .milliseconds(120))
    #expect(store.accountManagementNotice == nil)
  }

  @Test("A newer notice gets its own full display time")
  @MainActor
  func replacesNoticeDismissTimer() async throws {
    let fixture = try NotificationFixture(managedAccountCount: 2)
    defer { fixture.remove() }
    let store = UsageStore(
      registry: fixture.registry,
      noticeDismissDelay: .milliseconds(120),
      errorDismissDelay: .milliseconds(200)
    )
    let accountIDs = store.accountStates.filter { $0.account.isManaged }.map(\.id)
    #expect(accountIDs.count == 2)

    store.removeManagedAccount(accountID: accountIDs[0])
    try await Task.sleep(for: .milliseconds(70))
    store.removeManagedAccount(accountID: accountIDs[1])
    let newerNotice = try #require(store.accountManagementNotice)

    try await Task.sleep(for: .milliseconds(70))
    #expect(store.accountManagementNotice == newerNotice)
    store.clearAccountManagementNotice()
    #expect(store.accountManagementNotice == nil)
  }

  @Test("Automatically dismisses an account-management error")
  @MainActor
  func automaticallyDismissesError() async throws {
    let fixture = try NotificationFixture(managedAccountCount: 1)
    defer { fixture.remove() }
    let store = UsageStore(
      registry: fixture.registry,
      noticeDismissDelay: .milliseconds(40),
      errorDismissDelay: .milliseconds(40)
    )
    let accountID = try #require(store.accountStates.first(where: { $0.account.isManaged })?.id)
    try Data("invalid registry".utf8).write(to: fixture.registry.registryURL)

    store.renameWorkspace(accountID: accountID, workspaceName: "개발팀")

    #expect(store.accountManagementError != nil)
    try await Task.sleep(for: .milliseconds(120))
    #expect(store.accountManagementError == nil)
  }
}

private struct NotificationFixture {
  let root: URL
  let registry: UsageAccountRegistry

  init(managedAccountCount: Int) throws {
    root = FileManager.default.temporaryDirectory.appending(
      path: "UsageStoreNotificationTests-\(UUID().uuidString)",
      directoryHint: .isDirectory
    )
    registry = UsageAccountRegistry(applicationSupportURL: root)

    for index in 0..<managedAccountCount {
      var account = try registry.beginManagedAccount()
      account.displayName = "테스트 연결 \(index + 1)"
      try registry.commitPendingAccount(account)
    }
  }

  func remove() {
    try? FileManager.default.removeItem(at: root)
  }
}
