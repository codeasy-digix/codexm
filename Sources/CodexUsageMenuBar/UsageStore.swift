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

  @Published private(set) var accountStates: [AccountViewState]
  @Published private(set) var isRefreshingAll = false
  @Published private(set) var launchAtLoginEnabled = false
  @Published private(set) var launchAtLoginError: String?
  @Published private(set) var isAuthenticating = false
  @Published private(set) var authenticationAccountID: String?
  @Published private(set) var deviceLoginInfo: DeviceLoginInfo?
  @Published private(set) var authenticationError: String?
  @Published private(set) var accountManagementError: String?
  @Published private(set) var accountManagementNotice: String?
  @Published private(set) var recentlyAddedAccountID: String?
  @Published private(set) var pendingWorkspaceName = ""
  @Published private(set) var automaticActivationEnabled: Bool
  @Published private(set) var automaticActivationInProgressAccountIDs = Set<String>()
  @Published private(set) var automaticActivationError: String?

  private enum AuthenticationMode {
    case adding(UsageAccount)
    case relogin(UsageAccount)
  }

  private let registry: UsageAccountRegistry
  private let runtimeLocator: CodexRuntimeLocator
  private let client: CodexAppServerClient
  private let authenticationClient: CodexAuthenticationClient
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
  private var automaticActivationTask: Task<Void, Never>?
  private var authenticationTask: Task<Void, Never>?
  private var authenticationErrorDismissTask: Task<Void, Never>?
  private var accountManagementErrorDismissTask: Task<Void, Never>?
  private var accountManagementNoticeDismissTask: Task<Void, Never>?
  private var authenticationMode: AuthenticationMode?

  init(
    registry: UsageAccountRegistry = UsageAccountRegistry(),
    runtimeLocator: CodexRuntimeLocator = CodexRuntimeLocator(),
    client: CodexAppServerClient = CodexAppServerClient(),
    authenticationClient: CodexAuthenticationClient = CodexAuthenticationClient(),
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
    self.identityReader = identityReader
    self.automaticUsageClient = automaticUsageClient
    self.automaticScheduleStore = automaticScheduleStore
    self.automaticPromptGenerator = automaticPromptGenerator
    self.accountOperationGate = accountOperationGate
    self.refreshSchedule = refreshSchedule
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
          state: .failed(.serverError(error.localizedDescription)),
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

  var isAddingAccount: Bool {
    if case .adding = authenticationMode { return true }
    return false
  }

  var authenticationTitle: String {
    if let account = authenticationAccount {
      return "\(account.title) 다시 로그인"
    }
    return "계정 또는 워크스페이스 추가"
  }

  var authenticationInstruction: String {
    if authenticationAccount != nil {
      return "열린 브라우저에서 아래 코드를 입력하고, 이 연결에서 사용할 계정과 워크스페이스를 선택하세요."
    }
    return "열린 브라우저에서 아래 코드를 입력한 뒤 사용할 계정과 워크스페이스를 선택하세요. 같은 계정의 다른 워크스페이스도 별도로 추가할 수 있습니다."
  }

  var authenticationCompletionNote: String {
    if authenticationAccount != nil {
      return "코드는 클립보드에 자동으로 복사했습니다. 인증이 끝나면 계정 정보가 바로 갱신됩니다."
    }
    return "코드는 클립보드에 자동으로 복사했습니다. 인증이 끝나면 독립된 CODEX_HOME 연결로 바로 추가됩니다."
  }

  var nextAutomaticActivationDate: Date? {
    guard automaticActivationEnabled else { return nil }
    let now = Date()
    guard let nextAccountDate = accountStates.compactMap({ viewState -> Date? in
      guard viewState.id != authenticationAccountID else { return nil }
      if case .needsAuthentication = viewState.state {
        return nil
      }
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

    if refreshSchedule.systemDefaultIntervalSeconds < UsageRefreshSchedule.standardIntervalSeconds {
      systemDefaultRefreshTask = Task { [weak self] in
        while !Task.isCancelled {
          guard let interval = self?.refreshSchedule.systemDefaultInterval else { return }
          do {
            try await Task.sleep(for: interval)
          } catch {
            return
          }
          guard !Task.isCancelled else { return }
          self?.refreshSystemDefaultIfStale()
        }
      }
    }

    timerTask = Task { [weak self] in
      while !Task.isCancelled {
        try? await Task.sleep(for: .seconds(UsageRefreshSchedule.standardIntervalSeconds))
        guard !Task.isCancelled else { return }
        self?.refreshAll()
      }
    }
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
    cancelRefresh()
    let accounts = accountStates.map(\.account)
    guard !accounts.isEmpty else { return }
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
    cancelRefresh()
    let operationID = UUID()
    refreshOperationID = operationID
    refreshTask = Task { [weak self] in
      guard let self else { return }
      defer { self.finishRefreshOperation(operationID, wasRefreshingAll: false) }
      await self.refreshNow(account)
    }
  }

  private func refreshSystemDefaultIfStale() {
    guard refreshTask == nil, !isRefreshingAll else { return }
    guard
      let viewState = accountStates.first(where: { $0.account.isSystemDefault }),
      !viewState.isRefreshing
    else {
      return
    }
    if case .needsAuthentication = viewState.state { return }
    if case .loaded(let snapshot) = viewState.state,
      Date().timeIntervalSince(snapshot.fetchedAt)
        < TimeInterval(refreshSchedule.systemDefaultIntervalSeconds)
    {
      return
    }
    refresh(accountID: viewState.id)
  }

  private func isRefreshing(accountID: String) -> Bool {
    accountStates.first(where: { $0.id == accountID })?.isRefreshing ?? false
  }

  func connectExistingCodexLogin() {
    clearAuthenticationError()
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
      showAuthenticationError(error.localizedDescription)
    }
  }

  func startAddingAccount() {
    cancelAuthentication(discardPendingAccount: true)
    clearAuthenticationError()
    clearAccountManagementError()
    clearAccountManagementNotice()

    do {
      let account = try registry.beginManagedAccount()
      let home = try registry.pendingCodexHomeURL(for: account)
      let runtime = try runtimeLocator.locateManagedAccount(codexHomeURL: home)
      beginAuthentication(mode: .adding(account), runtime: runtime)
    } catch {
      showAuthenticationError(error.localizedDescription)
    }
  }

  func relogin(accountID: String) {
    guard !isAuthenticating else { return }
    guard let account = accountStates.first(where: { $0.id == accountID })?.account else { return }
    clearAuthenticationError()
    clearAccountManagementError()
    clearAccountManagementNotice()

    do {
      let runtime = try runtime(for: account)
      beginAuthentication(mode: .relogin(account), runtime: runtime)
    } catch {
      showAuthenticationError(error.localizedDescription)
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
      showAccountManagementError(error.localizedDescription)
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
      showAccountManagementError(error.localizedDescription)
    }
  }

  func setPendingWorkspaceName(_ workspaceName: String) {
    pendingWorkspaceName = workspaceName
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
    do {
      try registry.removeManagedAccount(account)
      accountStates.remove(at: index)
      automaticScheduleStore.remove(accountID: accountID)
      automaticActivationInProgressAccountIDs.remove(accountID)
      showAccountManagementNotice("\(account.title) 연결을 삭제했습니다.")
      if recentlyAddedAccountID == accountID {
        recentlyAddedAccountID = nil
      }
    } catch {
      showAccountManagementError(error.localizedDescription)
    }
    startAutomaticActivationSchedulerIfNeeded()
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
      let snapshot = try await accountOperationGate.withPermit(for: account.id) {
        try await client.fetchUsage(
          codexURL: runtime.executableURL,
          environmentOverride: runtime.environment
        )
      }
      guard !Task.isCancelled else { return }
      updateAccountMetadata(
        accountID: account.id,
        snapshot: snapshot,
        workspaceFingerprint: identityReader.fingerprint(codexHomeURL: runtime.codexHomeURL)
      )
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
    stopAutomaticActivationScheduler()
    authenticationMode = mode
    switch mode {
    case .adding(let account):
      authenticationAccountID = account.id
      pendingWorkspaceName = account.normalizedWorkspaceName ?? ""
    case .relogin(let account):
      authenticationAccountID = account.id
      pendingWorkspaceName = ""
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
        self.finishAuthentication(mode: mode, snapshot: snapshot, runtime: runtime)
      } catch is CancellationError {
        return
      } catch {
        guard !Task.isCancelled else { return }
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
    deviceLoginInfo = nil
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
          "이미 등록된 계정/워크스페이스 연결입니다: \(duplicate.title)"
        )
        pendingWorkspaceName = ""
        startAutomaticActivationSchedulerIfNeeded()
        return
      }

      do {
        try registry.commitPendingAccount(account)
        accountStates.append(
          AccountViewState(
            account: account,
            state: .loaded(snapshot),
            isRefreshing: false
          )
        )
        sortAccountStates()
        recentlyAddedAccountID = account.id
        let workspaceLabel = account.workspaceDisplayLabel.map { " · \($0)" } ?? ""
        showAccountManagementNotice(
          "\(account.title)\(workspaceLabel) 연결을 추가했습니다."
        )
      } catch {
        try? registry.discardPendingAccount(account)
        showAccountManagementError(error.localizedDescription)
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
      setState(.loaded(snapshot), for: account.id)
      authenticationMode = nil
      authenticationAccountID = nil
      pendingWorkspaceName = ""
      showAccountManagementNotice("\(account.title) 계정을 다시 연결했습니다.")
    }
    startAutomaticActivationSchedulerIfNeeded()
  }

  private func failAuthentication(mode: AuthenticationMode, error: Error) {
    isAuthenticating = false
    deviceLoginInfo = nil
    authenticationTask = nil
    showAuthenticationError(error.localizedDescription)
    authenticationMode = nil
    authenticationAccountID = nil
    pendingWorkspaceName = ""

    if case .adding(let account) = mode {
      try? registry.discardPendingAccount(account)
    }
    startAutomaticActivationSchedulerIfNeeded()
  }

  private func cancelAuthentication(discardPendingAccount: Bool) {
    authenticationTask?.cancel()
    authenticationTask = nil
    isAuthenticating = false
    deviceLoginInfo = nil

    if discardPendingAccount,
      case .adding(let account) = authenticationMode
    {
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
      if case .needsAuthentication = viewState.state { return nil }
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
      let message = "\(account.title): \(error.localizedDescription)"
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
    isRefreshingAll = false
    for index in accountStates.indices {
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
  }

  private func setState(_ state: AccountLoadState, for accountID: String) {
    guard let index = accountStates.firstIndex(where: { $0.id == accountID }) else { return }
    accountStates[index].state = state
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
        showAccountManagementError(error.localizedDescription)
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
