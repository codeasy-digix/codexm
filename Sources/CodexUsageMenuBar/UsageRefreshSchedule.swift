import Foundation

enum SystemDefaultRefreshInterval: Int, CaseIterable, Identifiable, Sendable {
  case seconds30 = 30
  case minute1 = 60
  case minutes5 = 300
  case minutes10 = 600
  case minutes30 = 1_800
  case hour1 = 3_600

  var id: Int { rawValue }

  var title: String {
    switch self {
    case .seconds30: return "30초"
    case .minute1: return "1분"
    case .minutes5: return "5분"
    case .minutes10: return "10분"
    case .minutes30: return "30분"
    case .hour1: return "1시간"
    }
  }

  var duration: Duration {
    .seconds(rawValue)
  }
}

struct UsageRefreshSchedule {
  static let systemDefaultIntervalKey = "SystemDefaultUsageRefreshIntervalSeconds"
  static let managedAccountIntervalSeconds = 15 * 60
  static let defaultSystemDefaultInterval = SystemDefaultRefreshInterval.minute1

  private let defaults: UserDefaults

  init(defaults: UserDefaults = .standard) {
    self.defaults = defaults
  }

  var systemDefaultInterval: SystemDefaultRefreshInterval {
    guard
      let rawValue = (defaults.object(forKey: Self.systemDefaultIntervalKey) as? NSNumber)?.intValue,
      let interval = SystemDefaultRefreshInterval(rawValue: rawValue)
    else {
      return Self.defaultSystemDefaultInterval
    }
    return interval
  }

  func setSystemDefaultInterval(_ interval: SystemDefaultRefreshInterval) {
    defaults.set(interval.rawValue, forKey: Self.systemDefaultIntervalKey)
  }
}
