import Foundation
import Testing

@testable import CodexUsageMenuBar

@Suite("App preferences")
@MainActor
struct AppPreferencesTests {
  @Test("Uses the current terminal-style icon by default")
  func defaultIconStyle() throws {
    let defaults = try #require(UserDefaults(suiteName: "AppPreferencesTests.default"))
    defaults.removePersistentDomain(forName: "AppPreferencesTests.default")

    let preferences = AppPreferences(defaults: defaults)

    #expect(preferences.menuBarIconStyle == .terminal)
    #expect(MenuBarIconStyle.terminal.title == "5시간 숫자 + 주간 채움")
    #expect(MenuBarIconStyle.circular.title == "주간 원형 + 5시간 숫자")
  }

  @Test("Persists the selected circular icon style")
  func persistsIconStyle() throws {
    let suiteName = "AppPreferencesTests.persistence"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)

    let preferences = AppPreferences(defaults: defaults)
    preferences.menuBarIconStyle = .circular
    let restored = AppPreferences(defaults: defaults)

    #expect(restored.menuBarIconStyle == .circular)
    defaults.removePersistentDomain(forName: suiteName)
  }
}
