import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("Live Codex integration")
struct LiveCodexIntegrationTests {
  @Test("Reads the signed-in account five-hour and weekly limits through app-server")
  func readsLiveUsage() async throws {
    guard ProcessInfo.processInfo.environment["CODEX_LIVE_TEST"] == "1" else {
      return
    }

    let environment = ProcessInfo.processInfo.environment
    let codex =
      environment["CODEX_RUNTIME_PATH"].map(URL.init(fileURLWithPath:))
      ?? CodexLocator().locate()
    let executable = try #require(codex)
    let runtime = try CodexRuntimeLocator(
      environment: ["HOME": FileManager.default.homeDirectoryForCurrentUser.path],
      runtimeURLOverride: executable
    ).locate()
    let snapshot = try await CodexAppServerClient()
      .fetchUsage(
        codexURL: runtime.executableURL,
        environmentOverride: runtime.environment
      )

    let fiveHourLimit = try #require(snapshot.fiveHourLimit)
    let weeklyLimit = try #require(snapshot.weeklyLimit)
    #expect((0...100).contains(fiveHourLimit.usedPercent))
    #expect(fiveHourLimit.windowDurationMinutes == 300)
    #expect(fiveHourLimit.resetsAt != nil)
    #expect((0...100).contains(weeklyLimit.usedPercent))
    #expect(weeklyLimit.windowDurationMinutes == 10_080)
    #expect(weeklyLimit.resetsAt != nil)
    #expect(snapshot.accountEmail?.contains("@") == true)
  }

  @Test("Performs one explicit automatic activation turn")
  func performsLiveAutomaticActivation() async throws {
    guard ProcessInfo.processInfo.environment["CODEX_LIVE_AUTOMATIC_TEST"] == "1" else {
      return
    }

    let environment = ProcessInfo.processInfo.environment
    let codex =
      environment["CODEX_RUNTIME_PATH"].map(URL.init(fileURLWithPath:))
      ?? CodexLocator().locate()
    let executable = try #require(codex)
    let runtime = try CodexRuntimeLocator(
      environment: ["HOME": FileManager.default.homeDirectoryForCurrentUser.path],
      runtimeURLOverride: executable
    ).locate()
    let expression = AutomaticUsagePromptGenerator().makeExpression(excluding: nil)

    try await CodexAutomaticUsageClient().performRequest(
      codexURL: runtime.executableURL,
      environmentOverride: runtime.environment,
      workingDirectoryURL: FileManager.default.temporaryDirectory,
      prompt: AutomaticUsagePromptGenerator().prompt(for: expression)
    )
  }

  @Test("Adds the Codex launcher directory to a Finder-like PATH")
  func augmentsFinderPath() throws {
    let codexURL = URL(fileURLWithPath: "/Users/example/.nvm/versions/node/v25/bin/codex")
    let environment = CodexAppServerClient.processEnvironment(
      for: codexURL,
      base: ["PATH": "/usr/bin:/bin"]
    )

    let path = try #require(environment["PATH"])
    #expect(path.hasPrefix("/Users/example/.nvm/versions/node/v25/bin:"))
    #expect(path.contains("/usr/bin:/bin"))
  }
}
