import AppKit
import SwiftUI
import Testing

@testable import CodexUsageMenuBar

@Suite("Dual usage ring")
@MainActor
struct DualUsageRingTests {
  @Test("Renders distinct five-hour and weekly remaining percentages")
  func rendersBothLimits() throws {
    let content = DualUsageRing(
      fiveHourLimit: UsageLimitWindow(
        usedPercent: 10,
        windowDurationMinutes: 300,
        resetsAt: nil
      ),
      weeklyLimit: UsageLimitWindow(
        usedPercent: 36,
        windowDurationMinutes: 10_080,
        resetsAt: nil
      )
    )
    .frame(width: 98, height: 98)
    .padding(12)
    .background(Color.white)
    .environment(\.colorScheme, .light)

    let renderer = ImageRenderer(content: content)
    renderer.scale = 2
    let image = try #require(renderer.nsImage)

    #expect(image.size == NSSize(width: 122, height: 122))

    if let outputPath = ProcessInfo.processInfo.environment["CODEX_DUAL_RING_PREVIEW_PATH"],
      let tiff = image.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiff),
      let png = bitmap.representation(using: .png, properties: [:])
    {
      try png.write(to: URL(fileURLWithPath: outputPath))
    }
  }
}
