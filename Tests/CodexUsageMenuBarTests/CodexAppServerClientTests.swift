import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Persistent Codex app-server", .serialized)
struct CodexAppServerClientTests {
  @Test("Reuses one process for repeated reads")
  func reusesOneProcess() async throws {
    let fixture = try MockAppServerFixture()
    let client = CodexAppServerClient(timeout: .seconds(3), environment: [:])
    defer {
      client.shutdown()
      fixture.remove()
    }

    _ = try await client.fetchUsage(
      sessionID: "default",
      diagnosticLabel: "default",
      identityRevision: "workspace-a",
      codexURL: fixture.executableURL,
      environmentOverride: fixture.environment
    )
    _ = try await client.fetchUsage(
      sessionID: "default",
      diagnosticLabel: "default",
      identityRevision: "workspace-a",
      codexURL: fixture.executableURL,
      environmentOverride: fixture.environment
    )

    #expect(try fixture.launchCount() == 1)
    #expect(try fixture.rateLimitReadCount() == 2)
  }

  @Test("Coalesces concurrent reads for the same account")
  func coalescesConcurrentReads() async throws {
    let fixture = try MockAppServerFixture(slowRateLimitResponse: true)
    let client = CodexAppServerClient(timeout: .seconds(3), environment: [:])
    defer {
      client.shutdown()
      fixture.remove()
    }

    async let first = client.fetchUsage(
      sessionID: "default",
      diagnosticLabel: "default",
      identityRevision: "workspace-a",
      codexURL: fixture.executableURL,
      environmentOverride: fixture.environment
    )
    async let second = client.fetchUsage(
      sessionID: "default",
      diagnosticLabel: "default",
      identityRevision: "workspace-a",
      codexURL: fixture.executableURL,
      environmentOverride: fixture.environment
    )
    _ = try await (first, second)

    #expect(try fixture.launchCount() == 1)
    #expect(try fixture.rateLimitReadCount() == 1)
  }

  @Test("Replaces the session when the account identity changes")
  func replacesChangedIdentity() async throws {
    let fixture = try MockAppServerFixture()
    let client = CodexAppServerClient(timeout: .seconds(3), environment: [:])
    defer {
      client.shutdown()
      fixture.remove()
    }

    _ = try await client.fetchUsage(
      sessionID: "default",
      diagnosticLabel: "default",
      identityRevision: "workspace-a",
      codexURL: fixture.executableURL,
      environmentOverride: fixture.environment
    )
    _ = try await client.fetchUsage(
      sessionID: "default",
      diagnosticLabel: "default",
      identityRevision: "workspace-b",
      codexURL: fixture.executableURL,
      environmentOverride: fixture.environment
    )

    #expect(try fixture.launchCount() == 2)
    #expect(try fixture.rateLimitReadCount() == 2)
  }

  @Test("Forwards rate-limit update notifications")
  func forwardsNotifications() async throws {
    let fixture = try MockAppServerFixture(sendNotification: true)
    let client = CodexAppServerClient(timeout: .seconds(3), environment: [:])
    let updates = SnapshotCollector()
    defer {
      client.shutdown()
      fixture.remove()
    }

    _ = try await client.fetchUsage(
      sessionID: "default",
      diagnosticLabel: "default",
      identityRevision: "workspace-a",
      codexURL: fixture.executableURL,
      environmentOverride: fixture.environment,
      onUpdate: { snapshot in
        Task { await updates.append(snapshot) }
      }
    )

    await waitUntil(timeout: .seconds(2)) {
      await updates.count > 0
    }
    let latest = await updates.latest
    #expect(latest?.fiveHourLimit?.remainingPercent == 79)
    #expect(latest?.weeklyLimit?.remainingPercent == 59)
    #expect(latest?.accountEmail == "test@example.com")
  }
}

@Suite("Usage refresh failure state")
struct UsageRefreshFailureStateTests {
  @Test("Keeps the last good snapshot after a transient failure")
  @MainActor
  func keepsLastGoodSnapshot() {
    let snapshot = UsageSnapshot(
      fiveHourLimit: UsageLimitWindow(
        usedPercent: 20,
        windowDurationMinutes: 300,
        resetsAt: nil
      ),
      planType: "plus",
      availableResetCredits: nil,
      accountEmail: "test@example.com",
      fetchedAt: Date(timeIntervalSince1970: 1_000)
    )
    var state = UsageStore.AccountViewState(
      account: .systemDefault,
      state: .loaded(snapshot),
      isRefreshing: false
    )

    state.applyRefreshFailure(.timedOut)

    #expect(state.state == .loaded(snapshot))
    #expect(state.lastRefreshError == .timedOut)
  }

  @Test("Clears the transient error after a successful refresh")
  @MainActor
  func clearsErrorOnSuccess() {
    let first = UsageSnapshot(
      weeklyLimit: UsageLimitWindow(
        usedPercent: 40,
        windowDurationMinutes: 10_080,
        resetsAt: nil
      ),
      planType: "plus",
      availableResetCredits: nil,
      accountEmail: "test@example.com",
      fetchedAt: Date(timeIntervalSince1970: 1_000)
    )
    let second = UsageSnapshot(
      weeklyLimit: UsageLimitWindow(
        usedPercent: 41,
        windowDurationMinutes: 10_080,
        resetsAt: nil
      ),
      planType: "plus",
      availableResetCredits: nil,
      accountEmail: "test@example.com",
      fetchedAt: Date(timeIntervalSince1970: 2_000)
    )
    var state = UsageStore.AccountViewState(
      account: .systemDefault,
      state: .loaded(first),
      isRefreshing: false,
      lastRefreshError: .timedOut
    )

    state.apply(snapshot: second)

    #expect(state.state == .loaded(second))
    #expect(state.lastRefreshError == nil)
  }
}

private actor SnapshotCollector {
  private(set) var snapshots: [UsageSnapshot] = []

  var count: Int { snapshots.count }
  var latest: UsageSnapshot? { snapshots.last }

  func append(_ snapshot: UsageSnapshot) {
    snapshots.append(snapshot)
  }
}

private func waitUntil(
  timeout: Duration,
  condition: @escaping @Sendable () async -> Bool
) async {
  let clock = ContinuousClock()
  let deadline = clock.now.advanced(by: timeout)
  while !(await condition()), clock.now < deadline {
    try? await Task.sleep(for: .milliseconds(10))
  }
}

private struct MockAppServerFixture: Sendable {
  let rootURL: URL
  let executableURL: URL
  let launchCountURL: URL
  let rateLimitReadCountURL: URL
  let environment: [String: String]

  init(
    slowRateLimitResponse: Bool = false,
    sendNotification: Bool = false
  ) throws {
    rootURL = FileManager.default.temporaryDirectory.appending(
      path: "CodexAppServerClientTests-\(UUID().uuidString)",
      directoryHint: .isDirectory
    )
    executableURL = rootURL.appending(path: "mock-codex", directoryHint: .notDirectory)
    launchCountURL = rootURL.appending(path: "launch-count", directoryHint: .notDirectory)
    rateLimitReadCountURL = rootURL.appending(
      path: "rate-limit-read-count",
      directoryHint: .notDirectory
    )
    try FileManager.default.createDirectory(
      at: rootURL,
      withIntermediateDirectories: true
    )
    try Data(Self.mockExecutable.utf8).write(to: executableURL)
    try FileManager.default.setAttributes(
      [.posixPermissions: 0o755],
      ofItemAtPath: executableURL.path
    )
    environment = [
      "PATH": "/usr/bin:/bin",
      "CODEX_HOME": rootURL.path,
      "MOCK_LAUNCH_COUNT": launchCountURL.path,
      "MOCK_RATE_LIMIT_COUNT": rateLimitReadCountURL.path,
      "MOCK_SLOW": slowRateLimitResponse ? "1" : "0",
      "MOCK_NOTIFY": sendNotification ? "1" : "0",
    ]
  }

  func launchCount() throws -> Int {
    try lineCount(at: launchCountURL)
  }

  func rateLimitReadCount() throws -> Int {
    try lineCount(at: rateLimitReadCountURL)
  }

  func remove() {
    try? FileManager.default.removeItem(at: rootURL)
  }

  private func lineCount(at url: URL) throws -> Int {
    let value = try String(contentsOf: url, encoding: .utf8)
    return value.split(separator: "\n").count
  }

  private static var mockExecutable: String {
    #"""
    #!/bin/sh
    printf '1\n' >> "$MOCK_LAUNCH_COUNT"

    while IFS= read -r line; do
      id=$(printf '%s\n' "$line" | /usr/bin/sed -E 's/.*"id":([0-9]+).*/\1/')
      case "$line" in
        *initialize*)
          printf '{"id":%s,"result":{"userAgent":"mock"}}\n' "$id"
          ;;
        *refreshToken*)
          printf '{"id":%s,"result":{"account":{"type":"chatgpt","email":"test@example.com","planType":"plus"},"requiresOpenaiAuth":true}}\n' "$id"
          ;;
        *rateLimits*)
          printf '1\n' >> "$MOCK_RATE_LIMIT_COUNT"
          if [ "$MOCK_SLOW" = "1" ]; then
            /bin/sleep 0.15
          fi
          printf '{"id":%s,"result":{"rateLimits":{"limitId":"codex","planType":"plus","primary":{"usedPercent":20,"windowDurationMins":300,"resetsAt":2000000000},"secondary":{"usedPercent":40,"windowDurationMins":10080,"resetsAt":2000000000}}}}\n' "$id"
          if [ "$MOCK_NOTIFY" = "1" ]; then
            printf '{"method":"account/rateLimits/updated","params":{"rateLimits":{"limitId":"codex","planType":"plus","primary":{"usedPercent":21,"windowDurationMins":300,"resetsAt":2000000000},"secondary":{"usedPercent":41,"windowDurationMins":10080,"resetsAt":2000000000}}}}\n'
            MOCK_NOTIFY=0
          fi
          ;;
      esac
    done
    """#
  }
}
