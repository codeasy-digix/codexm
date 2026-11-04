import AppKit
import SwiftUI
import Testing

@testable import CodexUsageMenuBar

@Suite("Weekly menu-bar usage icon")
@MainActor
struct MenuBarWeeklyIconTests {
  @Test("Missing windows stay missing, percentages clamp, and the ring retains its single-window fallback")
  func independentFractions() {
    let weeklyOnly = MenuBarIndicator.limits(fiveHour: nil, weekly: 73)
    #expect(weeklyOnly.fiveHourRemainingFraction == nil)
    #expect(weeklyOnly.weeklyRemainingFraction == 0.73)
    #expect(weeklyOnly.text == "73%")
    let fiveHourOnly = MenuBarIndicator.limits(fiveHour: 42, weekly: nil)
    #expect(fiveHourOnly.fiveHourRemainingFraction == 0.42)
    #expect(fiveHourOnly.weeklyRemainingFraction == nil)
    #expect(fiveHourOnly.circularRemainingFraction == 0.42)
    let outOfBounds = MenuBarIndicator.limits(fiveHour: -10, weekly: 150)
    #expect(outOfBounds.fiveHourRemainingFraction == 0)
    #expect(outOfBounds.weeklyRemainingFraction == 1)
    for state in [MenuBarIndicator.loading, .unavailable, .limits(fiveHour: nil, weekly: nil)] {
      #expect(state.fiveHourRemainingFraction == nil)
      #expect(state.weeklyRemainingFraction == nil)
    }
  }

  @Test("The weekly bar occupies the full interior and drains from the right")
  func barGeometry() {
    let layout = MenuBarUsageBarLayout(bounds: NSRect(origin: .zero, size: CodexMenuBarIconRenderer.size))
    #expect(layout.outline.contains(layout.interior))
    #expect(layout.interior.height == layout.outline.height - 1.5)
    let fill = MenuBarUsageBarLayout.fillRect(in: layout.interior, fraction: 0.64)
    #expect(fill.minX == layout.interior.minX)
    #expect(fill.minY == layout.interior.minY)
    #expect(fill.height == layout.interior.height)
    #expect(abs(fill.width / layout.interior.width - 0.64) < 0.001)
    #expect(MenuBarUsageBarLayout.fillRect(in: layout.interior, fraction: -1).width == 0)
    #expect(MenuBarUsageBarLayout.fillRect(in: layout.interior, fraction: 2) == layout.interior)
  }

  @Test("Bold text and underline have at least 6:1 contrast against the orange fill and empty track",
    arguments: [ColorScheme.light, .dark])
  func readablePalette(_ scheme: ColorScheme) throws {
    let palette = MenuBarIconPalette.terminal(for: scheme)
    for background in [palette.weeklyFill, palette.track] {
      #expect(contrast(palette.text, background) >= 6)
      #expect(background.alphaComponent == 1)
    }
    #expect(palette.track.alphaComponent == 1)
    let text = CodexMenuBarIconRenderer.attributedText(for: .limits(fiveHour: 82, weekly: 20), colorScheme: scheme)
    #expect(text.attribute(.foregroundColor, at: 0, effectiveRange: nil) as? NSColor == palette.text)
    #expect(text.attribute(.underlineColor, at: 0, effectiveRange: nil) as? NSColor == palette.text)
    let font = try #require(text.attribute(.font, at: 0, effectiveRange: nil) as? NSFont)
    #expect(NSFontManager.shared.traits(of: font).contains(.boldFontMask))
    #expect(!CodexMenuBarIconRenderer.image(for: .limits(fiveHour: 82, weekly: 20), colorScheme: scheme).isTemplate)
  }

  @Test("Bright orange and dark text stay consistent in both appearances without a horizontal split",
    arguments: [ColorScheme.light, .dark])
  func singleOrangeBar(_ scheme: ColorScheme) throws {
    let palette = MenuBarIconPalette.terminal(for: scheme)
    let fill = rgb(palette.weeklyFill)
    #expect(fill == [1, CGFloat(0xA7) / 255, CGFloat(0x26) / 255])
    let light = MenuBarIconPalette.terminal(for: .light)
    #expect(rgb(palette.weeklyFill) == rgb(light.weeklyFill))
    #expect(rgb(palette.text) == rgb(light.text))
    for percent in [0, 50, 100] {
      let image = try bitmap(.limits(fiveHour: percent, weekly: percent), scheme: scheme)
      #expect(pixel(image, x: 20, y: 18) == pixel(image, x: 20, y: 4))
    }
  }

  @Test("The whole background responds to weekly data only, independently of the displayed 5h number",
    arguments: [ColorScheme.light, .dark])
  func weeklyOnlyBarPixels(_ scheme: ColorScheme) throws {
    let baseline = try bitmap(.limits(fiveHour: 20, weekly: 20), scheme: scheme)
    let changedNumber = try bitmap(.limits(fiveHour: 80, weekly: 20), scheme: scheme)
    let changedWeek = try bitmap(.limits(fiveHour: 20, weekly: 80), scheme: scheme)
    let weeklyOnly = try bitmap(.limits(fiveHour: nil, weekly: 20), scheme: scheme)
    // Sample the bare track just inside the frame, outside the glyph ink.
    for y: CGFloat in [4, 18] {
      #expect(pixel(baseline, x: 20, y: y) == pixel(changedNumber, x: 20, y: y))
      #expect(pixel(baseline, x: 20, y: y) != pixel(changedWeek, x: 20, y: y))
      #expect(pixel(baseline, x: 20, y: y) == pixel(weeklyOnly, x: 20, y: y))
    }
    #expect(MenuBarIndicator.limits(fiveHour: 80, weekly: 20).text == "80%")
    #expect(MenuBarIndicator.limits(fiveHour: nil, weekly: 20).text == "20%")
  }

  @Test("Missing weekly data is visibly unknown, never 0% or a substitute 5h bar",
    arguments: [ColorScheme.light, .dark])
  func missingWeeklyBar(_ scheme: ColorScheme) throws {
    let unknown = try bitmap(.limits(fiveHour: 42, weekly: nil), scheme: scheme)
    let empty = try bitmap(.limits(fiveHour: 42, weekly: 0), scheme: scheme)
    let substituted = try bitmap(.limits(fiveHour: 42, weekly: 42), scheme: scheme)
    #expect(unknown.tiffRepresentation != empty.tiffRepresentation)
    #expect(unknown.tiffRepresentation != substituted.tiffRepresentation)
    #expect(MenuBarIndicator.limits(fiveHour: 42, weekly: nil).text == "42%")
    #expect(MenuBarIndicator.limits(fiveHour: 42, weekly: nil).weeklyRemainingFraction == nil)
  }

  @Test("Glyph strokes stay visible instead of becoming holes across the weekly fill boundary", arguments: [ColorScheme.light, .dark])
  func stableGlyphPixels(_ scheme: ColorScheme) throws {
    let empty = try bitmap(.limits(fiveHour: 82, weekly: 0), scheme: scheme)
    let partial = try bitmap(.limits(fiveHour: 82, weekly: 47), scheme: scheme)
    let full = try bitmap(.limits(fiveHour: 82, weekly: 100), scheme: scheme)
    var coloredPixels = 0
    for y in 0..<empty.pixelsHigh {
      for x in 0..<empty.pixelsWide {
        let first = try #require(empty.colorAt(x: x, y: y))
        // Antialiased strokes legitimately blend with the changing background.
        // Their foreground tone and opaque track must not be cleared or
        // replaced with the weekly fill/track at the transition.
        if hasForegroundTone(first, scheme: scheme) {
          coloredPixels += 1
          #expect(hasForegroundTone(try #require(partial.colorAt(x: x, y: y)), scheme: scheme))
          #expect(hasForegroundTone(try #require(full.colorAt(x: x, y: y)), scheme: scheme))
          #expect(first.alphaComponent > 0.99)
        }
      }
    }
    #expect(coloredPixels >= 20)
  }

  @Test("Hover and accessibility descriptions retain both values and a localized bar legend")
  func barHelp() {
    let indicator = MenuBarIndicator.limits(fiveHour: 18, weekly: 64)
    for language in AppLanguage.allCases {
      L10n.$languageOverride.withValue(language) {
        #expect(indicator.helpText(for: .terminal).contains(indicator.accessibilityLabel))
        #expect(indicator.helpText(for: .terminal).contains(L10n.text("usage.weekly_bar_help")))
        #expect(indicator.helpText(for: .circular) == indicator.accessibilityLabel)
      }
    }
  }

  @Test("The SwiftUI menu-bar label retains original colors and follows the appearance",
    arguments: [ColorScheme.light, .dark])
  func originalColorsInLabel(_ scheme: ColorScheme) throws {
    let content = MenuBarUsageLabel(indicator: .limits(fiveHour: 47, weekly: 53), preferences: AppPreferences())
      .environment(\.colorScheme, scheme)
    let renderer = ImageRenderer(content: content)
    renderer.scale = 2
    let image = try #require(renderer.nsImage)
    #expect(image.size == CodexMenuBarIconRenderer.size)
    let bitmap = try #require(image.tiffRepresentation.flatMap(NSBitmapImageRep.init(data:)))
    var coloredPixels = 0
    for y in 0..<bitmap.pixelsHigh {
      for x in 0..<bitmap.pixelsWide {
        if let color = bitmap.colorAt(x: x, y: y), hasForegroundTone(color, scheme: scheme) {
          coloredPixels += 1
        }
      }
    }
    #expect(coloredPixels >= 20)
  }

  @Test("Produces light and dark comparison previews for partial, full, empty and missing windows")
  func comparisonPreview() throws {
    guard let directory = ProcessInfo.processInfo.environment["CODEX_WEEKLY_ICON_PREVIEW_DIR"] else { return }
    try FileManager.default.createDirectory(atPath: directory, withIntermediateDirectories: true)
    let examples: [(String, MenuBarIndicator)] = [
      ("5h 18% / week 64%", .limits(fiveHour: 18, weekly: 64)),
      ("5h 64% / week 18%", .limits(fiveHour: 64, weekly: 18)),
      ("5h 47% / week 53%", .limits(fiveHour: 47, weekly: 53)),
      ("Both half full", .limits(fiveHour: 50, weekly: 50)),
      ("Both full", .limits(fiveHour: 100, weekly: 100)),
      ("Both empty", .limits(fiveHour: 0, weekly: 0)),
      ("Weekly only", .limits(fiveHour: nil, weekly: 73)),
      ("5h only", .limits(fiveHour: 42, weekly: nil)),
      ("Loading", .loading), ("Unavailable", .unavailable),
    ]
    for scheme in [ColorScheme.light, .dark] {
      let canvas = NSImage(size: NSSize(width: 680, height: examples.count * 100 + 40), flipped: false) { rect in
        (scheme == .dark ? NSColor(srgbRed: 0.1, green: 0.12, blue: 0.15, alpha: 1) : NSColor.white).setFill()
        NSBezierPath(rect: rect).fill()
        for (index, example) in examples.enumerated() {
          let y = rect.height - CGFloat(index + 1) * 100
          NSAttributedString(string: example.0, attributes: [.font: NSFont.systemFont(ofSize: 16),
            .foregroundColor: scheme == .dark ? NSColor.white : NSColor.black])
            .draw(at: NSPoint(x: 20, y: y + 35))
          let image = CodexMenuBarIconRenderer.image(for: example.1, colorScheme: scheme)
          image.draw(in: NSRect(x: 290, y: y + 32, width: 43, height: 22))
          image.draw(in: NSRect(x: 400, y: y + 10, width: 172, height: 88))
        }
        return true
      }
      let bitmap = try #require(canvas.tiffRepresentation.flatMap(NSBitmapImageRep.init(data:)))
      let png = try #require(bitmap.representation(using: .png, properties: [:]))
      try png.write(to: URL(fileURLWithPath: directory).appending(path: "weekly-\(scheme == .dark ? "dark" : "light").png"))
    }

    let swatch = NSImage(size: NSSize(width: 320, height: 106), flipped: false) { _ in
      for (index, scheme) in [ColorScheme.light, .dark].enumerated() {
        let x = CGFloat(index) * 160
        (scheme == .dark ? NSColor(srgbRed: 0.1, green: 0.12, blue: 0.15, alpha: 1) : NSColor.white).setFill()
        NSBezierPath(rect: NSRect(x: x, y: 0, width: 160, height: 106)).fill()
        let image = CodexMenuBarIconRenderer.image(for: .limits(fiveHour: 18, weekly: 64), colorScheme: scheme)
        image.draw(in: NSRect(x: x + 15.5, y: 12, width: 129, height: 66))
        NSAttributedString(string: scheme == .dark ? "Dark" : "Light",
          attributes: [.font: NSFont.systemFont(ofSize: 12),
            .foregroundColor: scheme == .dark ? NSColor.white : NSColor.black])
          .draw(at: NSPoint(x: x + 15.5, y: 83))
      }
      return true
    }
    let bitmap = try #require(swatch.tiffRepresentation.flatMap(NSBitmapImageRep.init(data:)))
    let png = try #require(bitmap.representation(using: .png, properties: [:]))
    try png.write(to: URL(fileURLWithPath: directory).appending(path: "swatch.png"))
  }

  private func bitmap(_ indicator: MenuBarIndicator, scheme: ColorScheme) throws -> NSBitmapImageRep {
    let image = CodexMenuBarIconRenderer.image(for: indicator, colorScheme: scheme)
    // An explicit Retina sRGB surface avoids the color profile and 1x glyph
    // antialiasing of NSImage's default TIFF representation.
    let scale: CGFloat = 2
    let colorSpace = try #require(CGColorSpace(name: CGColorSpace.sRGB))
    let context = try #require(CGContext(data: nil, width: Int(image.size.width * scale),
      height: Int(image.size.height * scale), bitsPerComponent: 8, bytesPerRow: 0,
      space: colorSpace,
      bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
    context.scaleBy(x: scale, y: scale)
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(cgContext: context, flipped: false)
    image.draw(in: NSRect(origin: .zero, size: image.size))
    NSGraphicsContext.restoreGraphicsState()
    return NSBitmapImageRep(cgImage: try #require(context.makeImage()))
  }

  private func pixel(_ bitmap: NSBitmapImageRep, x: CGFloat, y: CGFloat) -> [CGFloat] {
    let scaleX = CGFloat(bitmap.pixelsWide) / CodexMenuBarIconRenderer.size.width
    let scaleY = CGFloat(bitmap.pixelsHigh) / CodexMenuBarIconRenderer.size.height
    return rgb(bitmap.colorAt(x: Int(x * scaleX), y: Int((CodexMenuBarIconRenderer.size.height - y) * scaleY))!)
  }

  private func rgb(_ color: NSColor) -> [CGFloat] {
    let c = color.usingColorSpace(.sRGB)!
    return [c.redComponent, c.greenComponent, c.blueComponent]
  }

  private func hasForegroundTone(_ color: NSColor, scheme: ColorScheme) -> Bool {
    guard color.alphaComponent > 0.99 else { return false }
    let values = rgb(color)
    let foreground = rgb(MenuBarIconPalette.terminal(for: scheme).text)
    return zip(values, foreground).allSatisfy { abs($0 - $1) < 0.2 }
  }

  private func contrast(_ first: NSColor, _ second: NSColor) -> Double {
    func luminance(_ color: NSColor) -> Double {
      let values = rgb(color).map(Double.init).map { $0 <= 0.04045 ? $0 / 12.92 : pow(($0 + 0.055) / 1.055, 2.4) }
      return values[0] * 0.2126 + values[1] * 0.7152 + values[2] * 0.0722
    }
    let a = luminance(first), b = luminance(second)
    return (max(a, b) + 0.05) / (min(a, b) + 0.05)
  }
}
