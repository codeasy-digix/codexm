import AppKit
import SwiftUI
import Testing

@testable import CodexUsageMenuBar

@Suite("Menu bar usage label")
@MainActor
struct MenuBarUsageLabelTests {
  @Test("Shows the five-hour percentage while retaining weekly fill")
  func dualLimitText() {
    let indicator = MenuBarIndicator.limits(fiveHour: 18, weekly: 64)
    #expect(indicator.text == "18%")
    #expect(indicator.terminalText == ">18%")
    #expect(indicator.weeklyRemainingFraction == 0.64)
    #expect(MenuBarIndicator.limits(fiveHour: 100, weekly: 50).text == "100%")
    #expect(MenuBarIndicator.limits(fiveHour: -2, weekly: 50).text == "0%")
    #expect(MenuBarIndicator.limits(fiveHour: nil, weekly: 73).text == "73%")
  }

  @Test("Underlines the terminal prompt and usage as one continuous value")
  func terminalPromptUnderline() throws {
    let indicator = MenuBarIndicator.limits(fiveHour: 18, weekly: 64)
    let text = CodexMenuBarIconRenderer.attributedText(for: indicator)
    let underlineRange = indicator.terminalUnderlineRange

    #expect(text.string == ">18%")
    #expect(
      text.attribute(.underlineStyle, at: 0, effectiveRange: nil) as? Int
        == NSUnderlineStyle.single.rawValue
    )
    #expect(underlineRange == NSRange(location: 0, length: 4))
  }

  @Test("Uses the enlarged font for the prompt and the percentage")
  func enlargedPromptAndPercentage() throws {
    let text = CodexMenuBarIconRenderer.attributedText(
      for: .limits(fiveHour: 100, weekly: 50)
    )
    let promptFont = try #require(text.attribute(.font, at: 0, effectiveRange: nil) as? NSFont)
    let percentageFont = try #require(text.attribute(.font, at: 1, effectiveRange: nil) as? NSFont)

    #expect(abs(promptFont.pointSize - 12.6) < 0.001)
    #expect(abs(percentageFont.pointSize - 12.6) < 0.001)
    #expect(text.size().width <= CodexMenuBarIconRenderer.size.width - 2)
  }

  @Test("Renders the compact Codex-inspired indicator")
  func rendersIndicator() throws {
    let nativeImage = CodexMenuBarIconRenderer.image(
      for: .limits(fiveHour: 18, weekly: 64)
    )
    #expect(nativeImage.isTemplate)
    #expect(nativeImage.size == CodexMenuBarIconRenderer.size)

    let content = MenuBarUsageLabel(indicator: .limits(fiveHour: 18, weekly: 64))
      .padding(10)
      .background(Color.white)
      .environment(\.colorScheme, .light)
    let renderer = ImageRenderer(content: content)
    renderer.scale = 2

    let image = try #require(renderer.nsImage)
    #expect(image.size.width == 63)
    #expect(image.size.height == 42)

    if let outputPath = ProcessInfo.processInfo.environment["CODEX_ICON_PREVIEW_PATH"],
      let tiff = image.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiff),
      let png = bitmap.representation(using: .png, properties: [:])
    {
      try png.write(to: URL(fileURLWithPath: outputPath))
    }
  }

  @Test("Changes the terminal battery fill without adding another number")
  func rendersWeeklyBatteryFill() throws {
    let low = CodexMenuBarIconRenderer.image(
      for: .limits(fiveHour: 82, weekly: 20)
    )
    let high = CodexMenuBarIconRenderer.image(
      for: .limits(fiveHour: 82, weekly: 80)
    )

    #expect(CodexMenuBarIconRenderer.attributedText(
      for: .limits(fiveHour: 82, weekly: 20)
    ).string == ">82%")
    #expect(low.tiffRepresentation != high.tiffRepresentation)
  }

  @Test("Keeps the battery capacity outline when weekly usage is empty")
  func rendersEmptyWeeklyBatteryOutline() throws {
    let nativeImage = CodexMenuBarIconRenderer.image(
      for: .limits(fiveHour: 82, weekly: 0)
    )

    #expect(nativeImage.isTemplate)
    #expect(nativeImage.tiffRepresentation != nil)

    let content = MenuBarUsageLabel(indicator: .limits(fiveHour: 82, weekly: 0))
      .padding(10)
      .background(Color.white)
      .environment(\.colorScheme, .light)
    let renderer = ImageRenderer(content: content)
    renderer.scale = 2
    let image = try #require(renderer.nsImage)

    if let outputPath = ProcessInfo.processInfo.environment["CODEX_EMPTY_BATTERY_PREVIEW_PATH"],
      let tiff = image.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiff),
      let png = bitmap.representation(using: .png, properties: [:])
    {
      try png.write(to: URL(fileURLWithPath: outputPath))
    }
  }

  @Test("Renders a circular remaining-usage chart with the percentage")
  func rendersCircularIndicator() throws {
    let indicator = MenuBarIndicator.limits(fiveHour: 18, weekly: 64)
    let nativeImage = CodexMenuBarIconRenderer.image(for: indicator, style: .circular)
    #expect(nativeImage.isTemplate)
    #expect(nativeImage.size == CodexMenuBarIconRenderer.circularSize)
    #expect(indicator.weeklyRemainingFraction == 0.64)
    #expect(CodexMenuBarIconRenderer.circularAttributedText(for: indicator).string == "18%")

    let content = MenuBarUsageLabel(indicator: indicator, style: .circular)
      .padding(10)
      .background(Color.white)
      .environment(\.colorScheme, .light)
    let renderer = ImageRenderer(content: content)
    renderer.scale = 2

    let image = try #require(renderer.nsImage)
    #expect(image.size.width == 74)
    #expect(image.size.height == 42)

    if let outputPath = ProcessInfo.processInfo.environment["CODEX_CIRCULAR_ICON_PREVIEW_PATH"],
      let tiff = image.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiff),
      let png = bitmap.representation(using: .png, properties: [:])
    {
      try png.write(to: URL(fileURLWithPath: outputPath))
    }
  }

}
