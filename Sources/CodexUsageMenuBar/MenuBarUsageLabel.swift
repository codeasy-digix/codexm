import AppKit
import SwiftUI

enum MenuBarIndicator: Equatable {
  case limits(fiveHour: Int?, weekly: Int?)
  case loading
  case unavailable

  private static func clamped(_ percent: Int) -> Int {
    min(max(percent, 0), 100)
  }

  var displayedPercent: Int? {
    guard case .limits(let fiveHour, let weekly) = self else { return nil }
    return (fiveHour ?? weekly).map(Self.clamped)
  }

  var text: String {
    switch self {
    case .limits:
      return displayedPercent.map { "\($0)%" } ?? "!"
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

  var weeklyRemainingFraction: CGFloat? {
    guard case .limits(let fiveHour, let weekly) = self,
      let percent = weekly ?? fiveHour
    else { return nil }
    return CGFloat(Self.clamped(percent)) / 100
  }

  var accessibilityLabel: String {
    switch self {
    case .limits(let fiveHour?, let weekly?):
      return "Codex 5시간 사용량 \(Self.clamped(fiveHour))퍼센트, 주간 사용량 \(Self.clamped(weekly))퍼센트 남음"
    case .limits(let fiveHour?, nil):
      return "Codex 5시간 사용량 \(Self.clamped(fiveHour))퍼센트 남음"
    case .limits(nil, let weekly?):
      return "Codex 주간 사용량 \(Self.clamped(weekly))퍼센트 남음"
    case .limits(nil, nil):
      return "Codex 사용량을 확인할 수 없음"
    case .loading:
      return "Codex 사용량을 불러오는 중"
    case .unavailable:
      return "Codex 사용량을 확인할 수 없음"
    }
  }
}

struct MenuBarUsageLabel: View {
  let indicator: MenuBarIndicator
  var style: MenuBarIconStyle = .terminal

  var body: some View {
    Image(nsImage: CodexMenuBarIconRenderer.image(for: indicator, style: style))
      .renderingMode(.template)
      .accessibilityLabel(indicator.accessibilityLabel)
  }
}

/// Renders a compact terminal-style usage value into one native template image.
enum CodexMenuBarIconRenderer {
  static let size = NSSize(width: 43, height: 22)
  static let circularSize = NSSize(width: 54, height: 22)

  static func image(
    for indicator: MenuBarIndicator,
    style: MenuBarIconStyle = .terminal
  ) -> NSImage {
    switch style {
    case .terminal:
      return terminalImage(for: indicator)
    case .circular:
      return circularImage(for: indicator)
    }
  }

  static func imageSize(for style: MenuBarIconStyle) -> NSSize {
    switch style {
    case .terminal:
      return size
    case .circular:
      return circularSize
    }
  }

  private static func terminalImage(for indicator: MenuBarIndicator) -> NSImage {
    let image = NSImage(size: imageSize(for: .terminal), flipped: false) { rect in
      NSGraphicsContext.current?.shouldAntialias = true

      let text = attributedText(for: indicator)
      let textSize = text.size()
      let textRect = NSRect(
        x: 1,
        y: ((rect.height - textSize.height) / 2) + 0.5,
        width: rect.width - 2,
        height: textSize.height
      )

      if let fraction = indicator.weeklyRemainingFraction, fraction > 0 {
        let batteryRect = NSRect(
          x: textRect.minX,
          y: textRect.minY + 1,
          width: textRect.width,
          height: max(1, textRect.height - 2)
        )
        let fillRect = NSRect(
          x: batteryRect.minX,
          y: batteryRect.minY,
          width: batteryRect.width * fraction,
          height: batteryRect.height
        )
        let fill = NSBezierPath(roundedRect: fillRect, xRadius: 2.2, yRadius: 2.2)
        NSColor.black.setFill()
        fill.fill()

        // The same template glyph is black outside the weekly fill and punched
        // out inside it, producing the requested positive/negative transition.
        text.draw(in: textRect)
        NSGraphicsContext.saveGraphicsState()
        fill.addClip()
        NSGraphicsContext.current?.compositingOperation = .clear
        text.draw(in: textRect)
        NSGraphicsContext.restoreGraphicsState()
      } else {
        text.draw(in: textRect)
      }
      return true
    }
    image.isTemplate = true
    return image
  }

  private static func circularImage(for indicator: MenuBarIndicator) -> NSImage {
    let image = NSImage(size: imageSize(for: .circular), flipped: false) { rect in
      NSGraphicsContext.current?.shouldAntialias = true

      let ringRect = NSRect(x: 1.5, y: 2.5, width: 17, height: 17)
      let track = NSBezierPath(ovalIn: ringRect)
      track.lineWidth = 2.1
      NSColor.black.withAlphaComponent(0.24).setStroke()
      track.stroke()

      let progress = indicator.weeklyRemainingFraction ?? (indicator == .loading ? 0.28 : 0)
      if progress > 0 {
        let progressRing = NSBezierPath()
        progressRing.appendArc(
          withCenter: NSPoint(x: ringRect.midX, y: ringRect.midY),
          radius: ringRect.width / 2,
          startAngle: 90,
          endAngle: 90 - (360 * progress),
          clockwise: true
        )
        progressRing.lineWidth = 2.3
        progressRing.lineCapStyle = .round
        NSColor.black.setStroke()
        progressRing.stroke()
      }

      let text = circularAttributedText(for: indicator)
      let textSize = text.size()
      let textRect = NSRect(
        x: 22,
        y: ((rect.height - textSize.height) / 2) + 0.5,
        width: rect.width - 22,
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

  static func circularAttributedText(for indicator: MenuBarIndicator) -> NSAttributedString {
    NSAttributedString(
      string: indicator.text,
      attributes: [
        .font: NSFont.monospacedSystemFont(ofSize: 11.5, weight: .semibold),
        .foregroundColor: NSColor.black,
      ]
    )
  }
}
