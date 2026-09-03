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
        == ["30초", "1분", "5분", "10분", "30분", "1시간"]
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
