import SwiftUI

@main
@MainActor
struct CodexUsageMenuBarApp: App {
  @StateObject private var store: UsageStore
  @StateObject private var preferences: AppPreferences

  init() {
    let store = UsageStore()
    let preferences = AppPreferences()
    _store = StateObject(wrappedValue: store)
    _preferences = StateObject(wrappedValue: preferences)
    store.start()
  }

  var body: some Scene {
    MenuBarExtra {
      MenuContentView(store: store, preferences: preferences)
    } label: {
      MenuBarUsageLabel(indicator: menuBarIndicator, style: preferences.menuBarIconStyle)
    }
    .menuBarExtraStyle(.window)
  }

  private var menuBarIndicator: MenuBarIndicator {
    switch store.state {
    case .loaded(let snapshot):
      return .remaining(snapshot.remainingPercent)
    case .loading:
      return .loading
    case .missingRuntime, .failed:
      return .unavailable
    }
  }
}
