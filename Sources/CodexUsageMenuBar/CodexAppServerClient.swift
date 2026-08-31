import Foundation

struct CodexAppServerClient: Sendable {
  let timeout: Duration
  let environment: [String: String]

  init(
    timeout: Duration = .seconds(15),
    environment: [String: String] = ProcessInfo.processInfo.environment
  ) {
    self.timeout = timeout
    self.environment = environment
  }

  func fetchUsage(codexURL: URL) async throws -> UsageSnapshot {
    try await fetchUsage(codexURL: codexURL, environmentOverride: nil)
  }

  func fetchUsage(
    codexURL: URL,
    environmentOverride: [String: String]?
  ) async throws -> UsageSnapshot {
    let session = AppServerSession(
      codexURL: codexURL,
      timeout: timeout,
      environment: Self.processEnvironment(
        for: codexURL,
        base: environmentOverride ?? environment
      )
    )
    return try await session.execute()
  }

  static func processEnvironment(
    for codexURL: URL,
    base: [String: String]
  ) -> [String: String] {
    var environment = base
    let fallbackPath = "/usr/bin:/bin:/usr/sbin:/sbin"
    let existingPath = base["PATH"] ?? fallbackPath
    let preferredDirectories = [
      codexURL.deletingLastPathComponent().standardizedFileURL.path,
      "/opt/homebrew/bin",
      "/usr/local/bin",
    ]

    var seen = Set<String>()
    let path = (preferredDirectories + existingPath.split(separator: ":").map(String.init))
      .filter { !$0.isEmpty && seen.insert($0).inserted }
      .joined(separator: ":")
    environment["PATH"] = path
    return environment
  }
}

private final class AppServerSession: @unchecked Sendable {
  private let codexURL: URL
  private let timeout: Duration
  private let environment: [String: String]
  private let process = Process()
  private let standardInput = Pipe()
  private let standardOutput = Pipe()
  private let standardError = Pipe()
  private let parsingQueue = DispatchQueue(label: "org.codeasy.CodexUsage.app-server")
  private let stateLock = NSLock()

  private var stdoutBuffer = Data()
  private var stderrBuffer = Data()
  private var account: CodexAccount?
  private var continuation: CheckedContinuation<UsageSnapshot, any Error>?
  private var timeoutTask: Task<Void, Never>?

  init(codexURL: URL, timeout: Duration, environment: [String: String]) {
    self.codexURL = codexURL
    self.timeout = timeout
    self.environment = environment
  }

  func execute() async throws -> UsageSnapshot {
    try await withTaskCancellationHandler {
      try await withCheckedThrowingContinuation { continuation in
        start(continuation: continuation)
      }
    } onCancel: {
      self.finish(with: .failure(CancellationError()))
    }
  }

  private func start(continuation: CheckedContinuation<UsageSnapshot, any Error>) {
    stateLock.lock()
    self.continuation = continuation
    stateLock.unlock()

    process.executableURL = codexURL
    process.arguments = ["app-server", "--listen", "stdio://"]
    process.environment = environment
    process.currentDirectoryURL =
      environment["CODEX_HOME"].map {
        URL(fileURLWithPath: $0, isDirectory: true)
      } ?? FileManager.default.homeDirectoryForCurrentUser
    process.standardInput = standardInput
    process.standardOutput = standardOutput
    process.standardError = standardError

    standardOutput.fileHandleForReading.readabilityHandler = { [weak self] handle in
      let data = handle.availableData
      guard let self, !data.isEmpty else { return }
      self.parsingQueue.async { [self] in
        consumeStandardOutput(data)
      }
    }

    standardError.fileHandleForReading.readabilityHandler = { [weak self] handle in
      let data = handle.availableData
      guard let self, !data.isEmpty else { return }
      self.parsingQueue.async { [self] in
        appendStandardError(data)
      }
    }

    process.terminationHandler = { [weak self] process in
      guard let self else { return }
      self.parsingQueue.async {
        let stderr = self.standardErrorText()
        let message =
          stderr.isEmpty
          ? "Codex app-server가 종료되었습니다 (코드 \(process.terminationStatus))."
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
            "version": "1.5.2",
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
    guard
      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
      let identifier = (json["id"] as? NSNumber)?.intValue
    else {
      return  // Notifications do not carry an id and are intentionally ignored.
    }

    if let error = json["error"] as? [String: Any] {
      let message = error["message"] as? String ?? ""
      if identifier == 2 {
        do {
          try requestRateLimits()
        } catch {
          finish(with: .failure(CodexUsageError.serverError(error.localizedDescription)))
        }
        return
      }
      finish(with: .failure(classifyServerError(message)))
      return
    }

    switch identifier {
    case 1:
      do {
        try send(["method": "initialized", "params": [:]])
        try send([
          "id": 2,
          "method": "account/read",
          "params": ["refreshToken": false],
        ])
      } catch {
        finish(with: .failure(CodexUsageError.serverError(error.localizedDescription)))
      }
    case 2:
      account = nil
      if let result = json["result"],
        let resultData = try? JSONSerialization.data(withJSONObject: result),
        let response = try? JSONDecoder().decode(GetAccountResponse.self, from: resultData)
      {
        account = response.account
      }

      do {
        try requestRateLimits()
      } catch {
        finish(with: .failure(CodexUsageError.serverError(error.localizedDescription)))
      }
    case 3:
      guard let result = json["result"] else {
        finish(with: .failure(CodexUsageError.invalidResponse))
        return
      }

      do {
        let resultData = try JSONSerialization.data(withJSONObject: result)
        let response = try JSONDecoder().decode(GetAccountRateLimitsResponse.self, from: resultData)
        finish(with: .success(try response.usageSnapshot(account: account)))
      } catch let error as CodexUsageError {
        finish(with: .failure(error))
      } catch {
        finish(with: .failure(CodexUsageError.invalidResponse))
      }
    default:
      break
    }
  }

  private func requestRateLimits() throws {
    try send(["id": 3, "method": "account/rateLimits/read", "params": NSNull()])
  }

  private func classifyServerError(_ message: String) -> CodexUsageError {
    let lowercased = message.lowercased()
    if lowercased.contains("not logged")
      || lowercased.contains("unauthorized")
      || lowercased.contains("authentication")
    {
      return .notAuthenticated(message)
    }

    if lowercased.contains("unknown variant")
      || lowercased.contains("method not found")
      || lowercased.contains("account/ratelimits/read")
    {
      return .unsupportedRuntime(message)
    }

    return .serverError(message)
  }

  private func appendStandardError(_ data: Data) {
    let maximumBytes = 16 * 1024
    stderrBuffer.append(data)
    if stderrBuffer.count > maximumBytes {
      stderrBuffer.removeFirst(stderrBuffer.count - maximumBytes)
    }
  }

  private func standardErrorText() -> String {
    String(data: stderrBuffer, encoding: .utf8)?
      .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
  }

  private func finish(with result: Result<UsageSnapshot, any Error>) {
    stateLock.lock()
    guard let continuation else {
      stateLock.unlock()
      return
    }
    self.continuation = nil
    let timeoutTask = self.timeoutTask
    self.timeoutTask = nil
    stateLock.unlock()

    timeoutTask?.cancel()
    standardOutput.fileHandleForReading.readabilityHandler = nil
    standardError.fileHandleForReading.readabilityHandler = nil
    if process.isRunning {
      process.terminate()
    }
    continuation.resume(with: result)
  }
}
