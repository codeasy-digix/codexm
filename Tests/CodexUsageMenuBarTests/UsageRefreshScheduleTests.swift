import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Usage refresh schedule")
struct UsageRefreshScheduleTests {
  @Test("Offers the requested default-account refresh intervals")
  func offersRequestedIntervals() {
    #expect(
      SystemDefaultRefreshInterval.allCases.map(\.rawValue)
        == [30, 60, 300, 600, 1800, 3600]
    )
    #expect(
      SystemDefaultRefreshInterval.allCases.map(\.title)
        == [L10n.text("duration.seconds", 30), L10n.text("duration.minutes", 1),
          L10n.text("duration.minutes", 5), L10n.text("duration.minutes", 10),
          L10n.text("duration.minutes", 30), L10n.text("duration.hours", 1)]
    )
  }

  @Test("Uses one minute when no device preference exists")
  func usesDefaultInterval() throws {
    let suiteName = "UsageRefreshScheduleTests.default"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }

    #expect(UsageRefreshSchedule(defaults: defaults).systemDefaultInterval == .minute1)
  }

  @Test("Persists a device-specific selection")
  func persistsSelection() throws {
    let suiteName = "UsageRefreshScheduleTests.persistence"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }
    let schedule = UsageRefreshSchedule(defaults: defaults)

    schedule.setSystemDefaultInterval(.minutes30)

    #expect(UsageRefreshSchedule(defaults: defaults).systemDefaultInterval == .minutes30)
  }

  @Test("Falls back to one minute for an unsupported stored value")
  func rejectsUnsupportedValue() throws {
    let suiteName = "UsageRefreshScheduleTests.unsupported"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }
    defaults.set(10, forKey: UsageRefreshSchedule.systemDefaultIntervalKey)

    #expect(UsageRefreshSchedule(defaults: defaults).systemDefaultInterval == .minute1)
  }
}
