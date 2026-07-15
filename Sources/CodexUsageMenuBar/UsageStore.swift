import AppKit
import Foundation
import ServiceManagement

@MainActor
final class UsageStore: ObservableObject {
  enum State: Equatable {
    case loading
    case loaded(UsageSnapshot)
    case missingRuntime
    case failed(CodexUsageError)
  }

  @Published private(set) var state: State = .loading
  @Published private(set) var isRefreshing = false
  @Published private(set) var launchAtLoginEnabled = false
  @Published private(set) var launchAtLoginError: String?
  @Published private(set) var isAuthenticating = false
  @Published private(set) var deviceLoginInfo: DeviceLoginInfo?
  @Published private(set) var authenticationError: String?

  private let runtimeLocator: CodexRuntimeLocator
  private let client: CodexAppServerClient
  private let authenticationClient: CodexAuthenticationClient
  private var refreshTask: Task<Void, Never>?
  private var timerTask: Task<Void, Never>?
  private var authenticationTask: Task<Void, Never>?

  init(
    runtimeLocator: CodexRuntimeLocator = CodexRuntimeLocator(),
    client: CodexAppServerClient = CodexAppServerClient(),
    authenticationClient: CodexAuthenticationClient = CodexAuthenticationClient()
  ) {
    self.runtimeLocator = runtimeLocator
    self.client = client
    self.authenticationClient = authenticationClient
    refreshLaunchAtLoginState()
  }

  deinit {
    refreshTask?.cancel()
    timerTask?.cancel()
    authenticationTask?.cancel()
  }

  func start() {
    guard timerTask == nil else { return }
    refresh()

    timerTask = Task { [weak self] in
      while !Task.isCancelled {
        try? await Task.sleep(for: .seconds(5 * 60))
        guard !Task.isCancelled else { return }
        self?.refresh()
      }
    }
  }

  func refresh() {
    refreshTask?.cancel()
    isRefreshing = true

    refreshTask = Task { [weak self, runtimeLocator, client] in
      guard let self else { return }
      let runtime: CodexRuntime
      do {
        runtime = try runtimeLocator.locate()
      } catch {
        self.state = .missingRuntime
        self.isRefreshing = false
        return
      }
      defer { withExtendedLifetime(runtime) {} }

      do {
        let snapshot = try await client.fetchUsage(
          codexURL: runtime.executableURL,
          environmentOverride: runtime.environment
        )
        guard !Task.isCancelled else { return }
        self.state = .loaded(snapshot)
      } catch is CancellationError {
        return
      } catch let error as CodexUsageError {
        guard !Task.isCancelled else { return }
        self.state = .failed(error)
      } catch {
        guard !Task.isCancelled else { return }
        self.state = .failed(.serverError(error.localizedDescription))
      }

      self.isRefreshing = false
    }
  }

  func connectExistingCodexLogin() {
    authenticationError = nil
    let panel = NSOpenPanel()
    panel.title = "기존 Codex 로그인 연결"
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
      deviceLoginInfo = nil
      refresh()
    } catch {
      authenticationError = error.localizedDescription
    }
  }

  func startDeviceLogin() {
    authenticationTask?.cancel()
    authenticationError = nil
    deviceLoginInfo = nil
    isAuthenticating = true

    authenticationTask = Task { [weak self, runtimeLocator, authenticationClient] in
      guard let self else { return }
      do {
        let runtime = try runtimeLocator.locate()
        try await authenticationClient.login(runtime: runtime) { [weak self] info in
          Task { @MainActor [weak self] in
            guard let self else { return }
            self.deviceLoginInfo = info
            self.copyDeviceLoginCode()
            NSWorkspace.shared.open(info.verificationURL)
          }
        }
        guard !Task.isCancelled else { return }
        self.isAuthenticating = false
        self.deviceLoginInfo = nil
        self.refresh()
      } catch is CancellationError {
        return
      } catch {
        guard !Task.isCancelled else { return }
        self.isAuthenticating = false
        self.authenticationError = error.localizedDescription
      }
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

  private func copyToPasteboard(_ text: String) {
    NSPasteboard.general.clearContents()
    NSPasteboard.general.setString(text, forType: .string)
  }

  private func refreshLaunchAtLoginState() {
    launchAtLoginEnabled = SMAppService.mainApp.status == .enabled
  }
}
