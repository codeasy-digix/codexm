import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Automatic five-hour activation")
@MainActor
struct AutomaticUsageActivationTests {
  @Test("Uses an opt-in setting and persists it")
  func persistsOptInSetting() throws {
    let suiteName = "AutomaticUsageActivationTests.opt-in"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }

    let store = AutomaticUsageScheduleStore(defaults: defaults)
    #expect(store.isEnabled == false)

    store.setEnabled(true)

    #expect(AutomaticUsageScheduleStore(defaults: defaults).isEnabled)
  }

  @Test("Schedules five hours from the successful request start")
  func schedulesFromSuccessfulRequest() throws {
    let suiteName = "AutomaticUsageActivationTests.success"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }
    let store = AutomaticUsageScheduleStore(defaults: defaults)
    let startedAt = Date(timeIntervalSince1970: 2_000_000_000)

    store.recordAttempt(accountID: "account-a", at: startedAt, expression: "10 * 2")
    store.recordSuccess(accountID: "account-a", requestStartedAt: startedAt)

    let restored = AutomaticUsageScheduleStore(defaults: defaults).entry(for: "account-a")
    #expect(restored.lastExpression == "10 * 2")
    #expect(
      restored.nextAttemptDate()
        == startedAt.addingTimeInterval(AutomaticUsageSchedulePolicy.activationInterval)
    )
  }

  @Test("Retries a failed request after thirty minutes")
  func retriesFailedRequest() throws {
    let suiteName = "AutomaticUsageActivationTests.retry"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }
    let store = AutomaticUsageScheduleStore(defaults: defaults)
    let attemptAt = Date(timeIntervalSince1970: 2_000_000_000)

    store.recordAttempt(accountID: "account-a", at: attemptAt, expression: "14 / 2")

    #expect(
      store.entry(for: "account-a").nextAttemptDate()
        == attemptAt.addingTimeInterval(AutomaticUsageSchedulePolicy.retryInterval)
    )
  }

  @Test("Persists a thirty-minute gap between account request starts")
  func persistsGlobalAccountSpacing() throws {
    let suiteName = "AutomaticUsageActivationTests.global-spacing"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }
    let store = AutomaticUsageScheduleStore(defaults: defaults)
    let attemptAt = Date(timeIntervalSince1970: 2_000_000_000)

    #expect(store.nextGlobalAttemptDate() == .distantPast)
    store.recordAttempt(accountID: "account-a", at: attemptAt, expression: "21 / 3")

    let restored = AutomaticUsageScheduleStore(defaults: defaults)
    #expect(
      restored.nextGlobalAttemptDate()
        == attemptAt.addingTimeInterval(AutomaticUsageSchedulePolicy.accountSpacingInterval)
    )
  }

  @Test("Serializes app-server work across different accounts")
  func serializesOperationsAcrossAccounts() async throws {
    let gate = AccountOperationGate()
    let probe = OperationConcurrencyProbe()

    async let first: Void = gate.withPermit(for: "account-a") {
      await probe.enter()
      try await Task.sleep(for: .milliseconds(40))
      await probe.leave()
    }
    async let second: Void = gate.withPermit(for: "account-b") {
      await probe.enter()
      try await Task.sleep(for: .milliseconds(40))
      await probe.leave()
    }

    _ = try await (first, second)
    #expect(await probe.maximumConcurrentCount() == 1)
  }

  @Test("Removes deleted account schedule data")
  func removesDeletedAccount() throws {
    let suiteName = "AutomaticUsageActivationTests.removal"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }
    let store = AutomaticUsageScheduleStore(defaults: defaults)
    let attemptAt = Date(timeIntervalSince1970: 2_000_000_000)
    store.recordAttempt(accountID: "account-a", at: attemptAt, expression: "8 + 3")

    store.remove(accountID: "account-a")

    #expect(store.entry(for: "account-a") == AutomaticUsageScheduleEntry())
  }

  @Test("Generates a different compact arithmetic request")
  func generatesDifferentPrompt() {
    let generator = AutomaticUsagePromptGenerator()
    let previous = generator.makeExpression(excluding: nil)
    let next = generator.makeExpression(excluding: previous)
    let prompt = generator.prompt(for: next)

    #expect(next != previous)
    #expect(prompt.contains(next))
    #expect(prompt.contains("도구를 사용하거나 파일을 읽지 마세요"))
  }
}

private actor OperationConcurrencyProbe {
  private var activeCount = 0
  private var maximumCount = 0

  func enter() {
    activeCount += 1
    maximumCount = max(maximumCount, activeCount)
  }

  func leave() {
    activeCount -= 1
  }

  func maximumConcurrentCount() -> Int {
    maximumCount
  }
}
