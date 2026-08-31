import Foundation

struct AutomaticUsageSchedulePolicy: Sendable {
  static let activationInterval: TimeInterval = 5 * 60 * 60
  static let retryInterval: TimeInterval = 15 * 60
  static let schedulerPollInterval: Duration = .seconds(30)
}

struct AutomaticUsageScheduleEntry: Codable, Equatable, Sendable {
  var lastAttemptAt: Date?
  var lastSuccessAt: Date?
  var lastExpression: String?

  func nextAttemptDate(
    activationInterval: TimeInterval = AutomaticUsageSchedulePolicy.activationInterval,
    retryInterval: TimeInterval = AutomaticUsageSchedulePolicy.retryInterval
  ) -> Date {
    if let lastAttemptAt {
      if let lastSuccessAt {
        if lastAttemptAt > lastSuccessAt {
          return lastAttemptAt.addingTimeInterval(retryInterval)
        }
      } else {
        return lastAttemptAt.addingTimeInterval(retryInterval)
      }
    }

    if let lastSuccessAt {
      return lastSuccessAt.addingTimeInterval(activationInterval)
    }

    return .distantPast
  }
}

@MainActor
final class AutomaticUsageScheduleStore {
  static let enabledKey = "AutomaticFiveHourActivationEnabled"
  static let scheduleKey = "AutomaticFiveHourActivationSchedule"

  private let defaults: UserDefaults
  private let encoder = JSONEncoder()
  private let decoder = JSONDecoder()

  init(defaults: UserDefaults = .standard) {
    self.defaults = defaults
  }

  var isEnabled: Bool {
    defaults.bool(forKey: Self.enabledKey)
  }

  func setEnabled(_ enabled: Bool) {
    defaults.set(enabled, forKey: Self.enabledKey)
  }

  func entry(for accountID: String) -> AutomaticUsageScheduleEntry {
    entries()[accountID] ?? AutomaticUsageScheduleEntry()
  }

  func recordAttempt(accountID: String, at date: Date, expression: String) {
    var values = entries()
    var value = values[accountID] ?? AutomaticUsageScheduleEntry()
    value.lastAttemptAt = date
    value.lastExpression = expression
    values[accountID] = value
    save(values)
  }

  func recordSuccess(accountID: String, requestStartedAt date: Date) {
    var values = entries()
    var value = values[accountID] ?? AutomaticUsageScheduleEntry()
    value.lastSuccessAt = date
    values[accountID] = value
    save(values)
  }

  func remove(accountID: String) {
    var values = entries()
    values.removeValue(forKey: accountID)
    save(values)
  }

  func retain(accountIDs: Set<String>) {
    let retained = entries().filter { accountIDs.contains($0.key) }
    save(retained)
  }

  private func entries() -> [String: AutomaticUsageScheduleEntry] {
    guard let data = defaults.data(forKey: Self.scheduleKey) else { return [:] }
    return (try? decoder.decode([String: AutomaticUsageScheduleEntry].self, from: data)) ?? [:]
  }

  private func save(_ entries: [String: AutomaticUsageScheduleEntry]) {
    guard !entries.isEmpty else {
      defaults.removeObject(forKey: Self.scheduleKey)
      return
    }
    if let data = try? encoder.encode(entries) {
      defaults.set(data, forKey: Self.scheduleKey)
    }
  }
}

struct AutomaticUsagePromptGenerator: Sendable {
  func makeExpression(excluding previousExpression: String?) -> String {
    for _ in 0..<8 {
      let expression = randomExpression()
      if expression != previousExpression {
        return expression
      }
    }

    let fallbackBase = Int.random(in: 10_000...99_999)
    return "\(fallbackBase) * 17 / 13 + 1"
  }

  func prompt(for expression: String) -> String {
    "다음 산술식의 결과만 숫자로 답하세요. 도구를 사용하거나 파일을 읽지 마세요: \(expression)"
  }

  private func randomExpression() -> String {
    let first = Int.random(in: 1_000...99_999)
    let second = Int.random(in: 11...997)
    let divisor = Int.random(in: 2...499)
    let offset = Int.random(in: 1...999)
    return "\(first) * \(second) / \(divisor) + \(offset)"
  }
}

actor AccountOperationGate {
  private var activeAccountIDs = Set<String>()
  private var waiters: [String: [CheckedContinuation<Void, Never>]] = [:]

  func withPermit<Value: Sendable>(
    for accountID: String,
    operation: @Sendable () async throws -> Value
  ) async throws -> Value {
    await acquire(accountID)
    defer { release(accountID) }
    try Task.checkCancellation()
    return try await operation()
  }

  private func acquire(_ accountID: String) async {
    guard activeAccountIDs.contains(accountID) else {
      activeAccountIDs.insert(accountID)
      return
    }

    await withCheckedContinuation { continuation in
      waiters[accountID, default: []].append(continuation)
    }
  }

  private func release(_ accountID: String) {
    guard var accountWaiters = waiters[accountID], !accountWaiters.isEmpty else {
      activeAccountIDs.remove(accountID)
      waiters.removeValue(forKey: accountID)
      return
    }

    let next = accountWaiters.removeFirst()
    if accountWaiters.isEmpty {
      waiters.removeValue(forKey: accountID)
    } else {
      waiters[accountID] = accountWaiters
    }
    next.resume()
  }
}

struct CodexAutomaticUsageClient: Sendable {
  let timeout: Duration
  let model: String
  let effort: String

  init(
    timeout: Duration = .seconds(90),
    model: String = "gpt-5.6-luna",
    effort: String = "low"
  ) {
    self.timeout = timeout
    self.model = model
    self.effort = effort
  }

  func performRequest(
    codexURL: URL,
    environmentOverride: [String: String],
    workingDirectoryURL: URL,
    prompt: String
  ) async throws {
    let session = AutomaticUsageAppServerSession(
      codexURL: codexURL,
      timeout: timeout,
      environment: CodexAppServerClient.processEnvironment(
        for: codexURL,
        base: environmentOverride
      ),
      workingDirectoryURL: workingDirectoryURL,
      model: model,
      effort: effort,
      prompt: prompt
    )
    try await session.execute()
  }
}

private final class AutomaticUsageAppServerSession: @unchecked Sendable {
  private let codexURL: URL
  private let timeout: Duration
  private let environment: [String: String]
  private let workingDirectoryURL: URL
  private let model: String
  private let effort: String
  private let prompt: String
  private let process = Process()
  private let standardInput = Pipe()
  private let standardOutput = Pipe()
  private let standardError = Pipe()
  private let parsingQueue = DispatchQueue(label: "org.codeasy.CodexUsage.automatic-usage")
  private let stateLock = NSLock()

  private var stdoutBuffer = Data()
  private var stderrBuffer = Data()
  private var continuation: CheckedContinuation<Void, any Error>?
  private var timeoutTask: Task<Void, Never>?
  private var threadID: String?
  private var selectedModel: String?
  private var selectedEffort: String?

  init(
    codexURL: URL,
    timeout: Duration,
    environment: [String: String],
    workingDirectoryURL: URL,
    model: String,
    effort: String,
    prompt: String
  ) {
    self.codexURL = codexURL
    self.timeout = timeout
    self.environment = environment
    self.workingDirectoryURL = workingDirectoryURL
    self.model = model
    self.effort = effort
    self.prompt = prompt
  }

  func execute() async throws {
    try await withTaskCancellationHandler {
      try await withCheckedThrowingContinuation { continuation in
        start(continuation: continuation)
      }
    } onCancel: {
      self.finish(with: .failure(CancellationError()))
    }
  }

  private func start(continuation: CheckedContinuation<Void, any Error>) {
    stateLock.lock()
    self.continuation = continuation
    stateLock.unlock()

    process.executableURL = codexURL
    process.arguments = ["app-server", "--listen", "stdio://"]
    process.environment = environment
    process.currentDirectoryURL = workingDirectoryURL
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
        let message = stderr.isEmpty
          ? "Codex 자동 요청이 종료되었습니다 (코드 \(process.terminationStatus))."
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
            "version": "1.5.0",
          ]
        ],
      ])
      timeoutTask = Task { [weak self, timeout] in
        try? await Task.sleep(for: timeout)
        guard !Task.isCancelled else { return }
        self?.finish(
          with: .failure(CodexUsageError.serverError("Codex 자동 요청 시간이 초과되었습니다."))
        )
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

    if let method = json["method"] as? String, method == "turn/completed" {
      handleTurnCompletion(json)
      return
    }

    guard let identifier = (json["id"] as? NSNumber)?.intValue else { return }
    if let error = json["error"] as? [String: Any] {
      let message = error["message"] as? String ?? "Codex 자동 요청에 실패했습니다."
      finish(with: .failure(classifyServerError(message)))
      return
    }

    do {
      switch identifier {
      case 1:
        try send(["method": "initialized", "params": [:]])
        try send([
          "id": 2,
          "method": "model/list",
          "params": ["limit": 100, "includeHidden": false],
        ])
      case 2:
        selectAvailableModel(from: json)
        var parameters: [String: Any] = [
          "cwd": workingDirectoryURL.path,
          "approvalPolicy": "never",
          "sandbox": "read-only",
          "ephemeral": true,
          "serviceName": "codex_usage_automatic_five_hour",
          "developerInstructions":
            "Solve only the supplied arithmetic expression. Never call tools, read files, browse, or modify anything. Return only the numeric result.",
        ]
        if let selectedModel {
          parameters["model"] = selectedModel
        }
        try send([
          "id": 3,
          "method": "thread/start",
          "params": parameters,
        ])
      case 3:
        guard
          let result = json["result"] as? [String: Any],
          let thread = result["thread"] as? [String: Any],
          let threadID = thread["id"] as? String
        else {
          throw CodexUsageError.invalidResponse
        }
        self.threadID = threadID
        var parameters: [String: Any] = [
          "threadId": threadID,
          "input": [["type": "text", "text": prompt]],
          "approvalPolicy": "never",
          "sandboxPolicy": ["type": "readOnly", "networkAccess": false],
          "summary": "none",
        ]
        if let selectedModel {
          parameters["model"] = selectedModel
        }
        if let selectedEffort {
          parameters["effort"] = selectedEffort
        }
        try send([
          "id": 4,
          "method": "turn/start",
          "params": parameters,
        ])
      case 4:
        break
      default:
        break
      }
    } catch {
      finish(with: .failure(error))
    }
  }

  private func selectAvailableModel(from json: [String: Any]) {
    guard
      let result = json["result"] as? [String: Any],
      let data = result["data"] as? [[String: Any]],
      !data.isEmpty
    else {
      selectedModel = nil
      selectedEffort = nil
      return
    }

    let preferred = data.first { modelIdentifier($0) == model }
    let lightweight = data.first { descriptor in
      let identifier = modelIdentifier(descriptor)
      return identifier.contains("luna")
    } ?? data.first { descriptor in
      let identifier = modelIdentifier(descriptor)
      return identifier.contains("mini") || identifier.contains("terra")
    }
    let defaultModel = data.first { ($0["isDefault"] as? Bool) == true }
    let selected = preferred ?? lightweight ?? defaultModel ?? data[0]

    selectedModel = modelIdentifier(selected)
    let supportedEfforts = (selected["supportedReasoningEfforts"] as? [[String: Any]]) ?? []
    let advertisedEfforts = supportedEfforts.compactMap { $0["reasoningEffort"] as? String }
    if advertisedEfforts.contains(effort) {
      selectedEffort = effort
    } else {
      selectedEffort = selected["defaultReasoningEffort"] as? String ?? advertisedEfforts.first
    }
  }

  private func modelIdentifier(_ descriptor: [String: Any]) -> String {
    (descriptor["model"] as? String ?? descriptor["id"] as? String ?? "").lowercased()
  }

  private func handleTurnCompletion(_ json: [String: Any]) {
    guard
      let params = json["params"] as? [String: Any],
      let completedThreadID = params["threadId"] as? String,
      completedThreadID == threadID,
      let turn = params["turn"] as? [String: Any],
      let status = turn["status"] as? String
    else {
      return
    }

    if status == "completed" {
      finish(with: .success(()))
      return
    }

    let error = turn["error"] as? [String: Any]
    let message = error?["message"] as? String ?? "Codex 자동 요청 상태: \(status)"
    finish(with: .failure(classifyServerError(message)))
  }

  private func classifyServerError(_ message: String) -> CodexUsageError {
    let lowercased = message.lowercased()
    if lowercased.contains("not logged")
      || lowercased.contains("unauthorized")
      || lowercased.contains("authentication")
    {
      return .notAuthenticated(message)
    }
    return .serverError(message)
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
