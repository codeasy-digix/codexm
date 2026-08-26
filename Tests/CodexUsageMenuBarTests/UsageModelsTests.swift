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

  @Test("Keeps both the five-hour and seven-day windows")
  func selectsBothWindowsByDuration() throws {
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

    #expect(snapshot.fiveHourLimit?.usedPercent == 31)
    #expect(snapshot.fiveHourLimit?.remainingPercent == 69)
    #expect(snapshot.fiveHourLimit?.windowDurationMinutes == 300)
    #expect(snapshot.weeklyLimit?.usedPercent == 72)
    #expect(snapshot.weeklyLimit?.remainingPercent == 28)
    #expect(snapshot.weeklyLimit?.windowDurationMinutes == 10_080)
    #expect(snapshot.planDisplayName == "Plus")
    #expect(snapshot.availableResetCredits == 2)

    let accountSnapshot = try response.usageSnapshot(
      account: CodexAccount(type: "chatgpt", email: "owner@example.com", planType: "team")
    )
    #expect(accountSnapshot.accountEmail == "owner@example.com")
    #expect(accountSnapshot.planDisplayName == "Plus")
  }

  @Test("Identifies both windows when primary and secondary are reversed")
  func selectsBothWindowsWhenReversed() throws {
    let response = GetAccountRateLimitsResponse(
      rateLimits: RateLimitSnapshot(
        limitId: "codex",
        planType: "plus",
        primary: RateLimitWindow(
          usedPercent: 72,
          windowDurationMins: 10_080,
          resetsAt: 1_700_100_000
        ),
        secondary: RateLimitWindow(
          usedPercent: 31,
          windowDurationMins: 300,
          resetsAt: 1_700_000_000
        )
      ),
      rateLimitsByLimitId: nil,
      rateLimitResetCredits: nil
    )

    let snapshot = try response.usageSnapshot()

    #expect(snapshot.fiveHourLimit?.remainingPercent == 69)
    #expect(snapshot.weeklyLimit?.remainingPercent == 28)
  }

  @Test("Reads reset credit expiration details from the rate-limit response")
  func decodesResetCreditExpirations() throws {
    let data = Data(
      """
      {
        "rateLimits": {
          "limitId": "codex",
          "planType": null,
          "primary": {"usedPercent": 20, "windowDurationMins": 10080, "resetsAt": 1800000000},
          "secondary": null
        },
        "rateLimitsByLimitId": null,
        "rateLimitResetCredits": {
          "availableCount": 3,
          "credits": [
            {
              "id": "later",
              "resetType": "codexRateLimits",
              "status": "available",
              "grantedAt": 1790000000,
              "expiresAt": 1792000000,
              "title": "Rate-limit reset",
              "description": "Later credit"
            },
            {
              "id": "earlier",
              "resetType": "codexRateLimits",
              "status": "AVAILABLE",
              "grantedAt": 1790000000,
              "expiresAt": 1791000000,
              "title": "Rate-limit reset",
              "description": "Earlier credit"
            },
            {
              "id": "used",
              "resetType": "codexRateLimits",
              "status": "used",
              "grantedAt": 1790000000,
              "expiresAt": 1790500000,
              "title": "Rate-limit reset",
              "description": "Already used credit"
            }
          ]
        }
      }
      """.utf8
    )

    let response = try JSONDecoder().decode(GetAccountRateLimitsResponse.self, from: data)
    let snapshot = try response.usageSnapshot(
      account: CodexAccount(type: "chatgpt", email: "owner@example.com", planType: "business"),
      fetchedAt: Date(timeIntervalSince1970: 1_790_996_400)
    )

    #expect(snapshot.availableResetCredits == 3)
    #expect(snapshot.resetCreditDetails?.count == 3)
    #expect(snapshot.availableResetCreditDetails.count == 2)
    #expect(snapshot.earliestResetCreditExpiration == Date(timeIntervalSince1970: 1_791_000_000))
    #expect(
      snapshot.resetCreditExpirationCountdown(relativeTo: snapshot.fetchedAt) == "1시간"
    )
    #expect(!snapshot.hasCompleteResetCreditDetails)
    #expect(snapshot.planDisplayName == "Business")
  }

  @Test("Marks an expired reset credit without appending a duration suffix")
  func formatsExpiredResetCredit() {
    let now = Date(timeIntervalSince1970: 1_000_000)
    let snapshot = UsageSnapshot(
      weeklyLimit: UsageLimitWindow(
        usedPercent: 10,
        windowDurationMinutes: 10_080,
        resetsAt: nil
      ),
      planType: "plus",
      availableResetCredits: 1,
      resetCreditDetails: [
        ResetCreditDetail(
          id: "expired",
          status: "available",
          grantedAt: nil,
          expiresAt: now.addingTimeInterval(-1),
          title: nil,
          description: nil
        )
      ],
      accountEmail: nil,
      fetchedAt: now
    )

    #expect(snapshot.hasCompleteResetCreditDetails)
    #expect(snapshot.resetCreditExpirationCountdown(relativeTo: now) == "곧 소멸")
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

    #expect(snapshot.fiveHourLimit == nil)
    #expect(snapshot.weeklyLimit?.usedPercent == 82)
    #expect(snapshot.weeklyLimit?.remainingPercent == 18)
    #expect(snapshot.planDisplayName == "Pro")
  }

  @Test("Keeps a five-hour limit usable when the weekly window is absent")
  func handlesSingleFiveHourWindow() throws {
    let response = GetAccountRateLimitsResponse(
      rateLimits: RateLimitSnapshot(
        limitId: "codex",
        planType: "team",
        primary: RateLimitWindow(
          usedPercent: 10,
          windowDurationMins: 300,
          resetsAt: 1_800_000_000
        ),
        secondary: nil
      ),
      rateLimitsByLimitId: nil,
      rateLimitResetCredits: nil
    )

    let snapshot = try response.usageSnapshot()

    #expect(snapshot.fiveHourLimit?.remainingPercent == 90)
    #expect(snapshot.weeklyLimit == nil)
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
    #expect(snapshot.weeklyLimit?.usedPercent == 100)
    #expect(snapshot.weeklyLimit?.remainingPercent == 0)
  }

  @Test("Formats the time remaining until the weekly limit resets")
  func formatsResetCountdown() {
    let now = Date(timeIntervalSince1970: 1_000_000)

    #expect(
      snapshot(resetAfter: 2 * 86_400 + 3 * 3_600, from: now)
        .weeklyLimit?.resetCountdown(relativeTo: now) == "2일 3시간"
    )
    #expect(
      snapshot(resetAfter: 5 * 3_600 + 25 * 60, from: now)
        .weeklyLimit?.resetCountdown(relativeTo: now) == "5시간 25분"
    )
    #expect(
      snapshot(resetAfter: 45, from: now).weeklyLimit?.resetCountdown(relativeTo: now) == "1분"
    )
    #expect(
      snapshot(resetAfter: -1, from: now).weeklyLimit?.resetCountdown(relativeTo: now)
        == "곧 초기화"
    )
  }

  private func snapshot(resetAfter seconds: TimeInterval, from date: Date) -> UsageSnapshot {
    UsageSnapshot(
      weeklyLimit: UsageLimitWindow(
        usedPercent: 72,
        windowDurationMinutes: 10_080,
        resetsAt: date.addingTimeInterval(seconds)
      ),
      planType: "plus",
      availableResetCredits: nil,
      accountEmail: nil,
      fetchedAt: date
    )
  }
}
