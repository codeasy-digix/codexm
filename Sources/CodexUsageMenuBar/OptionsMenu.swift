import SwiftUI

struct OptionsMenu: View {
  @ObservedObject var preferences: AppPreferences

  var body: some View {
    Menu {
      Picker("메뉴바 아이콘", selection: $preferences.menuBarIconStyle) {
        ForEach(MenuBarIconStyle.allCases) { style in
          Label(style.title, systemImage: style.systemImage)
            .tag(style)
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
