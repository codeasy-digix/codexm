import AppKit
import SwiftUI

enum ProductInformationKind: CaseIterable {
  case about
  case automaticRefresh
  case licenses

  var title: String {
    switch self {
    case .about: return L10n.text("about.title")
    case .automaticRefresh: return L10n.text("auto.help_title")
    case .licenses: return L10n.text("about.licenses")
    }
  }

  var paragraphKeys: [String] {
    switch self {
    case .about:
      return ["about.overview", "about.accounts", "options.interval_help", "about.refresh",
        "about.privacy", "about.opensource"]
    case .automaticRefresh:
      return ["auto.help.what", "auto.help.schedule", "auto.help.cost", "auto.help.limits",
        "auto.help.devices", "auto.help.privacy"]
    case .licenses: return ["about.license_original"]
    }
  }
}

private final class ProductInformationPanel: NSPanel {
  override var canBecomeKey: Bool { true }
  override var canBecomeMain: Bool { false }
}

/// Separate, focusable panels stay usable when the menu-bar window closes.
/// Keep each controller alive so repeated clicks reveal the existing panel.
@MainActor
final class ProductInformationPresenter {
  static let shared = ProductInformationPresenter()
  private var controllers: [ProductInformationKind: NSWindowController] = [:]

  func show(_ kind: ProductInformationKind) {
    if let controller = controllers[kind] {
      NSApp.activate(ignoringOtherApps: true)
      controller.showWindow(nil)
      controller.window?.makeKeyAndOrderFront(nil)
      return
    }

    let screen = NSScreen.screens.first { $0.frame.contains(NSEvent.mouseLocation) } ?? NSScreen.main
    let height = min(620, max(340, (screen?.visibleFrame.height ?? 800) - 100))
    let panel = ProductInformationPanel(
      contentRect: NSRect(x: 0, y: 0, width: 440, height: height),
      styleMask: [.titled, .closable, .utilityWindow], backing: .buffered, defer: false)
    panel.title = kind.title
    panel.isReleasedWhenClosed = false
    panel.isFloatingPanel = true
    panel.becomesKeyOnlyIfNeeded = false
    panel.hidesOnDeactivate = false
    panel.contentView = NSHostingView(rootView: ProductInformationView(
      kind: kind, onClose: { [weak panel] in panel?.close() }))
    panel.center()
    let controller = NSWindowController(window: panel)
    controllers[kind] = controller
    NSApp.activate(ignoringOtherApps: true)
    controller.showWindow(nil)
    panel.makeKeyAndOrderFront(nil)
  }
}

struct ProductInformationView: View {
  let kind: ProductInformationKind
  var onClose: () -> Void = {}

  var body: some View {
    VStack(spacing: 0) {
      ScrollView {
        VStack(alignment: .leading, spacing: 16) {
          Text(kind.title)
            .font(.title2.weight(.semibold))
          if kind == .about {
            Text(L10n.text("app.title"))
              .font(.headline)
            Text(L10n.text("about.version",
              Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "—",
              Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "—"))
              .font(.caption).foregroundStyle(.secondary)
          }

          ForEach(kind.paragraphKeys, id: \.self) { key in
            Text(L10n.text(key))
              .font(.callout)
              .fixedSize(horizontal: false, vertical: true)
              .textSelection(.enabled)
          }

          if kind == .about {
            HStack(spacing: 16) {
              Link(L10n.text("about.repository"),
                destination: URL(string: "https://github.com/codeasy-digix/codexm")!)
              Button(L10n.text("about.licenses")) {
                ProductInformationPresenter.shared.show(.licenses)
              }
            }
          } else if kind == .licenses {
            Text(verbatim: Self.licenseText)
              .font(.system(size: 11, design: .monospaced))
              .fixedSize(horizontal: false, vertical: true)
              .textSelection(.enabled)
          }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(22)
      }
      Divider()
      HStack {
        Spacer()
        Button(L10n.text("common.close"), action: onClose)
          .keyboardShortcut(.cancelAction)
          .controlSize(.regular)
      }
      .padding(14)
    }
    .environment(\.locale, L10n.locale)
  }

  static var licenseText: String {
    ["LICENSE.txt", "THIRD_PARTY_NOTICES.md", "CodexRuntime-LICENSE.txt"].map { file in
      let url = Bundle.main.resourceURL?.appendingPathComponent(file)
      let text = url.flatMap { try? String(contentsOf: $0, encoding: .utf8) }
        ?? L10n.text("about.license_missing")
      return "\(file)\n\n\(text)"
    }.joined(separator: "\n\n────────────────────────\n\n")
  }
}
