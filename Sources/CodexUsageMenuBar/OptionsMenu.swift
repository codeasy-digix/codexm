import SwiftUI

struct OptionsMenu: View {
  @ObservedObject var preferences: AppPreferences
  @ObservedObject var store: UsageStore

  var body: some View {
    Menu {
      Picker("메뉴바 아이콘", selection: $preferences.menuBarIconStyle) {
        ForEach(MenuBarIconStyle.allCases) { style in
          Label(style.title, systemImage: style.systemImage)
            .tag(style)
        }
      }

      Divider()

      Picker(
        "기본 계정 갱신 주기",
        selection: Binding(
          get: { store.systemDefaultRefreshInterval },
          set: { store.setSystemDefaultRefreshInterval($0) }
        )
      ) {
        ForEach(SystemDefaultRefreshInterval.allCases) { interval in
          Text(interval.title).tag(interval)
        }
      }
    } label: {
      Image(systemName: "gearshape")
        .frame(width: 16, height: 16)
    }
    .menuStyle(.borderlessButton)
    .menuIndicator(.hidden)
    .fixedSize()
    .help("옵션")
    .accessibilityLabel("옵션")
  }
}
