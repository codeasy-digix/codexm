import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Usage refresh schedule")
struct UsageRefreshScheduleTests {
  @Test("Keeps the standard fifteen-minute interval without a device override")
  func usesStandardInterval() throws {
    let suiteName = "UsageRefreshScheduleTests.standard"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }

    #expect(
      UsageRefreshSchedule(defaults: defaults).systemDefaultIntervalSeconds
        == UsageRefreshSchedule.standardIntervalSeconds
    )
  }

  @Test("Uses a one-minute device override for the system-default account")
  func usesDeviceOverride() throws {
    let suiteName = "UsageRefreshScheduleTests.override"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }
    defaults.set(60, forKey: UsageRefreshSchedule.systemDefaultIntervalKey)

    #expect(UsageRefreshSchedule(defaults: defaults).systemDefaultIntervalSeconds == 60)
  }

  @Test("Does not allow a device override shorter than one minute")
  func clampsTooShortOverride() throws {
    let suiteName = "UsageRefreshScheduleTests.minimum"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }
    defaults.set(10, forKey: UsageRefreshSchedule.systemDefaultIntervalKey)

    #expect(
      UsageRefreshSchedule(defaults: defaults).systemDefaultIntervalSeconds
        == UsageRefreshSchedule.minimumSystemDefaultIntervalSeconds
    )
  }
}
