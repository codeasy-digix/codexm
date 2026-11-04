import AppKit
import Foundation
import ServiceManagement

@MainActor
final class UsageStore: ObservableObject {
  enum AccountLoadState: Equatable {
    case loading, loaded(UsageSnapshot), needsAuthentication, failed(CodexUsageError)
  }
  struct AccountViewState: Equatable, Identifiable {
    var account: UsageAccount
    var state: AccountLoadState
    var isRefreshing: Bool
    var lastRefreshError: CodexUsageError? = nil
    var id: String { account.id }
    var isAutomaticActivationEligible: Bool {
      guard case .loaded(let snapshot) = state else { return false }
      return snapshot.fiveHourLimit != nil
    }
    mutating func apply(snapshot: UsageSnapshot) { state = .loaded(snapshot); lastRefreshError = nil }
    mutating func applyRefreshFailure(_ error: CodexUsageError, authenticationRequired: Bool = false) {
      if case .loaded = state { lastRefreshError = error }
      else { state = authenticationRequired ? .needsAuthentication : .failed(error); lastRefreshError = nil }
    }
  }

  @Published private(set) var accountStates: [AccountViewState] = [.init(account: .systemDefault, state: .loading, isRefreshing: false)]
  @Published private(set) var isRefreshingAll = false
  @Published private(set) var launchAtLoginEnabled = false
  @Published private(set) var launchAtLoginError: String?
  @Published private(set) var isAuthenticating = false
  @Published private(set) var authenticationAccountID: String?
  @Published private(set) var loginChallenge: CodexLoginChallenge?
  @Published private(set) var authenticationMethod: CodexLoginMethod?
  @Published private(set) var authenticationError: String?
  @Published private(set) var accountManagementError: String?
  @Published private(set) var accountManagementNotice: String?
  @Published private(set) var recentlyAddedAccountID: String?
  @Published private(set) var pendingWorkspaceName = ""
  @Published private(set) var systemDefaultRefreshInterval: SystemDefaultRefreshInterval
  @Published private(set) var automaticActivationEnabled: Bool
  @Published private(set) var automaticActivationInProgressAccountIDs = Set<String>()
  @Published private(set) var automaticActivationError: String?
  @Published private(set) var isManagingAccounts = false
  @Published private(set) var legacyImportPreview: ToolbarImport?

  private let backend: any CodexMServing
  private let authorizationPageOpener: @MainActor (URL) -> Bool
  private let automaticScheduleStore: AutomaticUsageScheduleStore
  private let refreshSchedule: UsageRefreshSchedule
  private let noticeDismissDelay: Duration
  private let errorDismissDelay: Duration
  private var refreshTask: Task<Void, Never>?
  private var timerTask: Task<Void, Never>?
  private var authenticationTask: Task<Void, Never>?
  private var mutationTask: Task<Void, Never>?
  private var activationTask: Task<Void, Never>?
  private var noticeTask: Task<Void, Never>?
  private var errorTask: Task<Void, Never>?
  private var authErrorTask: Task<Void, Never>?
  private var addingAccount = false
  private var loginID: String?
  private var authenticationAttempt: UUID?
  private var lastRefreshAll: Date?
  private var lastDefaultRefresh = Date.distantPast
  private var lastManagedRefresh = Date.distantPast
  private var pendingRefreshAll = false
  private var catalogSequence = 0
  private var appliedCatalogSequence = 0

  init(backend: any CodexMServing = CodexMBridge(),
       authorizationPageOpener: @escaping @MainActor (URL) -> Bool = { NSWorkspace.shared.open($0) },
       automaticScheduleStore: AutomaticUsageScheduleStore = AutomaticUsageScheduleStore(),
       refreshSchedule: UsageRefreshSchedule = UsageRefreshSchedule(),
       noticeDismissDelay: Duration = .seconds(5), errorDismissDelay: Duration = .seconds(10)) {
    self.backend = backend; self.authorizationPageOpener = authorizationPageOpener
    self.automaticScheduleStore = automaticScheduleStore; self.refreshSchedule = refreshSchedule
    self.systemDefaultRefreshInterval = refreshSchedule.systemDefaultInterval
    self.automaticActivationEnabled = automaticScheduleStore.isEnabled
    self.noticeDismissDelay = noticeDismissDelay; self.errorDismissDelay = errorDismissDelay
    refreshLaunchAtLoginState()
  }

  deinit {
    refreshTask?.cancel(); timerTask?.cancel(); authenticationTask?.cancel()
    mutationTask?.cancel(); activationTask?.cancel(); noticeTask?.cancel()
    errorTask?.cancel(); authErrorTask?.cancel(); backend.shutdown()
  }
  func shutdown() {
    refreshTask?.cancel(); timerTask?.cancel(); authenticationTask?.cancel()
    mutationTask?.cancel(); activationTask?.cancel(); backend.shutdown()
  }
  var primaryAccountState: AccountLoadState { accountStates.first { $0.account.isSystemDefault }?.state ?? .needsAuthentication }
  var latestFetchedAt: Date? {
    accountStates.compactMap { if case .loaded(let s) = $0.state { return s.fetchedAt }; return nil }.max()
  }
  var canRequestManualRefresh: Bool { refreshTask == nil && !isRefreshingAll }
  var isAddingAccount: Bool { addingAccount && isAuthenticating }
  var deviceLoginInfo: DeviceLoginInfo? { if case .deviceCode(let info) = loginChallenge { return info }; return nil }
  private var authenticationAccount: UsageAccount? { accountStates.first { $0.id == authenticationAccountID }?.account }
  var authenticationTitle: String { authenticationAccount.map { L10n.text("auth.relogin_title", $0.title) } ?? L10n.text("auth.add") }
  var authenticationInstruction: String {
    L10n.text(authenticationMethod == .browser
      ? (addingAccount ? "auth.browser_instruction_new" : "auth.browser_instruction_existing")
      : (addingAccount ? "auth.instruction_new" : "auth.instruction_existing"))
  }
  var authenticationCompletionNote: String {
    L10n.text(authenticationMethod == .browser
      ? (addingAccount ? "auth.browser_completion_new" : "auth.browser_completion_existing")
      : (addingAccount ? "auth.completion_new" : "auth.completion_existing"))
  }
  var nextAutomaticActivationDate: Date? {
    guard automaticActivationEnabled else { return nil }
    let dates = accountStates.filter { $0.isAutomaticActivationEligible && $0.account.activationKey != nil }.map {
      automaticScheduleStore.entry(for: $0.account.activationKey!).nextAttemptDate()
    }
    guard let date = dates.min() else { return nil }
    return max(Date(), date, automaticScheduleStore.nextGlobalAttemptDate())
  }

  func start() {
    guard timerTask == nil else { return }
    refreshAll()
    timerTask = Task { [weak self] in
      while !Task.isCancelled {
        do { try await Task.sleep(for: .seconds(2)) } catch { return }
        guard let self else { return }
        if self.canRequestManualRefresh && !self.isManagingAccounts && !self.isAuthenticating {
          do {
            let changed = try await self.reloadCatalog()
            if changed || Date().timeIntervalSince(self.lastManagedRefresh) >= Double(UsageRefreshSchedule.managedAccountIntervalSeconds) {
              self.refreshAll()
            } else if Date().timeIntervalSince(self.lastDefaultRefresh) >= Double(self.systemDefaultRefreshInterval.rawValue) {
              self.refresh(accountID: UsageAccount.systemDefaultID)
            }
          } catch { self.showAccountManagementError(L10n.errorDescription(error)) }
        }
        self.activateIfDue()
      }
    }
  }

  // Read display metadata only. No credential bytes, native DB, or persistent
  // account app-server connection belongs to this UI process.
  @discardableResult
  func reloadCatalog() async throws -> Bool {
    catalogSequence += 1
    let sequence = catalogSequence
    let catalog: [DesktopAccount] = try await backend.call("accounts.catalog")
    try Task.checkCancellation()
    guard sequence > appliedCatalogSequence else { return false }
    appliedCatalogSequence = sequence
    let old = Dictionary(uniqueKeysWithValues: accountStates.map { ($0.id, $0) })
    let changed = catalog.count != old.count || catalog.contains {
      let a = $0.viewAccount
      return old[a.id]?.account.revision != a.revision
    }
    accountStates = catalog.map { item in
      let account = item.viewAccount
      if var current = old[account.id], current.account.revision == account.revision {
        current.account = account; return current
      }
      return AccountViewState(account: account, state: .loading, isRefreshing: false)
    }
    for item in accountStates {
      guard let identity = item.account.activationKey else { continue }
      let oldID = item.id.hasPrefix("usage-") ? String(item.id.dropFirst(6)).uppercased() : item.id
      automaticScheduleStore.migrate(from: oldID, to: identity)
    }
    return changed
  }

  func refreshAll() {
    guard !isRefreshingAll else { return }
    guard refreshTask == nil else { pendingRefreshAll = true; return }
    if let lastRefreshAll, Date().timeIntervalSince(lastRefreshAll) < 5 { return }
    lastRefreshAll = Date(); isRefreshingAll = true
    beginRefresh(accountID: nil)
  }
  func refresh(accountID: String) { guard refreshTask == nil else { return }; beginRefresh(accountID: accountID) }
  private func beginRefresh(accountID: String?) {
    refreshTask = Task { [weak self] in
      guard let self else { return }
      defer {
        self.refreshTask = nil; self.isRefreshingAll = false
        if self.pendingRefreshAll { self.pendingRefreshAll = false; self.lastRefreshAll = nil; self.refreshAll() }
      }
      do {
        try await self.reloadCatalog()
        let accounts = self.accountStates.map(\.account).filter { accountID == nil || $0.id == accountID }
        for account in accounts {
          guard !Task.isCancelled else { return }
          if account.id != self.authenticationAccountID { await self.refreshNow(account) }
        }
        self.lastDefaultRefresh = Date()
        if accountID == nil { self.lastManagedRefresh = Date() }
      } catch { self.showAccountManagementError(L10n.errorDescription(error)) }
    }
  }
  private func refreshNow(_ account: UsageAccount) async {
    setRefreshing(true, for: account.id)
    defer { setRefreshing(false, for: account.id) }
    do {
      let result: DesktopUsage = try await backend.call("accounts.usage", ["account": .string(account.nativeName)])
      try Task.checkCancellation()
      try await reloadCatalog()
      guard result.revision == account.revision,
        let index = accountStates.firstIndex(where: { $0.id == account.id && $0.account.revision == result.revision })
      else { return }
      accountStates[index].apply(snapshot: try result.snapshot.usage())
    } catch is CancellationError { return }
    catch {
      // The backend rejects reads that cross a default/login change. Clear the
      // old presentation by refreshing the catalog before retaining an error.
      _ = try? await reloadCatalog()
      guard let index = accountStates.firstIndex(where: { $0.id == account.id && $0.account.revision == account.revision }) else { return }
      accountStates[index].applyRefreshFailure(.serverError(L10n.errorDescription(error)))
    }
  }
  private func setRefreshing(_ value: Bool, for id: String) {
    if let i = accountStates.firstIndex(where: { $0.id == id }) { accountStates[i].isRefreshing = value }
  }
  func refreshIfStale(maximumAge: TimeInterval = 60) {
    if latestFetchedAt.map({ Date().timeIntervalSince($0) >= maximumAge }) ?? true { refreshAll() }
  }
  func setSystemDefaultRefreshInterval(_ interval: SystemDefaultRefreshInterval) {
    refreshSchedule.setSystemDefaultInterval(interval); systemDefaultRefreshInterval = interval
  }

  private func manage(_ action: @escaping @MainActor () async throws -> Void) {
    guard !isManagingAccounts else { return }
    isManagingAccounts = true
    mutationTask = Task { [weak self] in
      guard let self else { return }
      defer { self.isManagingAccounts = false; self.mutationTask = nil }
      do { try await action(); try await self.reloadCatalog() }
      catch { self.showAccountManagementError(L10n.errorDescription(error)); _ = try? await self.reloadCatalog() }
    }
  }
  func renameManagedAccount(accountID: String, displayName: String) {
    _ = editPresentation(accountID: accountID, key: "display_name", value: displayName)
  }
  func renameWorkspace(accountID: String, workspaceName: String) -> Bool {
    editPresentation(accountID: accountID, key: "workspace_name", value: workspaceName)
  }
  private func editPresentation(accountID: String, key: String, value: String) -> Bool {
    guard !isManagingAccounts,
      let account = accountStates.first(where: { $0.id == accountID })?.account,
      account.lastKnownWorkspaceFingerprint != nil else { return false }
    let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
    manage { [self] in
      let _: Bool = try await backend.call("accounts.presentation", ["account": .string(account.nativeName),
        "revision": .string(account.revision), key: .string(trimmed)])
    }
    return true
  }
  func reorderAccounts(accountIDs: [String]) -> Bool {
    guard !isManagingAccounts, Set(accountIDs) == Set(accountStates.map(\.id)), Set(accountIDs).count == accountIDs.count else { return false }
    manage { [self] in
      let _: Bool = try await backend.call("accounts.presentation", ["order": .strings(accountIDs.map { $0 == UsageAccount.systemDefaultID ? "default" : $0 })])
    }
    return true
  }
  func removeManagedAccount(accountID: String) {
    guard let account = accountStates.first(where: { $0.id == accountID })?.account, account.isManaged else { return }
    manage { [self] in
      let _: Bool = try await backend.call("accounts.remove", ["account": .string(account.nativeName), "revision": .string(account.revision)])
    }
  }
  func setNativeDefault(accountID: String) {
    guard let account = accountStates.first(where: { $0.id == accountID })?.account, account.isManaged else { return }
    manage { [self] in
      let _: Bool = try await backend.call("accounts.setDefault", ["account": .string(account.nativeName), "revision": .string(account.revision)])
      showAccountManagementNotice(L10n.text("codexm.default_updated"))
    }
  }
  func previewLegacyImport() {
    manage { [self] in legacyImportPreview = try await backend.call("accounts.importLegacy", ["apply": .bool(false)]) }
  }
  func cancelLegacyImport() { legacyImportPreview = nil }
  func applyLegacyImport() {
    manage { [self] in
      let result: ToolbarImport = try await backend.call("accounts.importLegacy", ["apply": .bool(true)])
      try await reloadCatalog()
      for item in result.items {
        if let name = item.account, let a = accountStates.first(where: { $0.account.nativeName == name })?.account,
          let key = a.activationKey { automaticScheduleStore.migrate(from: item.legacy_id, to: key) }
      }
      legacyImportPreview = nil
      showAccountManagementNotice(L10n.text(result.complete ? "codexm.import_complete" : "codexm.import_partial"))
    }
  }
  func connectExistingCodexLogin() { refresh(accountID: UsageAccount.systemDefaultID) }

  func startAddingAccount(method: CodexLoginMethod = .deviceCode, nickname: String? = nil) {
    guard !isAuthenticating else { return }
    let name: String
    if let nickname { name = nickname }
    else {
      let alert = NSAlert(); alert.messageText = L10n.text("codexm.nickname_title")
      alert.informativeText = L10n.text("codexm.nickname_help")
      let field = NSTextField(frame: NSRect(x: 0, y: 0, width: 260, height: 24))
      alert.accessoryView = field; alert.addButton(withTitle: L10n.text("common.continue")); alert.addButton(withTitle: L10n.text("common.cancel"))
      guard alert.runModal() == .alertFirstButtonReturn else { return }; name = field.stringValue
    }
    guard name.range(of: "^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$", options: .regularExpression) != nil,
      name != "default", !accountStates.contains(where: { $0.account.nativeName == name })
    else { showAuthenticationError(L10n.text("codexm.nickname_invalid")); return }
    beginAuthentication(account: name, adding: true, method: method)
  }
  func relogin(accountID: String, method: CodexLoginMethod = .deviceCode) {
    guard let account = accountStates.first(where: { $0.id == accountID })?.account else { return }
    if account.isSystemDefault {
      // Native defaults are role links, never overwritten by an anonymous UI login.
      if let selected = account.selectedNickname { beginAuthentication(account: selected, adding: false, method: method) }
      else { showAuthenticationError(L10n.text("codexm.login_named_default")) }
    } else { beginAuthentication(account: account.nativeName, adding: false, method: method) }
  }
  private func beginAuthentication(account: String, adding: Bool, method: CodexLoginMethod) {
    guard !isAuthenticating else { return }
    let attempt = UUID(); authenticationAttempt = attempt; addingAccount = adding
    isAuthenticating = true; authenticationAccountID = account; authenticationMethod = method
    authenticationError = nil; loginChallenge = nil; pendingWorkspaceName = ""
    authenticationTask = Task { [weak self] in
      guard let self else { return }
      defer { if self.authenticationAttempt == attempt { self.isAuthenticating = false; self.authenticationTask = nil; self.loginChallenge = nil; self.loginID = nil } }
      do {
        var result: DesktopLogin = try await self.backend.call("login.start", ["account": .string(account), "mode": .string(method == .browser ? "browser" : "device")])
        self.loginID = result.login_id
        try Task.checkCancellation()
        if let challenge = try result.challenge(method: method) {
          self.loginChallenge = challenge
          if !self.authorizationPageOpener(challenge.authorizationURL) { self.showAuthenticationError(L10n.text("auth.browser_open_failed")) }
        }
        while result.status == "pending" || result.status == "verifying" {
          try await Task.sleep(for: .seconds(1))
          result = try await self.backend.call("login.status", ["login_id": .string(result.login_id)])
        }
        guard result.status == "succeeded" else { throw CodexMBridgeError.rejected(result.error ?? L10n.text("codexm.login_incomplete")) }
        try await self.reloadCatalog()
        if adding {
          self.recentlyAddedAccountID = account
          if !self.pendingWorkspaceName.isEmpty,
            let current = self.accountStates.first(where: { $0.id == account })?.account {
            let _: Bool = try await self.backend.call("accounts.presentation", ["account": .string(account), "revision": .string(current.revision), "workspace_name": .string(self.pendingWorkspaceName)])
            try await self.reloadCatalog()
          }
        }
        if let current = self.accountStates.first(where: { $0.id == account })?.account { await self.refreshNow(current) }
      } catch {
        if let id = self.loginID {
          // An uncancelled cleanup task is required when Cancel arrived before
          // login.start returned its owner-scoped ID. Never restart login here.
          await Task { [backend = self.backend] in
            let _: DesktopLogin? = try? await backend.call("login.cancel", ["login_id": .string(id)])
          }.value
        }
        if !Task.isCancelled { self.showAuthenticationError(L10n.errorDescription(error)) }
      }
    }
  }
  func cancelCurrentAuthentication() {
    authenticationTask?.cancel()
  }
  func setPendingWorkspaceName(_ workspaceName: String) { pendingWorkspaceName = workspaceName }
  func copyDeviceLoginCode() { if let code = deviceLoginInfo?.userCode { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(code, forType: .string) } }
  func reopenAuthenticationPage() { if let url = loginChallenge?.authorizationURL, !authorizationPageOpener(url) { showAuthenticationError(L10n.text("auth.browser_open_failed")) } }

  func setAutomaticActivationEnabled(_ enabled: Bool) {
    automaticScheduleStore.setEnabled(enabled); automaticActivationEnabled = enabled; automaticActivationError = nil
    if !enabled { activationTask?.cancel() }
  }
  private func activateIfDue() {
    guard automaticActivationEnabled, activationTask == nil, refreshTask == nil,
      !isAuthenticating, !isManagingAccounts, automaticScheduleStore.nextGlobalAttemptDate() <= Date() else { return }
    guard let account = accountStates.first(where: {
      guard $0.isAutomaticActivationEligible, let key = $0.account.activationKey else { return false }
      return automaticScheduleStore.entry(for: key).nextAttemptDate() <= Date()
    })?.account, let key = account.activationKey else { return }
    let now = Date()
    let expression = AutomaticUsagePromptGenerator().makeExpression(excluding: automaticScheduleStore.entry(for: key).lastExpression)
    automaticScheduleStore.recordAttempt(accountID: key, at: now, expression: expression)
    automaticActivationInProgressAccountIDs.insert(account.id)
    activationTask = Task { [weak self] in
      guard let self else { return }
      defer { self.automaticActivationInProgressAccountIDs.remove(account.id); self.activationTask = nil }
      do {
        let _: Bool = try await self.backend.call("accounts.activate", ["account": .string(account.nativeName), "revision": .string(account.revision), "expression": .string(expression)])
        self.automaticScheduleStore.recordSuccess(accountID: key, requestStartedAt: now)
        if !Task.isCancelled { await self.refreshNow(account) }
      } catch { self.automaticActivationError = L10n.errorDescription(error) }
    }
  }

  func openTerminal() {
    do { try CodexMTerminal.open() } catch { showAccountManagementError(L10n.errorDescription(error)) }
  }
  func setLaunchAtLogin(_ enabled: Bool) {
    launchAtLoginError = nil
    do { if enabled { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() } }
    catch { launchAtLoginError = L10n.errorDescription(error) }
    refreshLaunchAtLoginState()
  }
  private func refreshLaunchAtLoginState() { launchAtLoginEnabled = SMAppService.mainApp.status == .enabled }
  func clearAccountManagementError() { errorTask?.cancel(); accountManagementError = nil }
  func clearAccountManagementNotice() { noticeTask?.cancel(); accountManagementNotice = nil }
  func clearAuthenticationError() { authErrorTask?.cancel(); authenticationError = nil }
  func consumeRecentlyAddedAccount(_ id: String) { if recentlyAddedAccountID == id { recentlyAddedAccountID = nil } }
  private func showAccountManagementNotice(_ text: String) {
    clearAccountManagementNotice(); clearAccountManagementError(); accountManagementNotice = text
    noticeTask = Task { [weak self, noticeDismissDelay] in
      do { try await Task.sleep(for: noticeDismissDelay) } catch { return }; self?.accountManagementNotice = nil
    }
  }
  private func showAccountManagementError(_ text: String) {
    clearAccountManagementError(); accountManagementError = text
    errorTask = Task { [weak self, errorDismissDelay] in
      do { try await Task.sleep(for: errorDismissDelay) } catch { return }; self?.accountManagementError = nil
    }
  }
  private func showAuthenticationError(_ text: String) {
    clearAuthenticationError(); authenticationError = text
    authErrorTask = Task { [weak self, errorDismissDelay] in
      do { try await Task.sleep(for: errorDismissDelay) } catch { return }; self?.authenticationError = nil
    }
  }
}
