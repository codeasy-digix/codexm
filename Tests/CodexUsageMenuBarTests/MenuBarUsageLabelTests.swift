import AppKit
import SwiftUI
import Testing

@testable import CodexUsageMenuBar

@Suite("Menu bar usage label")
@MainActor
struct MenuBarUsageLabelTests {
  @Test("Shows the remaining percentage inside the outline")
  func remainingPercentageText() {
    #expect(MenuBarIndicator.remaining(18).text == "18%")
    #expect(MenuBarIndicator.remaining(100).text == "100%")
    #expect(MenuBarIndicator.remaining(-2).text == "0%")
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
    #expect(image.size.width == 62)
    #expect(image.size.height == 42)

    if let outputPath = ProcessInfo.processInfo.environment["CODEX_ICON_PREVIEW_PATH"],
      let tiff = image.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiff),
      let png = bitmap.representation(using: .png, properties: [:])
    {
      try png.write(to: URL(fileURLWithPath: outputPath))
    }
  }

  @Test("Keeps the cloud outline inside its menu bar bounds")
  func outlineBounds() {
    let target = CGRect(x: 0, y: 0, width: 42, height: 22)
    let bounds = CodexMenuBarIconRenderer.outlinePath(in: target).bounds
    let strokeInset = CodexMenuBarIconRenderer.outlineLineWidth / 2

    #expect(bounds.minX >= target.minX + strokeInset)
    #expect(bounds.minY >= target.minY + strokeInset)
    #expect(bounds.maxX <= target.maxX - strokeInset)
    #expect(bounds.maxY <= target.maxY - strokeInset)
  }
}
