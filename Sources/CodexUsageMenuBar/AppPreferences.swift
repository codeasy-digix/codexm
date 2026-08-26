import Foundation

enum MenuBarIconStyle: String, CaseIterable, Identifiable {
  case terminal
  case circular

  var id: Self { self }

  var title: String {
    switch self {
    case .terminal:
      return "5시간 숫자 + 주간 채움"
    case .circular:
      return "주간 원형 + 5시간 숫자"
    }
  }

  var systemImage: String {
    switch self {
    case .terminal:
      return "terminal"
    case .circular:
      return "chart.pie"
    }
  }
}

@MainActor
final class AppPreferences: ObservableObject {
  private enum Key {
    static let menuBarIconStyle = "MenuBarIconStyle"
  }

  private let defaults: UserDefaults

  @Published var menuBarIconStyle: MenuBarIconStyle {
    didSet {
      defaults.set(menuBarIconStyle.rawValue, forKey: Key.menuBarIconStyle)
    }
  }

  init(defaults: UserDefaults = .standard) {
    self.defaults = defaults
    self.menuBarIconStyle = defaults.string(forKey: Key.menuBarIconStyle)
      .flatMap(MenuBarIconStyle.init(rawValue:)) ?? .terminal
  }
}
