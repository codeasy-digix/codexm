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
    var lastRefreshError: CodexUsageError? = nil

    var id: String { account.id }

    var isAutomaticActivationEligible: Bool {
      guard case .loaded(let snapshot) = state else { return false }
      return snapshot.fiveHourLimit != nil
    }

    mutating func apply(snapshot: UsageSnapshot) {
      state = .loaded(snapshot)
      lastRefreshError = nil
    }

    mutating func applyRefreshFailure(
      _ error: CodexUsageError,
      authenticationRequired: Bool = false
    ) {
      if case .loaded = state {
        lastRefreshError = error
      } else {
        state = authenticationRequired ? .needsAuthentication : .failed(error)
        lastRefreshError = nil
      }
    }
  }

  @Published private(set) var accountStates: [AccountViewState]
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

  private enum AuthenticationMode {
    case adding(UsageAccount)
    case relogin(UsageAccount)

    var account: UsageAccount {
      switch self {
      case .adding(let account), .relogin(let account): return account
      }
    }
  }

  private let registry: UsageAccountRegistry
  private let runtimeLocator: CodexRuntimeLocator
  private let client: CodexAppServerClient
  private let authenticationClient: CodexAuthenticationClient
  private let authorizationPageOpener: @MainActor (URL) -> Bool
  private let identityReader: CodexAccountIdentityReader
  private let automaticUsageClient: CodexAutomaticUsageClient
  private let automaticScheduleStore: AutomaticUsageScheduleStore
  private let automaticPromptGenerator: AutomaticUsagePromptGenerator
  private let accountOperationGate: AccountOperationGate
  private let refreshSchedule: UsageRefreshSchedule
  private let noticeDismissDelay: Duration
  private let errorDismissDelay: Duration
  private var refreshTask: Task<Void, Never>?
  private var refreshOperationID: UUID?
  private var timerTask: Task<Void, Never>?
  private var systemDefaultRefreshTask: Task<Void, Never>?
  private var isSystemDefaultScheduledRefreshInProgress = false
  private var automaticActivationTask: Task<Void, Never>?
  private var authenticationTask: Task<Void, Never>?
  private var authenticationErrorDismissTask: Task<Void, Never>?
  private var accountManagementErrorDismissTask: Task<Void, Never>?
  private var accountManagementNoticeDismissTask: Task<Void, Never>?
  private var authenticationMode: AuthenticationMode?
  private var authenticationAttemptID: UUID?
  private var lastRefreshAllRequestedAt: Date?
  private var pendingRefreshAll = false

  private static let refreshAllDebounceInterval: TimeInterval = 5

  init(
    registry: UsageAccountRegistry = UsageAccountRegistry(),
    runtimeLocator: CodexRuntimeLocator = CodexRuntimeLocator(),
    client: CodexAppServerClient = CodexAppServerClient(),
    authenticationClient: CodexAuthenticationClient = CodexAuthenticationClient(),
    authorizationPageOpener: @escaping @MainActor (URL) -> Bool = { NSWorkspace.shared.open($0) },
    identityReader: CodexAccountIdentityReader = CodexAccountIdentityReader(),
    automaticUsageClient: CodexAutomaticUsageClient = CodexAutomaticUsageClient(),
    automaticScheduleStore: AutomaticUsageScheduleStore = AutomaticUsageScheduleStore(),
    automaticPromptGenerator: AutomaticUsagePromptGenerator = AutomaticUsagePromptGenerator(),
    accountOperationGate: AccountOperationGate = AccountOperationGate(),
    refreshSchedule: UsageRefreshSchedule = UsageRefreshSchedule(),
    noticeDismissDelay: Duration = .seconds(5),
    errorDismissDelay: Duration = .seconds(10)
  ) {
    self.registry = registry
    self.runtimeLocator = runtimeLocator
    self.client = client
    self.authenticationClient = authenticationClient
    self.authorizationPageOpener = authorizationPageOpener
    self.identityReader = identityReader
    self.automaticUsageClient = automaticUsageClient
    self.automaticScheduleStore = automaticScheduleStore
    self.automaticPromptGenerator = automaticPromptGenerator
    self.accountOperationGate = accountOperationGate
    self.refreshSchedule = refreshSchedule
    self.systemDefaultRefreshInterval = refreshSchedule.systemDefaultInterval
    self.noticeDismissDelay = noticeDismissDelay
    self.errorDismissDelay = errorDismissDelay
    self.automaticActivationEnabled = automaticScheduleStore.isEnabled

    do {
      self.accountStates = try registry.loadAccounts().map {
        AccountViewState(account: $0, state: .loading, isRefreshing: false)
      }
    } catch {
      self.accountStates = [
        AccountViewState(
          account: .systemDefault,
          state: .failed(.serverError(L10n.errorDescription(error))),
          isRefreshing: false
        )
      ]
    }
    automaticScheduleStore.retain(accountIDs: Set(accountStates.map(\.id)))
    refreshLaunchAtLoginState()
  }

  deinit {
    refreshTask?.cancel()
    timerTask?.cancel()
    systemDefaultRefreshTask?.cancel()
    automaticActivationTask?.cancel()
    authenticationTask?.cancel()
    authenticationErrorDismissTask?.cancel()
    accountManagementErrorDismissTask?.cancel()
    accountManagementNoticeDismissTask?.cancel()
    client.shutdown()
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

  var canRequestManualRefresh: Bool {
    refreshTask == nil && !isRefreshingAll && !isSystemDefaultScheduledRefreshInProgress
  }

  var isAddingAccount: Bool {
    if case .adding = authenticationMode { return true }
    return false
  }

  var deviceLoginInfo: DeviceLoginInfo? {
    guard case .deviceCode(let info) = loginChallenge else { return nil }
    return info
  }

  var authenticationTitle: String {
    if let account = authenticationAccount {
      return L10n.text("auth.relogin_title", account.title)
    }
    return L10n.text("auth.add")
  }

  var authenticationInstruction: String {
    if authenticationMethod == .browser {
      return L10n.text(authenticationAccount != nil ? "auth.browser_instruction_existing" : "auth.browser_instruction_new")
    }
    if authenticationAccount != nil {
      return L10n.text("auth.instruction_existing")
    }
    return L10n.text("auth.instruction_new")
  }

  var authenticationCompletionNote: String {
    if authenticationMethod == .browser {
      return L10n.text(authenticationAccount != nil ? "auth.browser_completion_existing" : "auth.browser_completion_new")
    }
    if authenticationAccount != nil {
      return L10n.text("auth.completion_existing")
    }
    return L10n.text("auth.completion_new")
  }

  var nextAutomaticActivationDate: Date? {
    guard automaticActivationEnabled else { return nil }
    let now = Date()
    guard let nextAccountDate = accountStates.compactMap({ viewState -> Date? in
      guard viewState.id != authenticationAccountID else { return nil }
      guard viewState.isAutomaticActivationEligible else { return nil }
      return max(
        now,
        automaticScheduleStore.entry(for: viewState.id).nextAttemptDate()
      )
    }).min() else {
      return nil
    }
    return max(
      nextAccountDate,
      automaticScheduleStore.nextGlobalAttemptDate()
    )
  }

  func start() {
    guard timerTask == nil else { return }
    refreshAll()
    startAutomaticActivationSchedulerIfNeeded()
    startSystemDefaultRefreshScheduler()

    timerTask = Task { [weak self] in
      while !Task.isCancelled {
        try? await Task.sleep(
          for: .seconds(UsageRefreshSchedule.managedAccountIntervalSeconds)
        )
        guard !Task.isCancelled else { return }
        self?.refreshManagedAccountsIfIdle()
      }
    }
  }

  func setSystemDefaultRefreshInterval(_ interval: SystemDefaultRefreshInterval) {
    guard interval != systemDefaultRefreshInterval else { return }
    refreshSchedule.setSystemDefaultInterval(interval)
    systemDefaultRefreshInterval = interval
    startSystemDefaultRefreshScheduler()
  }

  func setAutomaticActivationEnabled(_ enabled: Bool) {
    automaticScheduleStore.setEnabled(enabled)
    automaticActivationEnabled = enabled
    automaticActivationError = nil
    if enabled {
      startAutomaticActivationSchedulerIfNeeded()
    } else {
      stopAutomaticActivationScheduler()
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
    // A panel opened during an individual/scheduled read still needs the
    // other connections checked. Coalesce those requests into one next pass.
    guard !isRefreshingAll else { return }
    guard refreshTask == nil, !isSystemDefaultScheduledRefreshInProgress else {
      pendingRefreshAll = true
      return
    }
    pendingRefreshAll = false
    let now = Date()
    if let lastRefreshAllRequestedAt,
      now.timeIntervalSince(lastRefreshAllRequestedAt) < Self.refreshAllDebounceInterval
    {
      return
    }
    let accounts = accountStates.map(\.account)
    guard !accounts.isEmpty else { return }
    lastRefreshAllRequestedAt = now
    isRefreshingAll = true
    let operationID = UUID()
    refreshOperationID = operationID

    refreshTask = Task { [weak self] in
      guard let self else { return }
      defer { self.finishRefreshOperation(operationID, wasRefreshingAll: true) }
      for account in accounts {
        guard !Task.isCancelled else { return }
        guard !self.isRefreshing(accountID: account.id) else { continue }
        await self.refreshNow(account)
      }
    }
  }

  func refresh(accountID: String) {
    guard let account = accountStates.first(where: { $0.id == accountID })?.account else { return }
    guard refreshTask == nil, !isRefreshingAll, !isSystemDefaultScheduledRefreshInProgress else {
      return
    }
    guard !isRefreshing(accountID: accountID) else { return }
    let operationID = UUID()
    refreshOperationID = operationID
    refreshTask = Task { [weak self] in
      guard let self else { return }
      defer { self.finishRefreshOperation(operationID, wasRefreshingAll: false) }
      await self.refreshNow(account)
    }
  }

  private func startSystemDefaultRefreshScheduler() {
    systemDefaultRefreshTask?.cancel()
    let interval = systemDefaultRefreshInterval.duration
    systemDefaultRefreshTask = Task { [weak self] in
      while !Task.isCancelled {
        do {
          try await Task.sleep(for: interval)
        } catch {
          return
        }
        guard !Task.isCancelled else { return }
        await self?.refreshSystemDefaultOnSchedule()
      }
    }
  }

  private func refreshSystemDefaultOnSchedule() async {
    guard refreshTask == nil, !isRefreshingAll else { return }
    guard
      let viewState = accountStates.first(where: { $0.account.isSystemDefault }),
      !viewState.isRefreshing
    else {
      return
    }
    if case .needsAuthentication = viewState.state { return }
    isSystemDefaultScheduledRefreshInProgress = true
    defer {
      isSystemDefaultScheduledRefreshInProgress = false
      drainPendingRefreshAll()
    }
    await refreshNow(viewState.account)
  }

  private func isRefreshing(accountID: String) -> Bool {
    accountStates.first(where: { $0.id == accountID })?.isRefreshing ?? false
  }

  private func refreshManagedAccountsIfIdle() {
    guard refreshTask == nil else { return }
    let accounts = accountStates.map(\.account).filter(\.isManaged)
    guard !accounts.isEmpty else { return }
    let operationID = UUID()
    refreshOperationID = operationID
    refreshTask = Task { [weak self] in
      guard let self else { return }
      defer { self.finishRefreshOperation(operationID, wasRefreshingAll: false) }
      for account in accounts {
        guard !Task.isCancelled else { return }
        guard !self.isRefreshing(accountID: account.id) else { continue }
        await self.refreshNow(account)
      }
    }
  }

  func connectExistingCodexLogin() {
    clearAuthenticationError()
    let panel = NSOpenPanel()
    panel.title = L10n.text("auth.connect_default")
    panel.message = L10n.text("auth.select_folder")
    panel.prompt = L10n.text("auth.use_folder")
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
      showAuthenticationError(L10n.errorDescription(error))
    }
  }

  func startAddingAccount(method: CodexLoginMethod = .deviceCode) {
    cancelAuthentication(discardPendingAccount: true)
    clearAuthenticationError()
    clearAccountManagementError()
    clearAccountManagementNotice()

    do {
      let account = try registry.beginManagedAccount()
      let home = try registry.pendingCodexHomeURL(for: account)
      let runtime = try runtimeLocator.locateManagedAccount(codexHomeURL: home)
      beginAuthentication(mode: .adding(account), method: method, runtime: runtime)
    } catch {
      showAuthenticationError(L10n.errorDescription(error))
    }
  }

  func relogin(accountID: String, method: CodexLoginMethod = .deviceCode) {
    guard !isAuthenticating else { return }
    // The system default must remain this Mac's existing login, not an app-owned sign-in target.
    guard let account = accountStates.first(where: { $0.id == accountID })?.account,
      account.isManaged
    else { return }
    clearAuthenticationError()
    clearAccountManagementError()
    clearAccountManagementNotice()

    do {
      let runtime = try runtime(for: account)
      beginAuthentication(mode: .relogin(account), method: method, runtime: runtime)
    } catch {
      showAuthenticationError(L10n.errorDescription(error))
    }
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
      try registry.updateAccount(accountStates[index].account)
    } catch {
      showAccountManagementError(L10n.errorDescription(error))
    }
  }

  func renameWorkspace(accountID: String, workspaceName: String) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    var account = accountStates[index].account
    let trimmed = workspaceName.trimmingCharacters(in: .whitespacesAndNewlines)
    account.workspaceName = trimmed.isEmpty ? nil : trimmed

    do {
      try registry.updateAccount(account)
      accountStates[index].account = account
      clearAccountManagementError()
    } catch {
      showAccountManagementError(L10n.errorDescription(error))
    }
  }

  func setPendingWorkspaceName(_ workspaceName: String) {
    pendingWorkspaceName = workspaceName
  }

  @discardableResult
  func reorderAccounts(accountIDs: [String]) -> Bool {
    let currentIDs = accountStates.map(\.id)
    guard accountIDs.count == currentIDs.count,
      Set(accountIDs) == Set(currentIDs)
    else { return false }
    guard accountIDs != currentIDs else { return true }
    do {
      try registry.saveAccountOrder(accountIDs)
      let currentStates = accountStates
      accountStates = accountIDs.compactMap { id in currentStates.first { $0.id == id } }
      clearAccountManagementError()
      return true
    } catch {
      showAccountManagementError(L10n.text("account.order_failed", L10n.errorDescription(error)))
      return false
    }
  }

  func removeManagedAccount(accountID: String) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    let account = accountStates[index].account
    guard account.isManaged else { return }
    stopAutomaticActivationScheduler()
    cancelRefresh()
    if authenticationAccountID == accountID {
      cancelAuthentication(discardPendingAccount: false)
    }
    client.invalidateSession(sessionID: accountID)
    do {
      try registry.removeManagedAccount(account)
      accountStates.remove(at: index)
      automaticScheduleStore.remove(accountID: accountID)
      automaticActivationInProgressAccountIDs.remove(accountID)
      showAccountManagementNotice(L10n.text("account.removed", account.title))
      if recentlyAddedAccountID == accountID {
        recentlyAddedAccountID = nil
      }
    } catch {
      showAccountManagementError(L10n.errorDescription(error))
    }
    startAutomaticActivationSchedulerIfNeeded()
  }

  func copyDeviceLoginCode() {
    guard let userCode = deviceLoginInfo?.userCode else { return }
    copyToPasteboard(userCode)
  }

  func reopenAuthenticationPage() {
    guard let url = loginChallenge?.authorizationURL else { return }
    if !authorizationPageOpener(url) {
      showAuthenticationError(L10n.text("auth.browser_open_failed"))
    }
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
      launchAtLoginError = L10n.errorDescription(error)
    }
    refreshLaunchAtLoginState()
  }

  func clearAccountManagementError() {
    accountManagementErrorDismissTask?.cancel()
    accountManagementErrorDismissTask = nil
    accountManagementError = nil
  }

  func clearAccountManagementNotice() {
    accountManagementNoticeDismissTask?.cancel()
    accountManagementNoticeDismissTask = nil
    accountManagementNotice = nil
  }

  func clearAuthenticationError() {
    authenticationErrorDismissTask?.cancel()
    authenticationErrorDismissTask = nil
    authenticationError = nil
  }

  func consumeRecentlyAddedAccount(_ accountID: String) {
    guard recentlyAddedAccountID == accountID else { return }
    recentlyAddedAccountID = nil
  }

  private func refreshNow(_ account: UsageAccount) async {
    setRefreshing(true, for: account.id)
    defer { setRefreshing(false, for: account.id) }
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
      let identityRevision = identityReader.fingerprint(codexHomeURL: runtime.codexHomeURL)
      let snapshot = try await accountOperationGate.withPermit(for: account.id) {
        try await client.fetchUsage(
          sessionID: account.id,
          diagnosticLabel: account.isSystemDefault ? "default" : "managed",
          identityRevision: identityRevision,
          codexURL: runtime.executableURL,
          environmentOverride: runtime.environment,
          onUpdate: { [weak self] snapshot in
            Task { @MainActor [weak self] in
              self?.applyServerUpdate(snapshot, accountID: account.id)
            }
          }
        )
      }
      guard !Task.isCancelled else { return }
      updateAccountMetadata(
        accountID: account.id,
        snapshot: snapshot,
        workspaceFingerprint: identityReader.fingerprint(codexHomeURL: runtime.codexHomeURL)
      )
      setSnapshot(snapshot, for: account.id)
    } catch is CancellationError {
      return
    } catch CodexRuntimeError.defaultCodexHomeUnavailable {
      recordRefreshFailure(
        .notAuthenticated(L10n.text("error.default_missing")),
        authenticationRequired: true,
        for: account.id
      )
    } catch let error as CodexUsageError {
      let authenticationRequired: Bool
      if case .notAuthenticated = error {
        authenticationRequired = true
      } else {
        authenticationRequired = false
      }
      recordRefreshFailure(error, authenticationRequired: authenticationRequired, for: account.id)
    } catch {
      recordRefreshFailure(.serverError(L10n.errorDescription(error)), for: account.id)
    }
  }

  private func runtime(for account: UsageAccount) throws -> CodexRuntime {
    if account.isSystemDefault {
      return try runtimeLocator.locateDefaultAccount()
    }
    let home = try registry.managedCodexHomeURL(for: account)
    return try runtimeLocator.locateManagedAccount(codexHomeURL: home)
  }

  private func beginAuthentication(mode: AuthenticationMode, method: CodexLoginMethod, runtime: CodexRuntime) {
    cancelAuthentication(discardPendingAccount: true)
    stopAutomaticActivationScheduler()
    authenticationMode = mode
    let attemptID = UUID()
    authenticationAttemptID = attemptID
    authenticationMethod = method
    switch mode {
    case .adding(let account):
      authenticationAccountID = account.id
      pendingWorkspaceName = account.normalizedWorkspaceName ?? ""
      client.invalidateSession(sessionID: account.id)
    case .relogin(let account):
      authenticationAccountID = account.id
      pendingWorkspaceName = ""
      client.invalidateSession(sessionID: account.id)
    }
    isAuthenticating = true
    loginChallenge = nil

    authenticationTask = Task { [weak self, authenticationClient, client, identityReader] in
      guard let self else { return }
      do {
        try await authenticationClient.login(runtime: runtime, method: method) { [weak self] challenge in
          Task { @MainActor [weak self] in
            guard let self, self.authenticationAttemptID == attemptID, self.isAuthenticating else { return }
            self.loginChallenge = challenge
            if case .deviceCode = challenge { self.copyDeviceLoginCode() }
            self.reopenAuthenticationPage()
          }
        }
        guard !Task.isCancelled, self.authenticationAttemptID == attemptID else { return }
        let snapshot = try await client.fetchUsage(
          sessionID: mode.account.id,
          diagnosticLabel: mode.account.isSystemDefault ? "default" : "managed",
          identityRevision: identityReader.fingerprint(codexHomeURL: runtime.codexHomeURL),
          codexURL: runtime.executableURL,
          environmentOverride: runtime.environment
        )
        guard !Task.isCancelled, self.authenticationAttemptID == attemptID else { return }
        self.finishAuthentication(mode: mode, snapshot: snapshot, runtime: runtime)
      } catch is CancellationError {
        return
      } catch {
        guard !Task.isCancelled, self.authenticationAttemptID == attemptID else { return }
        self.failAuthentication(mode: mode, error: error)
      }
    }
  }

  private func finishAuthentication(
    mode: AuthenticationMode,
    snapshot: UsageSnapshot,
    runtime: CodexRuntime
  ) {
    isAuthenticating = false
    loginChallenge = nil
    authenticationMethod = nil
    authenticationAttemptID = nil
    authenticationTask = nil
    clearAuthenticationError()

    switch mode {
    case .adding(var account):
      let trimmedWorkspaceName = pendingWorkspaceName.trimmingCharacters(
        in: .whitespacesAndNewlines
      )
      account.workspaceName = trimmedWorkspaceName.isEmpty ? nil : trimmedWorkspaceName
      account.lastKnownEmail = snapshot.accountEmail
      account.lastKnownPlanType = snapshot.planType
      let workspaceFingerprint = identityReader.fingerprint(
        codexHomeURL: runtime.codexHomeURL
      )
      account.lastKnownWorkspaceFingerprint = workspaceFingerprint

      let candidateIdentity = CodexAccountIdentity(
        fingerprint: workspaceFingerprint,
        email: snapshot.accountEmail,
        planType: snapshot.planType
      )
      if let duplicate = duplicateAccount(matching: candidateIdentity) {
        try? registry.discardPendingAccount(account)
        authenticationMode = nil
        authenticationAccountID = nil
        showAccountManagementError(
          L10n.text("account.duplicate", duplicate.title)
        )
        pendingWorkspaceName = ""
        startAutomaticActivationSchedulerIfNeeded()
        return
      }

      do {
        try registry.commitPendingAccount(account)
        client.invalidateSession(sessionID: account.id)
        accountStates.append(
          AccountViewState(
            account: account,
            state: .loaded(snapshot),
            isRefreshing: false
          )
        )
        recentlyAddedAccountID = account.id
        let workspaceLabel = account.workspaceDisplayLabel.map { " · \($0)" } ?? ""
        showAccountManagementNotice(
          L10n.text("account.added", account.title, workspaceLabel)
        )
      } catch {
        try? registry.discardPendingAccount(account)
        showAccountManagementError(L10n.errorDescription(error))
      }
      authenticationMode = nil
      authenticationAccountID = nil
      pendingWorkspaceName = ""
    case .relogin(let account):
      updateAccountMetadata(
        accountID: account.id,
        snapshot: snapshot,
        workspaceFingerprint: identityReader.fingerprint(codexHomeURL: runtime.codexHomeURL)
      )
      setSnapshot(snapshot, for: account.id)
      authenticationMode = nil
      authenticationAccountID = nil
      pendingWorkspaceName = ""
      showAccountManagementNotice(L10n.text("account.reconnected", account.title))
    }
    startAutomaticActivationSchedulerIfNeeded()
  }

  private func failAuthentication(mode: AuthenticationMode, error: Error) {
    isAuthenticating = false
    loginChallenge = nil
    authenticationMethod = nil
    authenticationAttemptID = nil
    authenticationTask = nil
    showAuthenticationError(L10n.errorDescription(error))
    authenticationMode = nil
    authenticationAccountID = nil
    pendingWorkspaceName = ""

    if case .adding(let account) = mode {
      client.invalidateSession(sessionID: account.id)
      try? registry.discardPendingAccount(account)
    }
    startAutomaticActivationSchedulerIfNeeded()
  }

  private func cancelAuthentication(discardPendingAccount: Bool) {
    authenticationTask?.cancel()
    authenticationTask = nil
    isAuthenticating = false
    loginChallenge = nil
    authenticationMethod = nil
    authenticationAttemptID = nil

    if discardPendingAccount,
      case .adding(let account) = authenticationMode
    {
      client.invalidateSession(sessionID: account.id)
      try? registry.discardPendingAccount(account)
    }
    authenticationMode = nil
    authenticationAccountID = nil
    pendingWorkspaceName = ""
    startAutomaticActivationSchedulerIfNeeded()
  }

  private func startAutomaticActivationSchedulerIfNeeded() {
    guard automaticActivationEnabled, automaticActivationTask == nil else { return }
    automaticActivationTask = Task { [weak self] in
      guard let self else { return }
      await self.runAutomaticActivationLoop()
    }
  }

  private func stopAutomaticActivationScheduler() {
    automaticActivationTask?.cancel()
    automaticActivationTask = nil
    automaticActivationInProgressAccountIDs.removeAll()
  }

  private func runAutomaticActivationLoop() async {
    while !Task.isCancelled, automaticActivationEnabled {
      await runDueAutomaticActivations()
      guard !Task.isCancelled, automaticActivationEnabled else { return }
      do {
        try await Task.sleep(for: AutomaticUsageSchedulePolicy.schedulerPollInterval)
      } catch {
        return
      }
    }
  }

  private func runDueAutomaticActivations() async {
    let now = Date()
    guard automaticActivationInProgressAccountIDs.isEmpty else { return }
    guard automaticScheduleStore.nextGlobalAttemptDate() <= now else { return }

    let dueAccount = accountStates.enumerated().compactMap {
      index, viewState -> (index: Int, nextDate: Date, account: UsageAccount)? in
      guard viewState.id != authenticationAccountID else { return nil }
      guard viewState.isAutomaticActivationEligible else { return nil }
      let nextDate = automaticScheduleStore.entry(for: viewState.id).nextAttemptDate()
      guard nextDate <= now else { return nil }
      return (index, nextDate, viewState.account)
    }.min { lhs, rhs in
      if lhs.nextDate == rhs.nextDate {
        return lhs.index < rhs.index
      }
      return lhs.nextDate < rhs.nextDate
    }?.account
    guard let account = dueAccount else { return }
    automaticActivationError = nil

    let attemptAt = Date()
    let previousExpression = automaticScheduleStore.entry(for: account.id).lastExpression
    let expression = automaticPromptGenerator.makeExpression(excluding: previousExpression)
    automaticScheduleStore.recordAttempt(
      accountID: account.id,
      at: attemptAt,
      expression: expression
    )
    automaticActivationInProgressAccountIDs.insert(account.id)
    await performAutomaticActivation(
      account: account,
      attemptAt: attemptAt,
      expression: expression
    )
  }

  private func performAutomaticActivation(
    account: UsageAccount,
    attemptAt: Date,
    expression: String
  ) async {
    defer { automaticActivationInProgressAccountIDs.remove(account.id) }

    do {
      let runtime = try runtime(for: account)
      defer { withExtendedLifetime(runtime) {} }
      let prompt = automaticPromptGenerator.prompt(for: expression)
      let workingDirectoryURL = FileManager.default.temporaryDirectory
      try await accountOperationGate.withPermit(for: account.id) {
        try await automaticUsageClient.performRequest(
          codexURL: runtime.executableURL,
          environmentOverride: runtime.environment,
          workingDirectoryURL: workingDirectoryURL,
          prompt: prompt
        )
      }
      guard !Task.isCancelled else { return }
      automaticScheduleStore.recordSuccess(
        accountID: account.id,
        requestStartedAt: attemptAt
      )
      await refreshNow(account)
    } catch is CancellationError {
      return
    } catch {
      guard !Task.isCancelled else { return }
      let message = "\(account.title): \(L10n.errorDescription(error))"
      if let existing = automaticActivationError, !existing.isEmpty {
        automaticActivationError = "\(existing)\n\(message)"
      } else {
        automaticActivationError = message
      }
    }
  }

  private func cancelRefresh() {
    refreshTask?.cancel()
    refreshTask = nil
    refreshOperationID = nil
    pendingRefreshAll = false
    isRefreshingAll = false
    for index in accountStates.indices {
      if isSystemDefaultScheduledRefreshInProgress,
        accountStates[index].account.isSystemDefault
      {
        continue
      }
      accountStates[index].isRefreshing = false
    }
  }

  private func finishRefreshOperation(_ operationID: UUID, wasRefreshingAll: Bool) {
    guard refreshOperationID == operationID else { return }
    refreshTask = nil
    refreshOperationID = nil
    if wasRefreshingAll {
      isRefreshingAll = false
    }
    drainPendingRefreshAll()
  }

  private func drainPendingRefreshAll() {
    guard pendingRefreshAll, canRequestManualRefresh else { return }
    refreshAll()
  }

  private func setState(_ state: AccountLoadState, for accountID: String) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    accountStates[index].state = state
  }

  private func setSnapshot(_ snapshot: UsageSnapshot, for accountID: String) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    accountStates[index].apply(snapshot: snapshot)
  }

  private func recordRefreshFailure(
    _ error: CodexUsageError,
    authenticationRequired: Bool = false,
    for accountID: String
  ) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    accountStates[index].applyRefreshFailure(
      error,
      authenticationRequired: authenticationRequired
    )
  }

  private func applyServerUpdate(_ snapshot: UsageSnapshot, accountID: String) {
    guard accountStates.contains(where: { $0.id == accountID }) else { return }
    updateAccountMetadata(accountID: accountID, snapshot: snapshot, workspaceFingerprint: nil)
    setSnapshot(snapshot, for: accountID)
  }

  private func setRefreshing(_ refreshing: Bool, for accountID: String) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    accountStates[index].isRefreshing = refreshing
  }

  private func updateAccountMetadata(
    accountID: String,
    snapshot: UsageSnapshot,
    workspaceFingerprint: String?
  ) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    accountStates[index].account.lastKnownEmail = snapshot.accountEmail
    accountStates[index].account.lastKnownPlanType = snapshot.planType
    if let workspaceFingerprint {
      accountStates[index].account.lastKnownWorkspaceFingerprint = workspaceFingerprint
    }
    if accountStates[index].account.isManaged {
      do {
        try registry.updateAccount(accountStates[index].account)
      } catch {
        showAccountManagementError(L10n.errorDescription(error))
      }
    }
  }

  private func duplicateAccount(
    matching candidateIdentity: CodexAccountIdentity
  ) -> UsageAccount? {
    accountStates.first { viewState in
      let snapshot: UsageSnapshot?
      if case .loaded(let loadedSnapshot) = viewState.state {
        snapshot = loadedSnapshot
      } else {
        snapshot = nil
      }

      var fingerprint: String?
      if let existingRuntime = try? runtime(for: viewState.account) {
        fingerprint = identityReader.fingerprint(codexHomeURL: existingRuntime.codexHomeURL)
        withExtendedLifetime(existingRuntime) {}
      }
      fingerprint = fingerprint ?? viewState.account.lastKnownWorkspaceFingerprint

      let existingIdentity = CodexAccountIdentity(
        fingerprint: fingerprint,
        email: snapshot?.accountEmail ?? viewState.account.lastKnownEmail,
        planType: snapshot?.planType ?? viewState.account.lastKnownPlanType
      )
      return candidateIdentity.matches(existingIdentity)
    }?.account
  }

  private var authenticationAccount: UsageAccount? {
    guard let authenticationAccountID else { return nil }
    return accountStates.first { $0.id == authenticationAccountID }?.account
  }

  private func showAccountManagementNotice(_ message: String) {
    clearAccountManagementError()
    clearAccountManagementNotice()
    accountManagementNotice = message
    let delay = noticeDismissDelay
    accountManagementNoticeDismissTask = Task { [weak self] in
      do {
        try await Task.sleep(for: delay)
      } catch {
        return
      }
      guard !Task.isCancelled, self?.accountManagementNotice == message else { return }
      self?.accountManagementNotice = nil
      self?.accountManagementNoticeDismissTask = nil
    }
  }

  private func showAccountManagementError(_ message: String) {
    clearAccountManagementNotice()
    clearAccountManagementError()
    accountManagementError = message
    let delay = errorDismissDelay
    accountManagementErrorDismissTask = Task { [weak self] in
      do {
        try await Task.sleep(for: delay)
      } catch {
        return
      }
      guard !Task.isCancelled, self?.accountManagementError == message else { return }
      self?.accountManagementError = nil
      self?.accountManagementErrorDismissTask = nil
    }
  }

  private func showAuthenticationError(_ message: String) {
    clearAuthenticationError()
    authenticationError = message
    let delay = errorDismissDelay
    authenticationErrorDismissTask = Task { [weak self] in
      do {
        try await Task.sleep(for: delay)
      } catch {
        return
      }
      guard !Task.isCancelled, self?.authenticationError == message else { return }
      self?.authenticationError = nil
      self?.authenticationErrorDismissTask = nil
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
