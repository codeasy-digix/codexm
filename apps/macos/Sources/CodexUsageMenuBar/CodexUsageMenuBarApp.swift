import SwiftUI
import Darwin

@main
@MainActor
struct CodexUsageMenuBarApp: App {
  @StateObject private var store: UsageStore
  @StateObject private var preferences: AppPreferences

  init() {
    if CommandLine.arguments.contains("--localization-diagnostics") {
      FileHandle.standardOutput.write(L10n.diagnosticData())
      FileHandle.standardOutput.write(Data("\n".utf8))
      exit(L10n.catalogsAreComplete ? 0 : 1)
    }
    let store = UsageStore()
    let preferences = AppPreferences()
    _store = StateObject(wrappedValue: store)
    _preferences = StateObject(wrappedValue: preferences)
    store.start()
  }

  var body: some Scene {
    MenuBarExtra {
      MenuContentView(store: store, preferences: preferences)
        .environment(\.locale, L10n.locale)
    } label: {
      MenuBarUsageLabel(indicator: menuBarIndicator, style: preferences.menuBarIconStyle,
        preferences: preferences)
    }
    .menuBarExtraStyle(.window)
  }

  private var menuBarIndicator: MenuBarIndicator {
    switch store.primaryAccountState {
    case .loaded(let snapshot):
      return .limits(
        fiveHour: snapshot.fiveHourLimit?.remainingPercent,
        weekly: snapshot.weeklyLimit?.remainingPercent
      )
    case .loading:
      return .loading
    case .needsAuthentication, .failed:
      return .unavailable
    }
  }
}
