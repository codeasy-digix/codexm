import Foundation

struct RateLimitWindow: Codable, Equatable, Sendable {
  let usedPercent: Int
  let windowDurationMins: Int64?
  let resetsAt: Int64?
}

struct RateLimitSnapshot: Codable, Equatable, Sendable {
  let limitId: String?
  let planType: String?
  let primary: RateLimitWindow?
  let secondary: RateLimitWindow?
}

struct RateLimitResetCredits: Codable, Equatable, Sendable {
  let availableCount: Int64
}

struct GetAccountRateLimitsResponse: Codable, Equatable, Sendable {
  let rateLimits: RateLimitSnapshot
  let rateLimitsByLimitId: [String: RateLimitSnapshot]?
  let rateLimitResetCredits: RateLimitResetCredits?

  var codexRateLimits: RateLimitSnapshot {
    if let exactMatch = rateLimitsByLimitId?["codex"] {
      return exactMatch
    }

    if let matchingValue = rateLimitsByLimitId?.values.first(where: { $0.limitId == "codex" }) {
      return matchingValue
    }

    return rateLimits
  }

  /// Codex has historically exposed a short rolling window and a weekly window in
  /// either primary/secondary order. Choose the window whose duration is closest
  /// to seven days instead of relying on the field name.
  var weeklyWindow: RateLimitWindow? {
    let limits = codexRateLimits
    let windows = [limits.primary, limits.secondary].compactMap { $0 }
    let weekInMinutes: Int64 = 7 * 24 * 60

    let dayOrLonger = windows.filter { ($0.windowDurationMins ?? 0) >= 24 * 60 }
    if let closestToWeek = dayOrLonger.min(by: {
      abs(($0.windowDurationMins ?? weekInMinutes) - weekInMinutes)
        < abs(($1.windowDurationMins ?? weekInMinutes) - weekInMinutes)
    }) {
      return closestToWeek
    }

    // Older payloads may omit durations. In that shape secondary was the
    // long window, while a lone primary window is commonly the weekly limit.
    return limits.secondary ?? limits.primary
  }

  func usageSnapshot(fetchedAt: Date = Date()) throws -> UsageSnapshot {
    guard let window = weeklyWindow else {
      throw CodexUsageError.noWeeklyLimit
    }

    return UsageSnapshot(
      usedPercent: min(max(window.usedPercent, 0), 100),
      windowDurationMinutes: window.windowDurationMins,
      resetsAt: window.resetsAt.map { Date(timeIntervalSince1970: TimeInterval($0)) },
      planType: codexRateLimits.planType,
      availableResetCredits: rateLimitResetCredits?.availableCount,
      fetchedAt: fetchedAt
    )
  }
}

struct UsageSnapshot: Equatable, Sendable {
  let usedPercent: Int
  let windowDurationMinutes: Int64?
  let resetsAt: Date?
  let planType: String?
  let availableResetCredits: Int64?
  let fetchedAt: Date

  var remainingPercent: Int { max(0, 100 - usedPercent) }

  var planDisplayName: String? {
    guard let planType else { return nil }
    switch planType {
    case "plus": return "Plus"
    case "pro": return "Pro"
    case "prolite": return "Pro"
    case "team": return "Team"
    case "business", "self_serve_business_usage_based": return "Business"
    case "enterprise", "enterprise_cbp_usage_based": return "Enterprise"
    case "edu": return "Edu"
    case "free": return "Free"
    case "go": return "Go"
    default: return nil
    }
  }
}

enum CodexUsageError: LocalizedError, Equatable {
  case launchFailed(String)
  case notAuthenticated(String)
  case unsupportedRuntime(String)
  case serverError(String)
  case invalidResponse
  case noWeeklyLimit
  case timedOut

  var errorDescription: String? {
    switch self {
    case .launchFailed(let message):
      return "앱에 포함된 Codex 런타임을 실행하지 못했습니다. \(message)"
    case .notAuthenticated:
      return "Codex 계정 연결이 필요합니다."
    case .unsupportedRuntime:
      return "앱에 포함된 Codex 런타임에서 사용량 조회를 지원하지 않습니다."
    case .serverError(let message):
      return message.isEmpty ? "Codex에서 사용량을 가져오지 못했습니다." : message
    case .invalidResponse:
      return "Codex가 예상하지 못한 응답을 반환했습니다."
    case .noWeeklyLimit:
      return "계정에서 주간 사용량 한도를 찾지 못했습니다."
    case .timedOut:
      return "Codex 사용량 조회 시간이 초과되었습니다."
    }
  }
}
