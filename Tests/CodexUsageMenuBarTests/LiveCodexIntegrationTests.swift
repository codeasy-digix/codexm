import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Live Codex integration")
struct LiveCodexIntegrationTests {
  @Test("Reads the signed-in account weekly limit through app-server")
  func readsLiveUsage() async throws {
    guard ProcessInfo.processInfo.environment["CODEX_LIVE_TEST"] == "1" else {
      return
    }

    let codex = try #require(CodexLocator().locate())
    let snapshot = try await CodexAppServerClient().fetchUsage(codexURL: codex)

    #expect((0...100).contains(snapshot.usedPercent))
    #expect(snapshot.windowDurationMinutes == 10_080)
    #expect(snapshot.resetsAt != nil)
  }
}
