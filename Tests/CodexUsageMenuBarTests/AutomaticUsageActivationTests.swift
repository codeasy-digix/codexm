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

  @Test("Retries a failed request after fifteen minutes")
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
