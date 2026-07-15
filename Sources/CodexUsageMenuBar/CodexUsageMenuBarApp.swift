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
      Image(systemName: menuBarSymbol)
        .accessibilityLabel("Codex 주간 사용량")
    }
    .menuBarExtraStyle(.window)
  }

  private var menuBarSymbol: String {
    switch store.state {
    case .loaded(let snapshot) where snapshot.usedPercent >= 90:
      return "chart.bar.fill"
    case .loaded:
      return "chart.bar"
    case .loading:
      return "arrow.triangle.2.circlepath"
    case .missingCLI, .failed:
      return "exclamationmark.circle"
    }
  }
}
