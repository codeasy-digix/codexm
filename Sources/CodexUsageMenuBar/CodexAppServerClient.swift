import Foundation
import OSLog

final class CodexAppServerClient: @unchecked Sendable {
  let timeout: Duration
  let environment: [String: String]

  private let sessionLock = NSLock()
  private var sessions: [String: AppServerSession] = [:]

  init(
    timeout: Duration = .seconds(15),
    environment: [String: String] = ProcessInfo.processInfo.environment
  ) {
    self.timeout = timeout
    self.environment = environment
  }

  deinit {
    shutdown()
  }

  func fetchUsage(codexURL: URL) async throws -> UsageSnapshot {
    try await fetchUsage(codexURL: codexURL, environmentOverride: nil)
  }

  func fetchUsage(
    codexURL: URL,
    environmentOverride: [String: String]?
  ) async throws -> UsageSnapshot {
    let processEnvironment = Self.processEnvironment(
      for: codexURL,
      base: environmentOverride ?? environment
    )
    let sessionID = processEnvironment["CODEX_HOME"] ?? codexURL.standardizedFileURL.path
    return try await fetchUsage(
      sessionID: sessionID,
      diagnosticLabel: "account",
      identityRevision: nil,
      codexURL: codexURL,
      environmentOverride: environmentOverride
    )
  }

  func fetchUsage(
    sessionID: String,
    diagnosticLabel: String,
    identityRevision: String?,
    codexURL: URL,
    environmentOverride: [String: String]?,
    onUpdate: (@Sendable (UsageSnapshot) -> Void)? = nil
  ) async throws -> UsageSnapshot {
    let processEnvironment = Self.processEnvironment(
      for: codexURL,
      base: environmentOverride ?? environment
    )
    let configuration = AppServerSession.Configuration(
      codexURL: codexURL,
      environment: processEnvironment,
      identityRevision: identityRevision
    )
    let session = session(
      for: sessionID,
      diagnosticLabel: diagnosticLabel,
      configuration: configuration
    )
    return try await session.execute(onUpdate: onUpdate)
  }

  func invalidateSession(sessionID: String) {
    let session = sessionLock.withLock { sessions.removeValue(forKey: sessionID) }
    session?.shutdown()
  }

  func shutdown() {
    let activeSessions = sessionLock.withLock {
      let activeSessions = Array(sessions.values)
      sessions.removeAll()
      return activeSessions
    }
    activeSessions.forEach { $0.shutdown() }
  }

  private func session(
    for sessionID: String,
    diagnosticLabel: String,
    configuration: AppServerSession.Configuration
  ) -> AppServerSession {
    var obsoleteSession: AppServerSession?
    let selectedSession = sessionLock.withLock { () -> AppServerSession in
      if let existing = sessions[sessionID], existing.configuration == configuration {
        return existing
      }

      obsoleteSession = sessions.removeValue(forKey: sessionID)
      let identity = UUID()
      let newSession = AppServerSession(
        identity: identity,
        configuration: configuration,
        timeout: timeout,
        diagnosticLabel: diagnosticLabel,
        onInvalidated: { [weak self] in
          self?.removeSession(sessionID: sessionID, identity: identity)
        }
      )
      sessions[sessionID] = newSession
      return newSession
    }
    obsoleteSession?.shutdown()
    return selectedSession
  }

  private func removeSession(sessionID: String, identity: UUID) {
    sessionLock.withLock {
      guard sessions[sessionID]?.identity == identity else { return }
      sessions.removeValue(forKey: sessionID)
    }
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
  struct Configuration: Equatable, Sendable {
    let codexURL: URL
    let environment: [String: String]
    let identityRevision: String?
  }

  private enum Phase {
    case idle
    case initializing
    case ready
    case stopped
  }

  private enum ActiveRequest {
    case account(Int)
    case rateLimits(Int)

    var identifier: Int {
      switch self {
      case .account(let identifier), .rateLimits(let identifier): return identifier
      }
    }
  }

  private struct Waiter {
    let continuation: CheckedContinuation<UsageSnapshot, any Error>
  }

  static let logger = Logger(
    subsystem: "org.codeasy.CodexUsage",
    category: "UsageRefresh"
  )

  let identity: UUID
  let configuration: Configuration

  private let timeout: Duration
  private let diagnosticLabel: String
  private let onInvalidated: @Sendable () -> Void
  private let parsingQueue: DispatchQueue

  private var phase = Phase.idle
  private var process: Process?
  private var standardInput: Pipe?
  private var standardOutput: Pipe?
  private var standardError: Pipe?
  private var stdoutBuffer = Data()
  private var stderrBuffer = Data()
  private var account: CodexAccount?
  private var lastSnapshot: UsageSnapshot?
  private var waiters: [UUID: Waiter] = [:]
  private var activeRequest: ActiveRequest?
  private var nextRequestID = 2
  private var timeoutTask: Task<Void, Never>?
  private var timeoutGeneration: UUID?
  private var updateHandler: (@Sendable (UsageSnapshot) -> Void)?

  init(
    identity: UUID,
    configuration: Configuration,
    timeout: Duration,
    diagnosticLabel: String,
    onInvalidated: @escaping @Sendable () -> Void
  ) {
    self.identity = identity
    self.configuration = configuration
    self.timeout = timeout
    self.diagnosticLabel = diagnosticLabel
    self.onInvalidated = onInvalidated
    self.parsingQueue = DispatchQueue(
      label: "org.codeasy.CodexUsage.app-server.\(identity.uuidString)"
    )
  }

  func execute(
    onUpdate: (@Sendable (UsageSnapshot) -> Void)?
  ) async throws -> UsageSnapshot {
    let waiterID = UUID()
    return try await withTaskCancellationHandler {
      try await withCheckedThrowingContinuation { continuation in
        parsingQueue.async { [self] in
          register(waiterID: waiterID, continuation: continuation, onUpdate: onUpdate)
        }
      }
    } onCancel: {
      self.cancel(waiterID: waiterID)
    }
  }

  func shutdown() {
    parsingQueue.sync { [self] in
      stop(with: CancellationError(), terminateProcess: true, notifyClient: false)
    }
  }

  private func register(
    waiterID: UUID,
    continuation: CheckedContinuation<UsageSnapshot, any Error>,
    onUpdate: (@Sendable (UsageSnapshot) -> Void)?
  ) {
    guard phase != .stopped else {
      continuation.resume(throwing: CodexUsageError.serverError("Codex 연결이 종료되었습니다."))
      return
    }

    waiters[waiterID] = Waiter(continuation: continuation)
    if let onUpdate {
      updateHandler = onUpdate
    }

    switch phase {
    case .idle:
      startProcess()
    case .ready:
      startQueryIfNeeded()
    case .initializing, .stopped:
      break
    }
  }

  private func cancel(waiterID: UUID) {
    parsingQueue.async { [self] in
      guard let waiter = waiters.removeValue(forKey: waiterID) else { return }
      waiter.continuation.resume(throwing: CancellationError())
    }
  }

  private func startProcess() {
    guard phase == .idle else { return }

    let process = Process()
    let standardInput = Pipe()
    let standardOutput = Pipe()
    let standardError = Pipe()
    self.process = process
    self.standardInput = standardInput
    self.standardOutput = standardOutput
    self.standardError = standardError
    phase = .initializing

    process.executableURL = configuration.codexURL
    process.arguments = ["app-server", "--listen", "stdio://"]
    process.environment = configuration.environment
    process.currentDirectoryURL =
      configuration.environment["CODEX_HOME"].map {
        URL(fileURLWithPath: $0, isDirectory: true)
      } ?? FileManager.default.homeDirectoryForCurrentUser
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
      self.parsingQueue.async { [self] in
        guard phase != .stopped else { return }
        let message = standardErrorText()
        let error = CodexUsageError.serverError(
          message.isEmpty
            ? "Codex app-server가 종료되었습니다 (코드 \(process.terminationStatus))."
            : message
        )
        stop(with: error, terminateProcess: false, notifyClient: true)
      }
    }

    do {
      try process.run()
      Self.logger.info(
        "Persistent app-server started for \(self.diagnosticLabel, privacy: .public) account"
      )
      try send([
        "id": 1,
        "method": "initialize",
        "params": [
          "clientInfo": [
            "name": "codex_usage_menubar",
            "title": "Codex Usage",
            "version": "1.5.4",
          ],
          "capabilities": ["experimentalApi": true],
        ],
      ])
      scheduleTimeout()
    } catch {
      stop(
        with: CodexUsageError.launchFailed(error.localizedDescription),
        terminateProcess: true,
        notifyClient: true
      )
    }
  }

  private func startQueryIfNeeded() {
    guard phase == .ready, activeRequest == nil, !waiters.isEmpty else { return }
    let identifier = allocateRequestID()
    activeRequest = .account(identifier)
    do {
      try send([
        "id": identifier,
        "method": "account/read",
        "params": ["refreshToken": false],
      ])
      scheduleTimeout()
    } catch {
      stop(
        with: CodexUsageError.serverError(error.localizedDescription),
        terminateProcess: true,
        notifyClient: true
      )
    }
  }

  private func requestRateLimits() {
    let identifier = allocateRequestID()
    activeRequest = .rateLimits(identifier)
    do {
      try send([
        "id": identifier,
        "method": "account/rateLimits/read",
        "params": NSNull(),
      ])
      scheduleTimeout()
    } catch {
      stop(
        with: CodexUsageError.serverError(error.localizedDescription),
        terminateProcess: true,
        notifyClient: true
      )
    }
  }

  private func allocateRequestID() -> Int {
    defer { nextRequestID += 1 }
    return nextRequestID
  }

  private func send(_ object: [String: Any]) throws {
    guard let standardInput else {
      throw CodexUsageError.serverError("Codex 입력 연결을 사용할 수 없습니다.")
    }
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

    if let method = json["method"] as? String {
      if method == "account/rateLimits/updated", let params = json["params"] {
        handleRateLimitsUpdate(params)
      }
      return
    }

    guard let identifier = (json["id"] as? NSNumber)?.intValue else { return }

    if identifier == 1, phase == .initializing {
      if let error = json["error"] as? [String: Any] {
        let message = error["message"] as? String ?? ""
        stop(
          with: classifyServerError(message),
          terminateProcess: true,
          notifyClient: true
        )
        return
      }

      do {
        try send(["method": "initialized", "params": [:]])
        phase = .ready
        cancelTimeout()
        startQueryIfNeeded()
      } catch {
        stop(
          with: CodexUsageError.serverError(error.localizedDescription),
          terminateProcess: true,
          notifyClient: true
        )
      }
      return
    }

    guard let activeRequest, activeRequest.identifier == identifier else { return }

    if let error = json["error"] as? [String: Any] {
      let message = error["message"] as? String ?? ""
      switch activeRequest {
      case .account:
        account = nil
        requestRateLimits()
      case .rateLimits:
        stop(
          with: classifyServerError(message),
          terminateProcess: true,
          notifyClient: true
        )
      }
      return
    }

    switch activeRequest {
    case .account:
      account = nil
      if let result = json["result"],
        let resultData = try? JSONSerialization.data(withJSONObject: result),
        let response = try? JSONDecoder().decode(GetAccountResponse.self, from: resultData)
      {
        account = response.account
      }
      requestRateLimits()
    case .rateLimits:
      guard let result = json["result"] else {
        stop(
          with: CodexUsageError.invalidResponse,
          terminateProcess: true,
          notifyClient: true
        )
        return
      }
      completeRateLimits(result)
    }
  }

  private func completeRateLimits(_ result: Any) {
    do {
      let resultData = try JSONSerialization.data(withJSONObject: result)
      let response = try JSONDecoder().decode(GetAccountRateLimitsResponse.self, from: resultData)
      let snapshot = mergedWithPrevious(try response.usageSnapshot(account: account))
      lastSnapshot = snapshot
      completeQuery(with: .success(snapshot))
    } catch let error as CodexUsageError {
      stop(with: error, terminateProcess: true, notifyClient: true)
    } catch {
      stop(
        with: CodexUsageError.invalidResponse,
        terminateProcess: true,
        notifyClient: true
      )
    }
  }

  private func handleRateLimitsUpdate(_ params: Any) {
    guard phase == .ready else { return }
    do {
      let data = try JSONSerialization.data(withJSONObject: params)
      let response = try JSONDecoder().decode(GetAccountRateLimitsResponse.self, from: data)
      let snapshot = mergedWithPrevious(try response.usageSnapshot(account: account))
      lastSnapshot = snapshot
      updateHandler?(snapshot)
      Self.logger.debug(
        "Rate-limit notification received for \(self.diagnosticLabel, privacy: .public) account"
      )
    } catch {
      Self.logger.debug(
        "Ignored malformed rate-limit notification for \(self.diagnosticLabel, privacy: .public) account"
      )
    }
  }

  private func mergedWithPrevious(_ snapshot: UsageSnapshot) -> UsageSnapshot {
    guard let previous = lastSnapshot else { return snapshot }
    return UsageSnapshot(
      fiveHourLimit: snapshot.fiveHourLimit,
      weeklyLimit: snapshot.weeklyLimit,
      planType: snapshot.planType ?? previous.planType,
      availableResetCredits: snapshot.availableResetCredits ?? previous.availableResetCredits,
      resetCreditDetails: snapshot.resetCreditDetails ?? previous.resetCreditDetails,
      accountEmail: snapshot.accountEmail ?? previous.accountEmail,
      fetchedAt: snapshot.fetchedAt
    )
  }

  private func completeQuery(with result: Result<UsageSnapshot, any Error>) {
    cancelTimeout()
    activeRequest = nil
    let completedWaiters = Array(waiters.values)
    waiters.removeAll()

    switch result {
    case .success:
      Self.logger.debug(
        "Usage refresh completed for \(self.diagnosticLabel, privacy: .public) account"
      )
    case .failure(let error):
      Self.logger.error(
        "Usage refresh failed for \(self.diagnosticLabel, privacy: .public) account: \(String(describing: type(of: error)), privacy: .public)"
      )
    }

    completedWaiters.forEach { $0.continuation.resume(with: result) }
    startQueryIfNeeded()
  }

  private func scheduleTimeout() {
    cancelTimeout()
    let generation = UUID()
    timeoutGeneration = generation
    timeoutTask = Task { [weak self, timeout] in
      do {
        try await Task.sleep(for: timeout)
      } catch {
        return
      }
      guard let self else { return }
      self.parsingQueue.async { [self] in
        guard timeoutGeneration == generation else { return }
        stop(
          with: CodexUsageError.timedOut,
          terminateProcess: true,
          notifyClient: true
        )
      }
    }
  }

  private func cancelTimeout() {
    timeoutTask?.cancel()
    timeoutTask = nil
    timeoutGeneration = nil
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

  private func stop(
    with error: any Error,
    terminateProcess: Bool,
    notifyClient: Bool
  ) {
    guard phase != .stopped else { return }
    phase = .stopped
    cancelTimeout()
    activeRequest = nil

    let completedWaiters = Array(waiters.values)
    waiters.removeAll()
    completedWaiters.forEach { $0.continuation.resume(throwing: error) }

    standardOutput?.fileHandleForReading.readabilityHandler = nil
    standardError?.fileHandleForReading.readabilityHandler = nil
    if terminateProcess, process?.isRunning == true {
      process?.terminate()
    }
    process?.terminationHandler = nil

    Self.logger.info(
      "Persistent app-server stopped for \(self.diagnosticLabel, privacy: .public) account"
    )
    if notifyClient {
      onInvalidated()
    }
  }
}
