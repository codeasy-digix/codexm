import AppKit
import SwiftUI
import UniformTypeIdentifiers

extension UTType {
  static let codexUsageAccountOrder = UTType(
    exportedAs: "org.codeasy.CodexUsage.account-order", conformingTo: .data
  )
}

struct AccountDragHandle: View {
  let accountID: String
  let label: String
  let onBegin: () -> Void
  let onEnd: () -> Void

  var body: some View {
    Image(systemName: "line.3.horizontal")
      .font(.system(size: 11, weight: .semibold))
      .foregroundStyle(.tertiary)
      .frame(width: 18, height: 20)
      .overlay {
        AccountDragSource(accountID: accountID, label: label, onBegin: onBegin, onEnd: onEnd)
      }
      .help(L10n.text("account.drag_help"))
      .accessibilityLabel(L10n.text("account.drag_label", label))
  }
}

private struct AccountDragSource: NSViewRepresentable {
  let accountID: String
  let label: String
  let onBegin: () -> Void
  let onEnd: () -> Void

  func makeNSView(context: Context) -> AccountDragSourceView { AccountDragSourceView() }

  func updateNSView(_ view: AccountDragSourceView, context: Context) {
    view.accountID = accountID
    view.label = label
    view.onBegin = onBegin
    view.onEnd = onEnd
  }
}

private final class AccountDragSourceView: NSView, NSDraggingSource {
  var accountID = ""
  var label = ""
  var onBegin: (() -> Void)?
  var onEnd: (() -> Void)?
  private var initialEvent: NSEvent?

  override func resetCursorRects() { addCursorRect(bounds, cursor: .openHand) }
  override func mouseDown(with event: NSEvent) { initialEvent = event }
  override func mouseUp(with event: NSEvent) { initialEvent = nil }

  override func mouseDragged(with event: NSEvent) {
    guard let initialEvent,
      hypot(
        event.locationInWindow.x - initialEvent.locationInWindow.x,
        event.locationInWindow.y - initialEvent.locationInWindow.y
      ) >= 3
    else { return }
    self.initialEvent = nil
    let pasteboardItem = NSPasteboardItem()
    pasteboardItem.setString(
      accountID, forType: NSPasteboard.PasteboardType(UTType.codexUsageAccountOrder.identifier)
    )
    let item = NSDraggingItem(pasteboardWriter: pasteboardItem)
    let preview = NSImage(size: NSSize(width: 220, height: 30), flipped: false) { rect in
      NSColor.controlBackgroundColor.setFill()
      NSBezierPath(roundedRect: rect, xRadius: 7, yRadius: 7).fill()
      (self.label as NSString).draw(
        in: rect.insetBy(dx: 10, dy: 7),
        withAttributes: [
          .font: NSFont.systemFont(ofSize: 11, weight: .semibold),
          .foregroundColor: NSColor.labelColor,
        ]
      )
      return true
    }
    let point = convert(event.locationInWindow, from: nil)
    item.setDraggingFrame(
      NSRect(x: point.x - 200, y: point.y - 15, width: 220, height: 30), contents: preview
    )
    onBegin?()
    beginDraggingSession(with: [item], event: event, source: self)
  }

  func draggingSession(
    _ session: NSDraggingSession, sourceOperationMaskFor context: NSDraggingContext
  ) -> NSDragOperation {
    context == .withinApplication ? .move : []
  }

  func ignoreModifierKeys(for session: NSDraggingSession) -> Bool { true }

  func draggingSession(
    _ session: NSDraggingSession, endedAt screenPoint: NSPoint, operation: NSDragOperation
  ) {
    // Native completion also runs for Escape and drops outside the panel.
    onEnd?()
  }
}

struct AccountCardDropDelegate: DropDelegate {
  let targetID: String
  @Binding var draggedAccountID: String?
  @Binding var previewOrder: [String]
  let onCommit: ([String]) -> Bool

  func validateDrop(info: DropInfo) -> Bool {
    draggedAccountID != nil && info.hasItemsConforming(to: [.codexUsageAccountOrder])
  }

  func dropEntered(info: DropInfo) {
    guard let draggedAccountID else { return }
    let nextOrder = AccountOrder.moving(draggedAccountID, to: targetID, in: previewOrder)
    if nextOrder != previewOrder {
      withAnimation(.easeInOut(duration: 0.16)) { previewOrder = nextOrder }
    }
  }

  func dropUpdated(info: DropInfo) -> DropProposal? { DropProposal(operation: .move) }

  func performDrop(info: DropInfo) -> Bool {
    guard validateDrop(info: info) else { return false }
    return onCommit(previewOrder)
  }
}
