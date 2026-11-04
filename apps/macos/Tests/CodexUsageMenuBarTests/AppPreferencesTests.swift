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
    #expect(MenuBarIconStyle.terminal.title == L10n.text("options.icon_terminal"))
    #expect(MenuBarIconStyle.circular.title == L10n.text("options.icon_circular"))
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

  @Test("Follows system language by default and rejects invalid saved preferences")
  func defaultLanguagePreference() throws {
    let suiteName = "AppPreferencesTests.languageDefault"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }

    #expect(AppPreferences(defaults: defaults).languagePreference == .system)
    defaults.set("unsupported-language", forKey: AppLanguagePreference.defaultsKey)
    #expect(AppPreferences(defaults: defaults).languagePreference == .system)
  }

  @Test("Persists language choices and allows returning to system settings", arguments: AppLanguagePreference.allCases)
  func persistsLanguagePreference(_ language: AppLanguagePreference) throws {
    let suiteName = "AppPreferencesTests.languagePersistence.\(language.rawValue)"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defaults.removePersistentDomain(forName: suiteName)
    defer { defaults.removePersistentDomain(forName: suiteName) }

    let preferences = AppPreferences(defaults: defaults)
    preferences.menuBarIconStyle = .circular
    preferences.languagePreference = language
    #expect(AppLanguagePreference.load(from: defaults) == language)
    #expect(AppPreferences(defaults: defaults).languagePreference == language)
    #expect(AppPreferences(defaults: defaults).menuBarIconStyle == .circular)

    preferences.languagePreference = .system
    #expect(AppPreferences(defaults: defaults).languagePreference == .system)
    #expect(defaults.string(forKey: AppLanguagePreference.defaultsKey) == "system")
  }
}
