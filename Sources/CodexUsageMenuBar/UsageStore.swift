import AppKit
import Foundation
import ServiceManagement

@MainActor
final class UsageStore: ObservableObject {
  enum State: Equatable {
    case loading
    case loaded(UsageSnapshot)
    case missingCLI
    case failed(CodexUsageError)
  }

  @Published private(set) var state: State = .loading
  @Published private(set) var isRefreshing = false
  @Published private(set) var launchAtLoginEnabled = false
  @Published private(set) var launchAtLoginError: String?

  private let locator: CodexLocator
  private let client: CodexAppServerClient
  private var refreshTask: Task<Void, Never>?
  private var timerTask: Task<Void, Never>?

  init(
    locator: CodexLocator = CodexLocator(),
    client: CodexAppServerClient = CodexAppServerClient()
  ) {
    self.locator = locator
    self.client = client
    refreshLaunchAtLoginState()
  }

  deinit {
    refreshTask?.cancel()
    timerTask?.cancel()
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

    refreshTask = Task { [weak self, locator, client] in
      guard let self else { return }
      guard let codexURL = locator.locate() else {
        self.state = .missingCLI
        self.isRefreshing = false
        return
      }

      do {
        let snapshot = try await client.fetchUsage(codexURL: codexURL)
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

  func copyInstallCommand() {
    copyToPasteboard("npm install -g @openai/codex")
  }

  func copyLoginCommand() {
    copyToPasteboard("codex login")
  }

  func copyUpdateCommand() {
    copyToPasteboard("codex update")
  }

  func openTerminal() {
    NSWorkspace.shared.openApplication(
      at: URL(fileURLWithPath: "/System/Applications/Utilities/Terminal.app"),
      configuration: .init()
    )
  }

  private func copyToPasteboard(_ text: String) {
    NSPasteboard.general.clearContents()
    NSPasteboard.general.setString(text, forType: .string)
  }

  private func refreshLaunchAtLoginState() {
    launchAtLoginEnabled = SMAppService.mainApp.status == .enabled
  }
}
