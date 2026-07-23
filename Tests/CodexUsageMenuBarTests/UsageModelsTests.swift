import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Codex rate limit parsing")
struct UsageModelsTests {
  @Test("Reads the signed-in ChatGPT account email separately from its plan")
  func decodesAccountEmail() throws {
    let data = Data(
      """
      {
        "account": {
          "type": "chatgpt",
          "email": "owner@example.com",
          "planType": "team"
        },
        "requiresOpenaiAuth": true
      }
      """.utf8
    )

    let response = try JSONDecoder().decode(GetAccountResponse.self, from: data)

    #expect(response.account?.email == "owner@example.com")
    #expect(response.account?.planType == "team")
  }

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

    let accountSnapshot = try response.usageSnapshot(accountEmail: "owner@example.com")
    #expect(accountSnapshot.accountEmail == "owner@example.com")
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

  @Test("Formats the time remaining until the weekly limit resets")
  func formatsResetCountdown() {
    let now = Date(timeIntervalSince1970: 1_000_000)

    #expect(
      snapshot(resetAfter: 2 * 86_400 + 3 * 3_600, from: now)
        .resetCountdown(relativeTo: now) == "2일 3시간"
    )
    #expect(
      snapshot(resetAfter: 5 * 3_600 + 25 * 60, from: now)
        .resetCountdown(relativeTo: now) == "5시간 25분"
    )
    #expect(snapshot(resetAfter: 45, from: now).resetCountdown(relativeTo: now) == "1분")
    #expect(snapshot(resetAfter: -1, from: now).resetCountdown(relativeTo: now) == "곧 초기화")
  }

  private func snapshot(resetAfter seconds: TimeInterval, from date: Date) -> UsageSnapshot {
    UsageSnapshot(
      usedPercent: 72,
      windowDurationMinutes: 10_080,
      resetsAt: date.addingTimeInterval(seconds),
      planType: "plus",
      availableResetCredits: nil,
      accountEmail: nil,
      fetchedAt: date
    )
  }
}
