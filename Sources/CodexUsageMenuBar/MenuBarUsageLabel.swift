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
  static let size = NSSize(width: 42, height: 22)
  static let outlineLineWidth: CGFloat = 1.35

  static func image(for indicator: MenuBarIndicator) -> NSImage {
    let image = NSImage(size: size, flipped: false) { rect in
      NSGraphicsContext.current?.shouldAntialias = true

      let outline = outlinePath(in: rect)
      outline.lineWidth = outlineLineWidth
      outline.lineCapStyle = .round
      outline.lineJoinStyle = .round
      NSColor.black.setStroke()
      outline.stroke()

      let paragraph = NSMutableParagraphStyle()
      paragraph.alignment = .center
      let attributes: [NSAttributedString.Key: Any] = [
        .font: NSFont.monospacedDigitSystemFont(ofSize: 9, weight: .bold),
        .foregroundColor: NSColor.black,
        .paragraphStyle: paragraph,
      ]
      let text = NSAttributedString(string: indicator.text, attributes: attributes)
      let textSize = text.size()
      let textRect = NSRect(
        x: 5,
        y: ((rect.height - textSize.height) / 2) + 0.5,
        width: rect.width - 10,
        height: textSize.height
      )
      text.draw(in: textRect)
      return true
    }
    image.isTemplate = true
    return image
  }

  static func outlinePath(in rect: NSRect) -> NSBezierPath {
    let drawingRect = rect.insetBy(dx: 2.5, dy: 2.5)
    let center = CGPoint(x: drawingRect.midX, y: drawingRect.midY)
    let radiusX = drawingRect.width / 2
    let radiusY = drawingRect.height / 2
    let sampleCount = 120
    let path = NSBezierPath()

    for index in 0...sampleCount {
      let angle = (Double(index) / Double(sampleCount)) * 2 * Double.pi
      let lobe = 1 + 0.07 * cos(8 * angle)
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
