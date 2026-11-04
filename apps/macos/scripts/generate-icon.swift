#!/usr/bin/env swift

import AppKit
import Foundation

guard CommandLine.arguments.count == 2 else {
  FileHandle.standardError.write(Data("usage: generate-icon.swift OUTPUT.icns\n".utf8))
  exit(2)
}

let outputURL = URL(fileURLWithPath: CommandLine.arguments[1])
let fileManager = FileManager.default
let iconsetURL = fileManager.temporaryDirectory
  .appending(path: "CodexUsage-\(UUID().uuidString).iconset", directoryHint: .isDirectory)

try fileManager.createDirectory(at: iconsetURL, withIntermediateDirectories: true)
defer { try? fileManager.removeItem(at: iconsetURL) }

let entries: [(name: String, pixels: Int)] = [
  ("icon_16x16.png", 16),
  ("icon_16x16@2x.png", 32),
  ("icon_32x32.png", 32),
  ("icon_32x32@2x.png", 64),
  ("icon_128x128.png", 128),
  ("icon_128x128@2x.png", 256),
  ("icon_256x256.png", 256),
  ("icon_256x256@2x.png", 512),
  ("icon_512x512.png", 512),
  ("icon_512x512@2x.png", 1024),
]

for entry in entries {
  let size = CGFloat(entry.pixels)
  let image = NSImage(size: NSSize(width: size, height: size))
  image.lockFocus()

  let bounds = NSRect(x: 0, y: 0, width: size, height: size)
  let inset = size * 0.055
  let background = NSBezierPath(
    roundedRect: bounds.insetBy(dx: inset, dy: inset),
    xRadius: size * 0.22,
    yRadius: size * 0.22
  )
  NSGradient(
    starting: NSColor(calibratedRed: 0.09, green: 0.11, blue: 0.16, alpha: 1),
    ending: NSColor(calibratedRed: 0.12, green: 0.47, blue: 0.39, alpha: 1)
  )?.draw(in: background, angle: -35)

  let barWidth = size * 0.105
  let gap = size * 0.075
  let totalWidth = barWidth * 4 + gap * 3
  let startX = (size - totalWidth) / 2
  let bottom = size * 0.25
  let heights = [0.22, 0.34, 0.47, 0.58].map { size * $0 }

  for (index, height) in heights.enumerated() {
    let rect = NSRect(
      x: startX + CGFloat(index) * (barWidth + gap),
      y: bottom,
      width: barWidth,
      height: height
    )
    let bar = NSBezierPath(
      roundedRect: rect,
      xRadius: barWidth / 2,
      yRadius: barWidth / 2
    )
    NSColor.white.withAlphaComponent(index == heights.count - 1 ? 1 : 0.83).setFill()
    bar.fill()
  }

  image.unlockFocus()

  guard
    let tiff = image.tiffRepresentation,
    let bitmap = NSBitmapImageRep(data: tiff),
    let png = bitmap.representation(using: .png, properties: [:])
  else {
    throw NSError(domain: "CodexUsageIcon", code: 1)
  }

  try png.write(to: iconsetURL.appending(path: entry.name))
}

try fileManager.createDirectory(
  at: outputURL.deletingLastPathComponent(),
  withIntermediateDirectories: true
)

let iconutil = Process()
iconutil.executableURL = URL(fileURLWithPath: "/usr/bin/iconutil")
iconutil.arguments = ["--convert", "icns", "--output", outputURL.path, iconsetURL.path]
try iconutil.run()
iconutil.waitUntilExit()

guard iconutil.terminationStatus == 0 else {
  throw NSError(domain: "CodexUsageIcon", code: Int(iconutil.terminationStatus))
}
