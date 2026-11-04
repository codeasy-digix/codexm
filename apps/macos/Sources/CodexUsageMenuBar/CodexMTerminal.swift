import AppKit
import Foundation

enum CodexMTerminal {
  static func shellQuote(_ value: String) -> String { "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'" }
  static func appleScriptQuote(_ value: String) -> String {
    "\"" + value.replacingOccurrences(of: "\\", with: "\\\\")
      .replacingOccurrences(of: "\"", with: "\\\"")
      .replacingOccurrences(of: "\n", with: "\\n")
      .replacingOccurrences(of: "\r", with: "\\r") + "\""
  }
  @MainActor static func open() throws {
    let binary = try CodexMLocator().locate().path
    let command = "cd " + shellQuote(FileManager.default.homeDirectoryForCurrentUser.path)
      + " && " + shellQuote(binary)
    let script = "tell application \"Terminal\"\nactivate\ndo script " + appleScriptQuote(command) + "\nend tell"
    var error: NSDictionary?
    NSAppleScript(source: script)?.executeAndReturnError(&error)
    if error != nil { throw CodexMBridgeError.rejected(L10n.text("codexm.terminal_failed")) }
  }
}
