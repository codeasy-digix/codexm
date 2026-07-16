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

  var terminalText: String { "> \(text)" }

  var usageUnderlineRange: NSRange? {
    guard case .remaining = self else { return nil }
    return NSRange(location: 2, length: (text as NSString).length)
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

/// Renders the outline and percentage into one native template image. Keeping
/// them in one image prevents macOS menu bar label simplification from dropping
/// the custom outline while preserving the text.
enum CodexMenuBarIconRenderer {
  static let size = NSSize(width: 46, height: 22)
  static let outlineLineWidth: CGFloat = 1.2

  static func image(for indicator: MenuBarIndicator) -> NSImage {
    let image = NSImage(size: size, flipped: false) { rect in
      NSGraphicsContext.current?.shouldAntialias = true

      let outline = outlinePath(in: rect)
      outline.lineWidth = outlineLineWidth
      outline.lineCapStyle = .round
      outline.lineJoinStyle = .round
      NSColor.black.setStroke()
      outline.stroke()

      let text = attributedText(for: indicator)
      let textSize = text.size()
      let textRect = NSRect(
        x: 4,
        y: ((rect.height - textSize.height) / 2) + 0.5,
        width: rect.width - 8,
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
        .font: NSFont.monospacedSystemFont(ofSize: 8.5, weight: .semibold),
        .foregroundColor: NSColor.black,
        .paragraphStyle: paragraph,
      ]
    )
    if let range = indicator.usageUnderlineRange {
      text.addAttributes(
        [
          .underlineStyle: NSUnderlineStyle.single.rawValue,
          .underlineColor: NSColor.black,
        ],
        range: range
      )
    }
    return text
  }

  static func outlinePath(in rect: NSRect) -> NSBezierPath {
    let drawingRect = rect.insetBy(dx: 2, dy: 2)
    let center = CGPoint(x: drawingRect.midX, y: drawingRect.midY)
    let radiusX = drawingRect.width / 2
    let radiusY = drawingRect.height / 2
    let sampleCount = 120
    let path = NSBezierPath()

    for index in 0...sampleCount {
      let angle = (Double(index) / Double(sampleCount)) * 2 * Double.pi
      let lobe = 1 + 0.04 * cos(8 * angle)
      let point = CGPoint(
        x: center.x + CGFloat(cos(angle) * lobe) * radiusX,
        y: center.y + CGFloat(sin(angle) * lobe) * radiusY
      )

      if index == 0 {
        path.move(to: point)
      } else {
        path.line(to: point)
      }
    }
    path.close()
    return path
  }
}
