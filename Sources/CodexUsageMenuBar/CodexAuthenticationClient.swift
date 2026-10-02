import Foundation

enum CodexLoginMethod: String, CaseIterable, Identifiable, Sendable {
  case browser
  case deviceCode

  var id: Self { self }
  var requestType: String { self == .browser ? "chatgpt" : "chatgptDeviceCode" }
  var title: String { L10n.text(self == .browser ? "auth.method_browser" : "auth.method_device") }
  var systemImage: String { self == .browser ? "globe" : "key.viewfinder" }
}

struct DeviceLoginInfo: Equatable, Sendable {
  let loginId: String
  let verificationURL: URL
  let userCode: String
}

struct BrowserLoginInfo: Equatable, Sendable {
  let loginId: String
  let authorizationURL: URL
}

enum CodexLoginChallenge: Equatable, Sendable {
  case browser(BrowserLoginInfo)
  case deviceCode(DeviceLoginInfo)

  var loginId: String {
    switch self {
    case .browser(let info): return info.loginId
    case .deviceCode(let info): return info.loginId
    }
  }

  var authorizationURL: URL {
    switch self {
    case .browser(let info): return info.authorizationURL
    case .deviceCode(let info): return info.verificationURL
    }
  }

  static func parse(_ result: [String: Any], method: CodexLoginMethod) throws -> Self {
    guard result["type"] as? String == method.requestType,
      let loginId = result["loginId"] as? String, !loginId.isEmpty
    else { throw CodexUsageError.invalidResponse }
    switch method {
    case .browser:
      return .browser(BrowserLoginInfo(loginId: loginId,
        authorizationURL: try loginURL(result["authUrl"])))
    case .deviceCode:
      guard let code = result["userCode"] as? String, !code.isEmpty else {
        throw CodexUsageError.invalidResponse
      }
      return .deviceCode(DeviceLoginInfo(loginId: loginId,
        verificationURL: try loginURL(result["verificationUrl"]), userCode: code))
    }
  }

  private static func loginURL(_ value: Any?) throws -> URL {
    // Do not launch file/custom schemes or send the user to an unexpected login host.
    guard let string = value as? String, let url = URL(string: string),
      url.scheme?.lowercased() == "https",
      ["auth.openai.com", "chatgpt.com"].contains(url.host?.lowercased() ?? ""),
      url.user == nil, url.password == nil, url.port == nil || url.port == 443
    else { throw CodexUsageError.invalidResponse }
    return url
  }
}

struct CodexAuthenticationClient: Sendable {
  let timeout: Duration

  init(timeout: Duration = .seconds(10 * 60)) {
    self.timeout = timeout
  }

  func login(
    runtime: CodexRuntime,
    method: CodexLoginMethod = .deviceCode,
    onChallenge: @escaping @Sendable (CodexLoginChallenge) -> Void
  ) async throws {
    let session = CodexLoginSession(runtime: runtime, method: method, timeout: timeout)
    try await session.execute(onChallenge: onChallenge)
  }
}

/// All mutable state and process lifecycle operations stay on parsingQueue.
private final class CodexLoginSession: @unchecked Sendable {
  private let runtime: CodexRuntime
  private let method: CodexLoginMethod
  private let timeout: Duration
  private let process = Process()
  private let standardInput = Pipe()
  private let standardOutput = Pipe()
  private let standardError = Pipe()
  private let parsingQueue = DispatchQueue(label: "org.codeasy.CodexUsage.login")

  private var stdoutBuffer = Data()
  private var stderrBuffer = Data()
  private var continuation: CheckedContinuation<Void, any Error>?
  private var timeoutTask: Task<Void, Never>?
  private var onChallenge: (@Sendable (CodexLoginChallenge) -> Void)?
  private var loginId: String?
  private var terminalResult: Result<Void, any Error>?

  init(runtime: CodexRuntime, method: CodexLoginMethod, timeout: Duration) {
    self.runtime = runtime
    self.method = method
    self.timeout = timeout
  }

  func execute(
    onChallenge: @escaping @Sendable (CodexLoginChallenge) -> Void
  ) async throws {
    try await withTaskCancellationHandler {
      try await withCheckedThrowingContinuation { continuation in
        parsingQueue.async { self.start(continuation: continuation, onChallenge: onChallenge) }
      }
    } onCancel: {
      self.parsingQueue.async { self.finish(with: .failure(CancellationError())) }
    }
  }

  private func start(
    continuation: CheckedContinuation<Void, any Error>,
    onChallenge: @escaping @Sendable (CodexLoginChallenge) -> Void
  ) {
    if let terminalResult {
      continuation.resume(with: terminalResult)
      return
    }
    self.continuation = continuation
    self.onChallenge = onChallenge

    process.executableURL = runtime.executableURL
    process.arguments = ["app-server", "--listen", "stdio://"]
    process.environment = runtime.environment
    process.currentDirectoryURL = runtime.codexHomeURL
    process.standardInput = standardInput
    process.standardOutput = standardOutput
    process.standardError = standardError

    standardOutput.fileHandleForReading.readabilityHandler = { [weak self] handle in
      let data = handle.availableData
      guard let self, !data.isEmpty else { return }
      self.parsingQueue.async { [self] in consumeStandardOutput(data) }
    }
    standardError.fileHandleForReading.readabilityHandler = { [weak self] handle in
      let data = handle.availableData
      guard let self, !data.isEmpty else { return }
      self.parsingQueue.async { [self] in appendStandardError(data) }
    }
    process.terminationHandler = { [weak self] process in
      guard let self else { return }
      self.parsingQueue.async {
        let stderr = self.standardErrorText()
        let message =
          stderr.isEmpty
          ? L10n.text("auth.process_exit", process.terminationStatus)
          : stderr
        self.finish(with: .failure(CodexUsageError.serverError(message)))
      }
    }

    do {
      try process.run()
      try send([
        "id": 1,
        "method": "initialize",
        "params": [
          "clientInfo": [
            "name": "codex_usage_menubar",
            "title": "Codex Usage",
            "version": Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "1.7.5",
          ],
          "capabilities": ["experimentalApi": true],
        ],
      ])
      timeoutTask = Task { [weak self, timeout] in
        try? await Task.sleep(for: timeout)
        guard !Task.isCancelled else { return }
        self?.parsingQueue.async { [weak self] in
          self?.finish(with: .failure(CodexUsageError.timedOut))
        }
      }
    } catch {
      finish(with: .failure(CodexUsageError.launchFailed(error.localizedDescription)))
    }
  }

  private func send(_ object: [String: Any]) throws {
    var data = try JSONSerialization.data(withJSONObject: object)
    data.append(0x0A)
    try standardInput.fileHandleForWriting.write(contentsOf: data)
  }

  private func consumeStandardOutput(_ data: Data) {
    stdoutBuffer.append(data)
    while let newline = stdoutBuffer.firstIndex(of: 0x0A) {
      let line = stdoutBuffer[..<newline]
      stdoutBuffer.removeSubrange(...newline)
      guard !line.isEmpty else { continue }
      handleServerMessage(Data(line))
    }
  }

  private func handleServerMessage(_ data: Data) {
    guard terminalResult == nil else { return }
    guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
      return
    }

    if let method = json["method"] as? String,
      method == "account/login/completed",
      let params = json["params"] as? [String: Any]
    {
      guard let completedLoginId = params["loginId"] as? String,
        completedLoginId == loginId
      else { return }
      if (params["success"] as? Bool) == true {
        finish(with: .success(()))
      } else {
        let error = params["error"] as? String ?? L10n.text("auth.failed")
        finish(with: .failure(CodexUsageError.notAuthenticated(error)))
      }
      return
    }

    guard let identifier = (json["id"] as? NSNumber)?.intValue else { return }
    if let error = json["error"] as? [String: Any] {
      let message = error["message"] as? String ?? L10n.text("auth.failed")
      finish(with: .failure(CodexUsageError.serverError(message)))
      return
    }

    do {
      switch identifier {
      case 1:
        try send(["method": "initialized", "params": [:]])
        try send([
          "id": 2,
          "method": "account/login/start",
          "params": ["type": method.requestType],
        ])
      case 2:
        guard let result = json["result"] as? [String: Any] else {
          throw CodexUsageError.invalidResponse
        }
        let challenge = try CodexLoginChallenge.parse(result, method: method)
        loginId = challenge.loginId
        onChallenge?(challenge)
      default:
        break
      }
    } catch {
      finish(with: .failure(error))
    }
  }

  private func appendStandardError(_ data: Data) {
    stderrBuffer.append(data)
    if stderrBuffer.count > 16 * 1024 {
      stderrBuffer.removeFirst(stderrBuffer.count - (16 * 1024))
    }
  }

  private func standardErrorText() -> String {
    String(data: stderrBuffer, encoding: .utf8)?
      .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
  }

  private func finish(with result: Result<Void, any Error>) {
    guard terminalResult == nil else { return }
    terminalResult = result
    let continuation = self.continuation
    self.continuation = nil
    self.onChallenge = nil
    let timeoutTask = self.timeoutTask
    self.timeoutTask = nil

    timeoutTask?.cancel()
    standardOutput.fileHandleForReading.readabilityHandler = nil
    standardError.fileHandleForReading.readabilityHandler = nil
    if process.isRunning {
      if case .failure = result, let loginId {
        try? send(["id": 3, "method": "account/login/cancel", "params": ["loginId": loginId]])
      }
      process.terminate()
    }
    try? standardInput.fileHandleForWriting.close()
    continuation?.resume(with: result)
  }
}
