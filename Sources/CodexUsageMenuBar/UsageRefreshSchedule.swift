import Foundation

struct UsageRefreshSchedule: Equatable, Sendable {
  static let systemDefaultIntervalKey = "SystemDefaultUsageRefreshIntervalSeconds"
  static let standardIntervalSeconds = 15 * 60
  static let minimumSystemDefaultIntervalSeconds = 60

  let systemDefaultIntervalSeconds: Int

  init(defaults: UserDefaults = .standard) {
    let configuredSeconds = (defaults.object(forKey: Self.systemDefaultIntervalKey) as? NSNumber)?
      .intValue
    systemDefaultIntervalSeconds = max(
      Self.minimumSystemDefaultIntervalSeconds,
      configuredSeconds ?? Self.standardIntervalSeconds
    )
  }

  var systemDefaultInterval: Duration {
    .seconds(systemDefaultIntervalSeconds)
  }
}
