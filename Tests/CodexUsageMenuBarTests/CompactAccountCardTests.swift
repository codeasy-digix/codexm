import AppKit
import SwiftUI
import Testing

@testable import CodexUsageMenuBar

@Suite("Compact connection cards")
@MainActor
struct CompactAccountCardTests {
  @Test("Keeps all credit summary fields on one line")
  func summarizesCredits() throws {
    let now = Date(timeIntervalSince1970: 1_790_000_000)
    let expiry = now.addingTimeInterval(172_800)
    let snapshot = snapshot(credits: 3, details: [credit(expiry), credit(expiry.addingTimeInterval(3600))])
    let summary = try #require(CompactResetCreditSummary(snapshot: snapshot, relativeTo: now))
    #expect(summary.text.contains(L10n.text("credit.count", 3)))
    #expect(summary.text.contains(L10n.text("credit.confirmed")))
    #expect(summary.text.contains(CompactUsageDate.string(expiry, relativeTo: now)))
    #expect(summary.text.contains(L10n.text("credit.after", L10n.text("duration.days", 2))))
    #expect(summary.text.contains(L10n.text("credit.coverage", 2, 3)))
    #expect(!summary.text.contains("\n"))
    #expect(summary.detail.contains(L10n.text("credit.confirmed_detail", L10n.date(expiry, style: .complete))))

    let complete = try #require(CompactResetCreditSummary(
      snapshot: self.snapshot(credits: 1, details: [credit(expiry)]), relativeTo: now))
    #expect(complete.text.contains(L10n.text("credit.earliest")))
    #expect(!complete.text.contains(L10n.text("credit.coverage", 1, 1)))
  }

  @Test("Distinguishes missing expiration details and zero credits")
  func summarizesUnavailableExpiration() throws {
    let missing = try #require(CompactResetCreditSummary(snapshot: snapshot(credits: 2, details: nil)))
    #expect(missing.text == L10n.text("credit.count", 2) + " · " + L10n.text("credit.expiration_missing"))
    let zero = try #require(CompactResetCreditSummary(snapshot: snapshot(credits: 0, details: [])))
    #expect(zero.text == L10n.text("credit.count", 0))
  }

  @Test("Keeps the default account as the menu-bar source after reordering")
  func reordersStoreWithoutChangingDefaultSource() throws {
    let root = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let registry = UsageAccountRegistry(applicationSupportURL: root)
    var managed = try registry.beginManagedAccount()
    managed.workspaceName = "개발팀"
    try registry.commitPendingAccount(managed)
    let store = UsageStore(registry: registry)
    let oldStates = store.accountStates
    let primary = store.primaryAccountState
    #expect(store.reorderAccounts(accountIDs: [managed.id, UsageAccount.systemDefaultID]))
    #expect(store.accountStates.map(\.id) == [managed.id, UsageAccount.systemDefaultID])
    #expect(store.accountStates == Array(oldStates.reversed()))
    #expect(store.primaryAccountState == primary)
    #expect(!store.reorderAccounts(accountIDs: [managed.id, managed.id]))
    #expect(try registry.loadAccounts().map(\.id) == store.accountStates.map(\.id))
  }

  @Test("Renders a full credit card at compact height")
  func rendersCompactCard() throws {
    let root = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let store = UsageStore(registry: UsageAccountRegistry(applicationSupportURL: root))
    let now = Date()
    var account = UsageAccount.systemDefault
    account.workspaceName = "개발팀"
    account.lastKnownWorkspaceFingerprint = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
    let viewState = UsageStore.AccountViewState(
      account: account,
      state: .loaded(snapshot(credits: 3, details: [credit(now.addingTimeInterval(172_800))])),
      isRefreshing: false
    )
    let content = AccountUsageCard(viewState: viewState, store: store,
      preferences: AppPreferences(), onRequestDelete: {})
      .frame(width: 392)
      .padding(14)
      .background(Color(nsColor: .windowBackgroundColor))
      .environment(\.locale, Locale(identifier: "ko_KR"))
      .environment(\.colorScheme, .light)
    let renderer = ImageRenderer(content: content)
    renderer.scale = 2
    let image = try #require(renderer.nsImage)
    #expect(image.size.width == 420)
    #expect(image.size.height <= 150)
    if let output = ProcessInfo.processInfo.environment["CODEX_ACCOUNT_CARD_PREVIEW_PATH"],
      let data = image.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: data),
      let png = bitmap.representation(using: .png, properties: [:])
    {
      try png.write(to: URL(fileURLWithPath: output))
    }
  }

  private func snapshot(credits: Int64, details: [ResetCreditDetail]?) -> UsageSnapshot {
    let now = Date()
    return UsageSnapshot(
      fiveHourLimit: UsageLimitWindow(usedPercent: 10, windowDurationMinutes: 300, resetsAt: now.addingTimeInterval(15_000)),
      weeklyLimit: UsageLimitWindow(usedPercent: 36, windowDurationMinutes: 10_080, resetsAt: now.addingTimeInterval(530_000)),
      planType: "team", availableResetCredits: credits, resetCreditDetails: details,
      accountEmail: "owner@example.com", fetchedAt: now
    )
  }

  private func credit(_ expiry: Date) -> ResetCreditDetail {
    ResetCreditDetail(id: nil, status: "available", grantedAt: nil, expiresAt: expiry, title: nil, description: nil)
  }
}
