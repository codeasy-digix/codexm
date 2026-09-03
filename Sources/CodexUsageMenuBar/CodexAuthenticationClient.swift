import Foundation

struct DeviceLoginInfo: Equatable, Sendable {
  let loginId: String
  let verificationURL: URL
  let userCode: String
}

struct CodexAuthenticationClient: Sendable {
  let timeout: Duration

  init(timeout: Duration = .seconds(10 * 60)) {
    self.timeout = timeout
  }

  func login(
    runtime: CodexRuntime,
    onChallenge: @escaping @Sendable (DeviceLoginInfo) -> Void
  ) async throws {
    let session = DeviceLoginSession(runtime: runtime, timeout: timeout)
    try await session.execute(onChallenge: onChallenge)
  }
}

private final class DeviceLoginSession: @unchecked Sendable {
  private let runtime: CodexRuntime
  private let timeout: Duration
  private let process = Process()
  private let standardInput = Pipe()
  private let standardOutput = Pipe()
  private let standardError = Pipe()
  private let parsingQueue = DispatchQueue(label: "org.codeasy.CodexUsage.login")
  private let stateLock = NSLock()

  private var stdoutBuffer = Data()
  private var stderrBuffer = Data()
  private var continuation: CheckedContinuation<Void, any Error>?
  private var timeoutTask: Task<Void, Never>?
  private var onChallenge: (@Sendable (DeviceLoginInfo) -> Void)?
  private var loginId: String?

  init(runtime: CodexRuntime, timeout: Duration) {
    self.runtime = runtime
    self.timeout = timeout
  }

  func execute(
    onChallenge: @escaping @Sendable (DeviceLoginInfo) -> Void
  ) async throws {
    try await withTaskCancellationHandler {
      try await withCheckedThrowingContinuation { continuation in
        start(continuation: continuation, onChallenge: onChallenge)
      }
    } onCancel: {
      self.finish(with: .failure(CancellationError()))
    }
  }

  private func start(
    continuation: CheckedContinuation<Void, any Error>,
    onChallenge: @escaping @Sendable (DeviceLoginInfo) -> Void
  ) {
    stateLock.lock()
    self.continuation = continuation
    self.onChallenge = onChallenge
    stateLock.unlock()

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
          ? "Codex 로그인 프로세스가 종료되었습니다 (코드 \(process.terminationStatus))."
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
            "version": "1.5.3",
          ],
          "capabilities": ["experimentalApi": true],
        ],
      ])
      timeoutTask = Task { [weak self, timeout] in
        try? await Task.sleep(for: timeout)
        guard !Task.isCancelled else { return }
        self?.finish(with: .failure(CodexUsageError.timedOut))
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
    guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
      return
    }

    if let method = json["method"] as? String,
      method == "account/login/completed",
      let params = json["params"] as? [String: Any]
    {
      let completedLoginId = params["loginId"] as? String
      guard completedLoginId == nil || completedLoginId == loginId else { return }
      if (params["success"] as? Bool) == true {
        finish(with: .success(()))
      } else {
        let error = params["error"] as? String ?? "ChatGPT 로그인에 실패했습니다."
        finish(with: .failure(CodexUsageError.notAuthenticated(error)))
      }
      return
    }

    guard let identifier = (json["id"] as? NSNumber)?.intValue else { return }
    if let error = json["error"] as? [String: Any] {
      let message = error["message"] as? String ?? "ChatGPT 로그인에 실패했습니다."
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
          "params": ["type": "chatgptDeviceCode"],
        ])
      case 2:
        guard
          let result = json["result"] as? [String: Any],
          let loginId = result["loginId"] as? String,
          let verificationURLString = result["verificationUrl"] as? String,
          let verificationURL = URL(string: verificationURLString),
          let userCode = result["userCode"] as? String
        else {
          throw CodexUsageError.invalidResponse
        }
        self.loginId = loginId
        onChallenge?(
          DeviceLoginInfo(
            loginId: loginId,
            verificationURL: verificationURL,
            userCode: userCode
          )
        )
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
    stateLock.lock()
    guard let continuation else {
      stateLock.unlock()
      return
    }
    self.continuation = nil
    self.onChallenge = nil
    let timeoutTask = self.timeoutTask
    self.timeoutTask = nil
    stateLock.unlock()

    timeoutTask?.cancel()
    standardOutput.fileHandleForReading.readabilityHandler = nil
    standardError.fileHandleForReading.readabilityHandler = nil
    if process.isRunning { process.terminate() }
    continuation.resume(with: result)
  }
}
