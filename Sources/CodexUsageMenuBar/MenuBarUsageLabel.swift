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

  var fiveHourRemainingFraction: CGFloat? {
    guard case .limits(let percent?, _) = self else { return nil }
    return CGFloat(Self.clamped(percent)) / 100
  }

  var weeklyRemainingFraction: CGFloat? {
    guard case .limits(_, let percent?) = self else { return nil }
    return CGFloat(Self.clamped(percent)) / 100
  }

  var circularRemainingFraction: CGFloat? {
    weeklyRemainingFraction ?? fiveHourRemainingFraction
  }

  func helpText(for style: MenuBarIconStyle) -> String {
    style == .terminal
      ? accessibilityLabel + "\n" + L10n.text("usage.split_bars_help")
      : accessibilityLabel
  }

  var accessibilityLabel: String {
    switch self {
    case .limits(let fiveHour?, let weekly?):
      return L10n.text("usage.dual_remaining", Self.clamped(fiveHour), Self.clamped(weekly))
    case .limits(let fiveHour?, nil):
      return L10n.text("usage.five_remaining", Self.clamped(fiveHour))
    case .limits(nil, let weekly?):
      return L10n.text("usage.weekly_remaining", Self.clamped(weekly))
    case .limits(nil, nil):
      return L10n.text("usage.unavailable")
    case .loading:
      return L10n.text("usage.loading")
    case .unavailable:
      return L10n.text("usage.unavailable")
    }
  }
}

struct MenuBarUsageLabel: View {
  let indicator: MenuBarIndicator
  var style: MenuBarIconStyle = .terminal
  @ObservedObject var preferences: AppPreferences
  @Environment(\.colorScheme) private var colorScheme

  var body: some View {
    Image(nsImage: CodexMenuBarIconRenderer.image(for: indicator, style: style, colorScheme: colorScheme))
      .renderingMode(style == .terminal ? .original : .template)
      .help(indicator.helpText(for: style))
      .accessibilityLabel(indicator.accessibilityLabel)
      .accessibilityHint(style == .terminal ? L10n.text("usage.split_bars_help") : "")
  }
}

/// Opaque, subdued tracks keep colored text readable over any menu-bar wallpaper.
/// Neither the glyph color nor its underline changes at a bar's fill boundary.
struct MenuBarIconPalette {
  let text: NSColor
  let track: NSColor
  let fill: NSColor
  let outline: NSColor

  static func terminal(for colorScheme: ColorScheme) -> Self {
    if colorScheme == .dark {
      return Self(text: color(0xB7D3E2), track: color(0x17242E),
        fill: color(0x405464), outline: color(0x8797A1))
    }
    return Self(text: color(0x1F4C65), track: color(0xF0F3F5),
      fill: color(0xACBBC5), outline: color(0x657580))
  }

  private static func color(_ rgb: UInt32) -> NSColor {
    NSColor(srgbRed: CGFloat((rgb >> 16) & 0xFF) / 255,
      green: CGFloat((rgb >> 8) & 0xFF) / 255,
      blue: CGFloat(rgb & 0xFF) / 255, alpha: 1)
  }
}

struct MenuBarUsageBarLayout {
  let outline: NSRect
  let interior: NSRect
  let fiveHour: NSRect
  let weekly: NSRect

  init(bounds: NSRect) {
    outline = bounds.insetBy(dx: 1.5, dy: 2.5)
    interior = outline.insetBy(dx: 0.75, dy: 0.75)
    let laneHeight = (interior.height - 1) / 2
    // The image is not flipped: larger y values are the top of the menu bar.
    fiveHour = NSRect(x: interior.minX, y: interior.midY + 0.5,
      width: interior.width, height: laneHeight)
    weekly = NSRect(x: interior.minX, y: interior.minY,
      width: interior.width, height: laneHeight)
  }

  static func fillRect(in lane: NSRect, fraction: CGFloat) -> NSRect {
    NSRect(x: lane.minX, y: lane.minY,
      width: lane.width * min(max(fraction, 0), 1), height: lane.height)
  }
}

/// Terminal icons retain their original colors; the alternate ring is a template.
enum CodexMenuBarIconRenderer {
  static let size = NSSize(width: 43, height: 22)
  static let circularSize = NSSize(width: 54, height: 22)

  static func image(
    for indicator: MenuBarIndicator,
    style: MenuBarIconStyle = .terminal,
    colorScheme: ColorScheme = .light
  ) -> NSImage {
    switch style {
    case .terminal:
      return terminalImage(for: indicator, colorScheme: colorScheme)
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

  private static func terminalImage(for indicator: MenuBarIndicator, colorScheme: ColorScheme) -> NSImage {
    let palette = MenuBarIconPalette.terminal(for: colorScheme)
    let image = NSImage(size: imageSize(for: .terminal), flipped: false) { rect in
      NSGraphicsContext.current?.shouldAntialias = true

      let layout = MenuBarUsageBarLayout(bounds: rect)
      let outline = NSBezierPath(roundedRect: layout.outline, xRadius: 2.4, yRadius: 2.4)
      palette.track.setFill()
      outline.fill()

      if case .limits = indicator {
        NSGraphicsContext.saveGraphicsState()
        NSBezierPath(roundedRect: layout.interior, xRadius: 1.6, yRadius: 1.6).addClip()
        drawLane(layout.fiveHour, fraction: indicator.fiveHourRemainingFraction, palette: palette)
        drawLane(layout.weekly, fraction: indicator.weeklyRemainingFraction, palette: palette)
        NSGraphicsContext.restoreGraphicsState()

        let divider = NSBezierPath()
        divider.move(to: NSPoint(x: layout.interior.minX, y: layout.interior.midY))
        divider.line(to: NSPoint(x: layout.interior.maxX, y: layout.interior.midY))
        divider.lineWidth = 0.5
        palette.outline.withAlphaComponent(0.5).setStroke()
        divider.stroke()
      }

      outline.lineWidth = 1
      palette.outline.setStroke()
      outline.stroke()

      let text = attributedText(for: indicator, colorScheme: colorScheme)
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
    image.isTemplate = false
    return image
  }

  private static func drawLane(_ lane: NSRect, fraction: CGFloat?, palette: MenuBarIconPalette) {
    guard let fraction else {
      // Missing is not 0%, nor a copy of the other window's remaining allowance.
      NSGraphicsContext.saveGraphicsState()
      NSBezierPath(rect: lane).addClip()
      let hatch = NSBezierPath()
      for x in stride(from: lane.minX - lane.height, through: lane.maxX, by: 4) {
        hatch.move(to: NSPoint(x: x, y: lane.minY))
        hatch.line(to: NSPoint(x: x + lane.height, y: lane.maxY))
      }
      hatch.lineWidth = 0.5
      palette.outline.withAlphaComponent(0.4).setStroke()
      hatch.stroke()
      NSGraphicsContext.restoreGraphicsState()
      return
    }
    guard fraction > 0 else { return }
    palette.fill.setFill()
    NSBezierPath(rect: MenuBarUsageBarLayout.fillRect(in: lane, fraction: fraction)).fill()
  }

  private static func circularImage(for indicator: MenuBarIndicator) -> NSImage {
    let image = NSImage(size: imageSize(for: .circular), flipped: false) { rect in
      NSGraphicsContext.current?.shouldAntialias = true

      let ringRect = NSRect(x: 1.5, y: 2.5, width: 17, height: 17)
      let track = NSBezierPath(ovalIn: ringRect)
      track.lineWidth = 2.1
      NSColor.black.withAlphaComponent(0.24).setStroke()
      track.stroke()

      let progress = indicator.circularRemainingFraction ?? (indicator == .loading ? 0.28 : 0)
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

  static func attributedText(for indicator: MenuBarIndicator, colorScheme: ColorScheme = .light) -> NSAttributedString {
    let textColor = MenuBarIconPalette.terminal(for: colorScheme).text
    let paragraph = NSMutableParagraphStyle()
    paragraph.alignment = .center
    let text = NSMutableAttributedString(
      string: indicator.terminalText,
      attributes: [
        .font: NSFont.monospacedSystemFont(ofSize: 12.6, weight: .semibold),
        .foregroundColor: textColor,
        .paragraphStyle: paragraph,
      ]
    )
    text.addAttributes(
      [
        .underlineStyle: NSUnderlineStyle.single.rawValue,
        .underlineColor: textColor,
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
