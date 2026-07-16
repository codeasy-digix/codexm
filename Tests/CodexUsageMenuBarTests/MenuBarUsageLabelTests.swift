import AppKit
import SwiftUI
import Testing

@testable import CodexUsageMenuBar

@Suite("Menu bar usage label")
@MainActor
struct MenuBarUsageLabelTests {
  @Test("Shows the remaining percentage after a terminal prompt")
  func remainingPercentageText() {
    #expect(MenuBarIndicator.remaining(18).text == "18%")
    #expect(MenuBarIndicator.remaining(18).terminalText == ">18%")
    #expect(MenuBarIndicator.remaining(100).text == "100%")
    #expect(MenuBarIndicator.remaining(-2).text == "0%")
  }

  @Test("Underlines the terminal prompt and usage as one continuous value")
  func terminalPromptUnderline() throws {
    let text = CodexMenuBarIconRenderer.attributedText(for: .remaining(18))
    let underlineRange = MenuBarIndicator.remaining(18).terminalUnderlineRange

    #expect(text.string == ">18%")
    #expect(
      text.attribute(.underlineStyle, at: 0, effectiveRange: nil) as? Int
        == NSUnderlineStyle.single.rawValue
    )
    #expect(underlineRange == NSRange(location: 0, length: 4))
  }

  @Test("Uses the enlarged font for the prompt and the percentage")
  func enlargedPromptAndPercentage() throws {
    let text = CodexMenuBarIconRenderer.attributedText(for: .remaining(100))
    let promptFont = try #require(text.attribute(.font, at: 0, effectiveRange: nil) as? NSFont)
    let percentageFont = try #require(text.attribute(.font, at: 1, effectiveRange: nil) as? NSFont)

    #expect(abs(promptFont.pointSize - 12.6) < 0.001)
    #expect(abs(percentageFont.pointSize - 12.6) < 0.001)
    #expect(text.size().width <= CodexMenuBarIconRenderer.size.width - 2)
  }

  @Test("Renders the compact Codex-inspired indicator")
  func rendersIndicator() throws {
    let nativeImage = CodexMenuBarIconRenderer.image(for: .remaining(18))
    #expect(nativeImage.isTemplate)
    #expect(nativeImage.size == CodexMenuBarIconRenderer.size)

    let content = MenuBarUsageLabel(indicator: .remaining(18))
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

}
