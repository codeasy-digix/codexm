import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Codex rate limit parsing")
struct UsageModelsTests {
  @Test("Selects the seven-day window regardless of primary/secondary field")
  func selectsWeeklyWindowByDuration() throws {
    let data = Data(
      """
      {
        "rateLimits": {
          "limitId": "codex",
          "planType": "plus",
          "primary": {"usedPercent": 31, "windowDurationMins": 300, "resetsAt": 1700000000},
          "secondary": {"usedPercent": 72, "windowDurationMins": 10080, "resetsAt": 1700100000}
        },
        "rateLimitsByLimitId": null,
        "rateLimitResetCredits": {"availableCount": 2}
      }
      """.utf8
    )

    let response = try JSONDecoder().decode(GetAccountRateLimitsResponse.self, from: data)
    let snapshot = try response.usageSnapshot(fetchedAt: Date(timeIntervalSince1970: 100))

    #expect(snapshot.usedPercent == 72)
    #expect(snapshot.remainingPercent == 28)
    #expect(snapshot.windowDurationMinutes == 10_080)
    #expect(snapshot.planDisplayName == "Plus")
    #expect(snapshot.availableResetCredits == 2)
  }

  @Test("Handles the current single primary weekly payload")
  func handlesSinglePrimaryWeeklyWindow() throws {
    let data = Data(
      """
      {
        "rateLimits": {
          "limitId": "codex",
          "planType": "pro",
          "primary": {"usedPercent": 82, "windowDurationMins": 10080, "resetsAt": 1784690412},
          "secondary": null
        },
        "rateLimitsByLimitId": {
          "codex": {
            "limitId": "codex",
            "planType": "pro",
            "primary": {"usedPercent": 82, "windowDurationMins": 10080, "resetsAt": 1784690412},
            "secondary": null
          }
        },
        "rateLimitResetCredits": null
      }
      """.utf8
    )

    let response = try JSONDecoder().decode(GetAccountRateLimitsResponse.self, from: data)
    let snapshot = try response.usageSnapshot()

    #expect(snapshot.usedPercent == 82)
    #expect(snapshot.remainingPercent == 18)
    #expect(snapshot.planDisplayName == "Pro")
  }

  @Test("Clamps unexpected percentage values")
  func clampsPercentages() throws {
    let response = GetAccountRateLimitsResponse(
      rateLimits: RateLimitSnapshot(
        limitId: "codex",
        planType: nil,
        primary: RateLimitWindow(usedPercent: 140, windowDurationMins: 10_080, resetsAt: nil),
        secondary: nil
      ),
      rateLimitsByLimitId: nil,
      rateLimitResetCredits: nil
    )

    let snapshot = try response.usageSnapshot()
    #expect(snapshot.usedPercent == 100)
    #expect(snapshot.remainingPercent == 0)
  }
}
