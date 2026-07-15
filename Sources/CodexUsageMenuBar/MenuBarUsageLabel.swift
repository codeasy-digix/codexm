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
    ZStack {
      CodexCloudOutline()
        .strokeBorder(
          style: StrokeStyle(lineWidth: 1.35, lineCap: .round, lineJoin: .round)
        )

      Text(indicator.text)
        .font(.system(size: 9, weight: .bold, design: .rounded))
        .monospacedDigit()
        .minimumScaleFactor(0.75)
        .lineLimit(1)
        .padding(.horizontal, 5)
    }
    .frame(width: 42, height: 22)
    .foregroundStyle(.primary)
    .accessibilityElement(children: .ignore)
    .accessibilityLabel(indicator.accessibilityLabel)
  }
}

/// A compact looped outline inspired by Codex's rounded, interwoven mark.
/// It remains a simple original outline so the percentage stays legible at
/// macOS menu bar sizes.
struct CodexCloudOutline: InsettableShape {
  var insetAmount: CGFloat = 0

  func path(in rect: CGRect) -> Path {
    let drawingRect = rect.insetBy(dx: insetAmount + 1.5, dy: insetAmount + 1.5)
    let center = CGPoint(x: drawingRect.midX, y: drawingRect.midY)
    let radiusX = drawingRect.width / 2
    let radiusY = drawingRect.height / 2
    let sampleCount = 120

    var path = Path()
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
        path.addLine(to: point)
      }
    }
    path.closeSubpath()
    return path
  }

  func inset(by amount: CGFloat) -> CodexCloudOutline {
    var shape = self
    shape.insetAmount += amount
    return shape
  }
}
