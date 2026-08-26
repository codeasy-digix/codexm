import AppKit
import Foundation
import ServiceManagement

@MainActor
final class UsageStore: ObservableObject {
  enum AccountLoadState: Equatable {
    case loading
    case loaded(UsageSnapshot)
    case needsAuthentication
    case failed(CodexUsageError)
  }

  struct AccountViewState: Equatable, Identifiable {
    var account: UsageAccount
    var state: AccountLoadState
    var isRefreshing: Bool

    var id: String { account.id }
  }

  struct PendingAccountConfirmation: Equatable {
    let account: UsageAccount
    let snapshot: UsageSnapshot
  }

  @Published private(set) var accountStates: [AccountViewState]
  @Published private(set) var isRefreshingAll = false
  @Published private(set) var launchAtLoginEnabled = false
  @Published private(set) var launchAtLoginError: String?
  @Published private(set) var isAuthenticating = false
  @Published private(set) var authenticationAccountID: String?
  @Published private(set) var deviceLoginInfo: DeviceLoginInfo?
  @Published private(set) var authenticationError: String?
  @Published private(set) var pendingAccountConfirmation: PendingAccountConfirmation?
  @Published private(set) var accountManagementError: String?

  private enum AuthenticationMode {
    case adding(UsageAccount)
    case relogin(UsageAccount)
  }

  private let registry: UsageAccountRegistry
  private let runtimeLocator: CodexRuntimeLocator
  private let client: CodexAppServerClient
  private let authenticationClient: CodexAuthenticationClient
  private var refreshTask: Task<Void, Never>?
  private var timerTask: Task<Void, Never>?
  private var authenticationTask: Task<Void, Never>?
  private var authenticationMode: AuthenticationMode?

  init(
    registry: UsageAccountRegistry = UsageAccountRegistry(),
    runtimeLocator: CodexRuntimeLocator = CodexRuntimeLocator(),
    client: CodexAppServerClient = CodexAppServerClient(),
    authenticationClient: CodexAuthenticationClient = CodexAuthenticationClient()
  ) {
    self.registry = registry
    self.runtimeLocator = runtimeLocator
    self.client = client
    self.authenticationClient = authenticationClient

    do {
      self.accountStates = try registry.loadAccounts().map {
        AccountViewState(account: $0, state: .loading, isRefreshing: false)
      }
    } catch {
      self.accountStates = [
        AccountViewState(
          account: .systemDefault,
          state: .failed(.serverError(error.localizedDescription)),
          isRefreshing: false
        )
      ]
    }
    refreshLaunchAtLoginState()
  }

  deinit {
    refreshTask?.cancel()
    timerTask?.cancel()
    authenticationTask?.cancel()
  }

  var primaryAccountState: AccountLoadState {
    accountStates.first(where: { $0.account.isSystemDefault })?.state ?? .needsAuthentication
  }

  var latestFetchedAt: Date? {
    accountStates.compactMap { viewState -> Date? in
      guard case .loaded(let snapshot) = viewState.state else { return nil }
      return snapshot.fetchedAt
    }.max()
  }

  func start() {
    guard timerTask == nil else { return }
    refreshAll()

    timerTask = Task { [weak self] in
      while !Task.isCancelled {
        try? await Task.sleep(for: .seconds(15 * 60))
        guard !Task.isCancelled else { return }
        self?.refreshAll()
      }
    }
  }

  func refreshIfStale(maximumAge: TimeInterval = 60) {
    let now = Date()
    let hasStaleAccount = accountStates.contains { viewState in
      guard case .loaded(let snapshot) = viewState.state else { return true }
      return now.timeIntervalSince(snapshot.fetchedAt) >= maximumAge
    }
    if hasStaleAccount, !isRefreshingAll {
      refreshAll()
    }
  }

  func refreshAll() {
    cancelRefresh()
    let accounts = accountStates.map(\.account)
    guard !accounts.isEmpty else { return }
    isRefreshingAll = true

    refreshTask = Task { [weak self] in
      guard let self else { return }
      for account in accounts {
        guard !Task.isCancelled else { break }
        await self.refreshNow(account)
      }
      guard !Task.isCancelled else { return }
      self.isRefreshingAll = false
    }
  }

  func refresh(accountID: String) {
    guard let account = accountStates.first(where: { $0.id == accountID })?.account else { return }
    cancelRefresh()
    refreshTask = Task { [weak self] in
      guard let self else { return }
      await self.refreshNow(account)
    }
  }

  func connectExistingCodexLogin() {
    authenticationError = nil
    let panel = NSOpenPanel()
    panel.title = "기본 Codex 로그인 연결"
    panel.message = "Codex CLI가 사용하는 .codex 폴더를 선택하세요. 계정 로그인은 다시 하지 않습니다."
    panel.prompt = "이 폴더 사용"
    panel.canChooseFiles = false
    panel.canChooseDirectories = true
    panel.allowsMultipleSelection = false
    panel.showsHiddenFiles = true
    panel.directoryURL = runtimeLocator.suggestedCodexHomeURL

    guard panel.runModal() == .OK, let url = panel.url else { return }
    do {
      try runtimeLocator.saveCodexHomeAccess(url)
      refresh(accountID: UsageAccount.systemDefaultID)
    } catch {
      authenticationError = error.localizedDescription
    }
  }

  func startAddingAccount() {
    cancelAuthentication(discardPendingAccount: true)
    authenticationError = nil
    accountManagementError = nil

    do {
      let account = try registry.beginManagedAccount()
      let home = try registry.pendingCodexHomeURL(for: account)
      let runtime = try runtimeLocator.locateManagedAccount(codexHomeURL: home)
      beginAuthentication(mode: .adding(account), runtime: runtime)
    } catch {
      authenticationError = error.localizedDescription
    }
  }

  func relogin(accountID: String) {
    guard !isAuthenticating, pendingAccountConfirmation == nil else { return }
    guard let account = accountStates.first(where: { $0.id == accountID })?.account else { return }
    authenticationError = nil
    accountManagementError = nil

    do {
      let runtime = try runtime(for: account)
      beginAuthentication(mode: .relogin(account), runtime: runtime)
    } catch {
      authenticationError = error.localizedDescription
    }
  }

  func confirmPendingAccount() {
    guard let confirmation = pendingAccountConfirmation else { return }
    do {
      try registry.commitPendingAccount(confirmation.account)
      accountStates.append(
        AccountViewState(
          account: confirmation.account,
          state: .loaded(confirmation.snapshot),
          isRefreshing: false
        )
      )
      sortAccountStates()
      pendingAccountConfirmation = nil
      authenticationMode = nil
      authenticationAccountID = nil
      authenticationError = nil
    } catch {
      accountManagementError = error.localizedDescription
    }
  }

  func cancelPendingAccount() {
    guard let confirmation = pendingAccountConfirmation else { return }
    try? registry.discardPendingAccount(confirmation.account)
    pendingAccountConfirmation = nil
    authenticationMode = nil
    authenticationAccountID = nil
  }

  func cancelCurrentAuthentication() {
    cancelAuthentication(discardPendingAccount: true)
  }

  func renameManagedAccount(accountID: String, displayName: String) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    guard accountStates[index].account.isManaged else { return }
    let trimmed = displayName.trimmingCharacters(in: .whitespacesAndNewlines)
    accountStates[index].account.displayName = trimmed.isEmpty ? nil : trimmed
    do {
      try registry.updateManagedAccount(accountStates[index].account)
    } catch {
      accountManagementError = error.localizedDescription
    }
  }

  func removeManagedAccount(accountID: String) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    let account = accountStates[index].account
    guard account.isManaged else { return }
    cancelRefresh()
    if authenticationAccountID == accountID {
      cancelAuthentication(discardPendingAccount: false)
    }
    do {
      try registry.removeManagedAccount(account)
      accountStates.remove(at: index)
    } catch {
      accountManagementError = error.localizedDescription
    }
  }

  func copyDeviceLoginCode() {
    guard let userCode = deviceLoginInfo?.userCode else { return }
    copyToPasteboard(userCode)
  }

  func reopenDeviceLoginPage() {
    guard let url = deviceLoginInfo?.verificationURL else { return }
    NSWorkspace.shared.open(url)
  }

  func setLaunchAtLogin(_ enabled: Bool) {
    launchAtLoginError = nil
    do {
      if enabled {
        try SMAppService.mainApp.register()
      } else {
        try SMAppService.mainApp.unregister()
      }
    } catch {
      launchAtLoginError = error.localizedDescription
    }
    refreshLaunchAtLoginState()
  }

  func clearAccountManagementError() {
    accountManagementError = nil
  }

  private func refreshNow(_ account: UsageAccount) async {
    setRefreshing(true, for: account.id)
    if let index = accountStates.firstIndex(where: { $0.id == account.id }),
      case .loaded = accountStates[index].state
    {
      // Preserve the last value while refreshing.
    } else {
      setState(.loading, for: account.id)
    }

    do {
      let runtime = try runtime(for: account)
      defer { withExtendedLifetime(runtime) {} }
      let snapshot = try await client.fetchUsage(
        codexURL: runtime.executableURL,
        environmentOverride: runtime.environment
      )
      guard !Task.isCancelled else { return }
      updateAccountMetadata(accountID: account.id, snapshot: snapshot)
      setState(.loaded(snapshot), for: account.id)
    } catch is CancellationError {
      return
    } catch CodexRuntimeError.defaultCodexHomeUnavailable {
      setState(.needsAuthentication, for: account.id)
    } catch let error as CodexUsageError {
      if case .notAuthenticated = error {
        setState(.needsAuthentication, for: account.id)
      } else {
        setState(.failed(error), for: account.id)
      }
    } catch {
      setState(.failed(.serverError(error.localizedDescription)), for: account.id)
    }
    setRefreshing(false, for: account.id)
  }

  private func runtime(for account: UsageAccount) throws -> CodexRuntime {
    if account.isSystemDefault {
      return try runtimeLocator.locateDefaultAccount()
    }
    let home = try registry.managedCodexHomeURL(for: account)
    return try runtimeLocator.locateManagedAccount(codexHomeURL: home)
  }

  private func beginAuthentication(mode: AuthenticationMode, runtime: CodexRuntime) {
    cancelAuthentication(discardPendingAccount: true)
    authenticationMode = mode
    switch mode {
    case .adding(let account), .relogin(let account):
      authenticationAccountID = account.id
    }
    isAuthenticating = true
    deviceLoginInfo = nil

    authenticationTask = Task { [weak self, authenticationClient, client] in
      guard let self else { return }
      do {
        try await authenticationClient.login(runtime: runtime) { [weak self] info in
          Task { @MainActor [weak self] in
            guard let self else { return }
            self.deviceLoginInfo = info
            self.copyDeviceLoginCode()
            NSWorkspace.shared.open(info.verificationURL)
          }
        }
        guard !Task.isCancelled else { return }
        let snapshot = try await client.fetchUsage(
          codexURL: runtime.executableURL,
          environmentOverride: runtime.environment
        )
        guard !Task.isCancelled else { return }
        self.finishAuthentication(mode: mode, snapshot: snapshot)
      } catch is CancellationError {
        return
      } catch {
        guard !Task.isCancelled else { return }
        self.failAuthentication(mode: mode, error: error)
      }
    }
  }

  private func finishAuthentication(mode: AuthenticationMode, snapshot: UsageSnapshot) {
    isAuthenticating = false
    deviceLoginInfo = nil
    authenticationTask = nil

    switch mode {
    case .adding(var account):
      account.lastKnownEmail = snapshot.accountEmail
      account.lastKnownPlanType = snapshot.planType
      pendingAccountConfirmation = PendingAccountConfirmation(account: account, snapshot: snapshot)
    case .relogin(let account):
      updateAccountMetadata(accountID: account.id, snapshot: snapshot)
      setState(.loaded(snapshot), for: account.id)
      authenticationMode = nil
      authenticationAccountID = nil
    }
  }

  private func failAuthentication(mode: AuthenticationMode, error: Error) {
    isAuthenticating = false
    deviceLoginInfo = nil
    authenticationTask = nil
    authenticationError = error.localizedDescription
    authenticationMode = nil
    authenticationAccountID = nil

    if case .adding(let account) = mode {
      try? registry.discardPendingAccount(account)
    }
  }

  private func cancelAuthentication(discardPendingAccount: Bool) {
    authenticationTask?.cancel()
    authenticationTask = nil
    isAuthenticating = false
    deviceLoginInfo = nil

    if discardPendingAccount,
      case .adding(let account) = authenticationMode,
      pendingAccountConfirmation == nil
    {
      try? registry.discardPendingAccount(account)
    }
    authenticationMode = nil
    authenticationAccountID = nil
  }

  private func cancelRefresh() {
    refreshTask?.cancel()
    refreshTask = nil
    isRefreshingAll = false
    for index in accountStates.indices {
      accountStates[index].isRefreshing = false
    }
  }

  private func setState(_ state: AccountLoadState, for accountID: String) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    accountStates[index].state = state
  }

  private func setRefreshing(_ refreshing: Bool, for accountID: String) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    accountStates[index].isRefreshing = refreshing
  }

  private func updateAccountMetadata(accountID: String, snapshot: UsageSnapshot) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    accountStates[index].account.lastKnownEmail = snapshot.accountEmail
    accountStates[index].account.lastKnownPlanType = snapshot.planType
    if accountStates[index].account.isManaged {
      do {
        try registry.updateManagedAccount(accountStates[index].account)
      } catch {
        accountManagementError = error.localizedDescription
      }
    }
  }

  private func sortAccountStates() {
    accountStates.sort { lhs, rhs in
      if lhs.account.isSystemDefault != rhs.account.isSystemDefault {
        return lhs.account.isSystemDefault
      }
      return lhs.account.createdAt < rhs.account.createdAt
    }
  }

  private func copyToPasteboard(_ text: String) {
    NSPasteboard.general.clearContents()
    NSPasteboard.general.setString(text, forType: .string)
  }

  private func refreshLaunchAtLoginState() {
    launchAtLoginEnabled = SMAppService.mainApp.status == .enabled
  }
}
