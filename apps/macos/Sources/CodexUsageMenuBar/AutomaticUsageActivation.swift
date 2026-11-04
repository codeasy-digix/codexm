import Foundation

struct AutomaticUsageSchedulePolicy: Sendable {
  static let activationInterval: TimeInterval = 5 * 60 * 60
  static let retryInterval: TimeInterval = 30 * 60
  static let accountSpacingInterval: TimeInterval = 30 * 60
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
  static let lastGlobalAttemptKey = "AutomaticFiveHourActivationLastGlobalAttemptAt"

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
    defaults.set(date, forKey: Self.lastGlobalAttemptKey)
  }

  func nextGlobalAttemptDate(
    spacingInterval: TimeInterval = AutomaticUsageSchedulePolicy.accountSpacingInterval
  ) -> Date {
    guard let lastAttemptAt = defaults.object(forKey: Self.lastGlobalAttemptKey) as? Date else {
      return .distantPast
    }
    return lastAttemptAt.addingTimeInterval(spacingInterval)
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

  /// Move a former UI identifier onto the shared login identity once. A named
  /// alias and the default role must not schedule two requests for one login.
  func migrate(from oldID: String, to identity: String) {
    guard oldID != identity else { return }
    var values = entries()
    guard let previous = values.removeValue(forKey: oldID) else { return }
    if let current = values[identity] {
      if previous.nextAttemptDate() > current.nextAttemptDate() { values[identity] = previous }
    } else { values[identity] = previous }
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
    "Reply with only the numeric result of this arithmetic expression. Do not use tools or read files: \(expression)"
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
  private var isActive = false
  private var waiters: [CheckedContinuation<Void, Never>] = []

  func withPermit<Value: Sendable>(
    for _: String,
    operation: @Sendable () async throws -> Value
  ) async throws -> Value {
    await acquire()
    defer { release() }
    try Task.checkCancellation()
    return try await operation()
  }

  private func acquire() async {
    guard isActive else {
      isActive = true
      return
    }

    await withCheckedContinuation { continuation in
      waiters.append(continuation)
    }
  }

  private func release() {
    guard !waiters.isEmpty else {
      isActive = false
      return
    }

    let next = waiters.removeFirst()
    next.resume()
  }
}
