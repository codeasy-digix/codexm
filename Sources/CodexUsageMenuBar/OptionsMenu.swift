import SwiftUI

struct OptionsMenu: View {
  @ObservedObject var preferences: AppPreferences
  @ObservedObject var store: UsageStore

  var body: some View {
    Menu {
      Picker(L10n.text("options.language"), selection: $preferences.languagePreference) {
        ForEach(AppLanguagePreference.allCases) { language in
          Text(language.title).tag(language)
        }
      }

      Divider()

      Picker(L10n.text("options.icon"), selection: $preferences.menuBarIconStyle) {
        ForEach(MenuBarIconStyle.allCases) { style in
          Label(style.title, systemImage: style.systemImage)
            .tag(style)
        }
      }

      Divider()

      Picker(
        L10n.text("options.default_interval"),
        selection: Binding(
          get: { store.systemDefaultRefreshInterval },
          set: { store.setSystemDefaultRefreshInterval($0) }
        )
      ) {
        ForEach(SystemDefaultRefreshInterval.allCases) { interval in
          Text(interval.title).tag(interval)
        }
      }

      Divider()
      Button(L10n.text("about.title"), systemImage: "info.circle") {
        ProductInformationPresenter.shared.show(.about, preferences: preferences)
      }
    } label: {
      Image(systemName: "gearshape")
        .frame(width: 16, height: 16)
    }
    .menuStyle(.borderlessButton)
    .menuIndicator(.hidden)
    .fixedSize()
    .help(L10n.text("common.options"))
    .accessibilityLabel(L10n.text("common.options"))
  }
}
