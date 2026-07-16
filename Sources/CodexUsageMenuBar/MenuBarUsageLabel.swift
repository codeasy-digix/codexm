import AppKit
import SwiftUI

enum MenuBarIndicator: Equatable {
  case remaining(Int)
  case loading
  case unavailable

  var text: String {
    switch self {
    case .remaining(let percent):
      return "\(min(max(percent, 0), 100))%"
    case .loading:
      return "•••"
    case .unavailable:
      return "!"
    }
  }

  var terminalText: String { ">\(text)" }

  var terminalUnderlineRange: NSRange {
    NSRange(location: 0, length: (terminalText as NSString).length)
  }

  var accessibilityLabel: String {
    switch self {
    case .remaining(let percent):
      return "Codex 주간 사용량 \(min(max(percent, 0), 100))퍼센트 남음"
    case .loading:
      return "Codex 주간 사용량을 불러오는 중"
    case .unavailable:
      return "Codex 주간 사용량을 확인할 수 없음"
    }
  }
}

struct MenuBarUsageLabel: View {
  let indicator: MenuBarIndicator

  var body: some View {
    Image(nsImage: CodexMenuBarIconRenderer.image(for: indicator))
      .renderingMode(.template)
      .accessibilityLabel(indicator.accessibilityLabel)
  }
}

/// Renders a compact terminal-style usage value into one native template image.
enum CodexMenuBarIconRenderer {
  static let size = NSSize(width: 43, height: 22)

  static func image(for indicator: MenuBarIndicator) -> NSImage {
    let image = NSImage(size: size, flipped: false) { rect in
      NSGraphicsContext.current?.shouldAntialias = true

      let text = attributedText(for: indicator)
      let textSize = text.size()
      let textRect = NSRect(
        x: 1,
        y: ((rect.height - textSize.height) / 2) + 0.5,
        width: rect.width - 2,
        height: textSize.height
      )
      text.draw(in: textRect)
      return true
    }
    image.isTemplate = true
    return image
  }

  static func attributedText(for indicator: MenuBarIndicator) -> NSAttributedString {
    let paragraph = NSMutableParagraphStyle()
    paragraph.alignment = .center
    let text = NSMutableAttributedString(
      string: indicator.terminalText,
      attributes: [
        .font: NSFont.monospacedSystemFont(ofSize: 12.6, weight: .semibold),
        .foregroundColor: NSColor.black,
        .paragraphStyle: paragraph,
      ]
    )
    text.addAttributes(
      [
        .underlineStyle: NSUnderlineStyle.single.rawValue,
        .underlineColor: NSColor.black,
      ],
      range: indicator.terminalUnderlineRange
    )
    return text
  }
}
