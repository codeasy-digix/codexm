import SwiftUI

@main
@MainActor
struct CodexUsageMenuBarApp: App {
  @StateObject private var store: UsageStore

  init() {
    let store = UsageStore()
    _store = StateObject(wrappedValue: store)
    store.start()
  }

  var body: some Scene {
    MenuBarExtra {
      MenuContentView(store: store)
    } label: {
      MenuBarUsageLabel(indicator: menuBarIndicator)
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
