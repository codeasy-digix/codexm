import AppKit
import Foundation

struct CodexRuntime: @unchecked Sendable {
  let executableURL: URL
  let environment: [String: String]
  let codexHomeURL: URL

  init(
    executableURL: URL,
    environment: [String: String],
    codexHomeURL: URL
  ) {
    self.executableURL = executableURL
    self.environment = environment
    self.codexHomeURL = codexHomeURL
  }
}

final class SecurityScopedAccess: @unchecked Sendable {
  let url: URL
  private let isAccessing: Bool

  init(url: URL) {
    self.url = url
    self.isAccessing = url.startAccessingSecurityScopedResource()
  }

  deinit {
    if isAccessing {
      url.stopAccessingSecurityScopedResource()
    }
  }
}

/// A read-only lifetime token for inspecting the machine's existing Codex
/// identity. It is deliberately separate from `CodexRuntime`, so the system
/// `~/.codex` directory can never be passed to an app-server process.
struct CodexHomeIdentityAccess: @unchecked Sendable {
  let codexHomeURL: URL
  private let securityScopedAccess: SecurityScopedAccess?

  init(
    codexHomeURL: URL,
    securityScopedAccess: SecurityScopedAccess? = nil
  ) {
    self.codexHomeURL = codexHomeURL
    self.securityScopedAccess = securityScopedAccess
  }
}

enum CodexRuntimeError: LocalizedError, Equatable {
  case bundledRuntimeMissing
  case invalidCodexHome
  case defaultCodexHomeUnavailable

  var errorDescription: String? {
    switch self {
    case .bundledRuntimeMissing:
      return "앱에 포함된 Codex 런타임을 찾을 수 없습니다."
    case .invalidCodexHome:
      return "선택한 폴더에서 Codex 로그인 정보를 찾을 수 없습니다."
    case .defaultCodexHomeUnavailable:
      return "기본 Codex 로그인 정보를 찾을 수 없습니다."
    }
  }
}

struct CodexRuntimeLocator: @unchecked Sendable {
  static let bookmarkKey = "CodexHomeSecurityScopedBookmark"

  private let fileManager: FileManager
  private let defaults: UserDefaults
  private let environment: [String: String]
  private let runtimeURLOverride: URL?
  private let systemCodexHomeURL: URL

  init(
    fileManager: FileManager = .default,
    defaults: UserDefaults = .standard,
    environment: [String: String] = ProcessInfo.processInfo.environment,
    runtimeURLOverride: URL? = nil,
    systemCodexHomeURL: URL? = nil
  ) {
    self.fileManager = fileManager
    self.defaults = defaults
    self.environment = environment
    self.runtimeURLOverride = runtimeURLOverride
    self.systemCodexHomeURL =
      systemCodexHomeURL
      ?? fileManager.homeDirectoryForCurrentUser.appending(
        path: ".codex",
        directoryHint: .isDirectory
      )
  }

  func locateSystemCodexHomeIdentity() throws -> CodexHomeIdentityAccess {
    if isAuthenticatedCodexHome(systemCodexHomeURL) {
      return CodexHomeIdentityAccess(codexHomeURL: systemCodexHomeURL)
    }
    if let selection = selectedCodexHome() {
      return CodexHomeIdentityAccess(
        codexHomeURL: selection.url,
        securityScopedAccess: selection.access
      )
    }
    throw CodexRuntimeError.defaultCodexHomeUnavailable
  }

  func locateManagedAccount(codexHomeURL: URL) throws -> CodexRuntime {
    try fileManager.createDirectory(
      at: codexHomeURL,
      withIntermediateDirectories: true,
      attributes: [.posixPermissions: 0o700]
    )
    return try makeRuntime(codexHomeURL: codexHomeURL)
  }

  private func makeRuntime(codexHomeURL: URL) throws -> CodexRuntime {
    let executableURL = try bundledRuntimeURL()
    var runtimeEnvironment = environment
    runtimeEnvironment["CODEX_HOME"] = codexHomeURL.path
    runtimeEnvironment["PATH"] = "/usr/bin:/bin:/usr/sbin:/sbin"

    return CodexRuntime(
      executableURL: executableURL,
      environment: runtimeEnvironment,
      codexHomeURL: codexHomeURL
    )
  }

  func saveCodexHomeAccess(_ url: URL) throws {
    guard isAuthenticatedCodexHome(url) else {
      throw CodexRuntimeError.invalidCodexHome
    }

    let bookmark = try url.bookmarkData(
      options: [.withSecurityScope, .securityScopeAllowOnlyReadAccess],
      includingResourceValuesForKeys: nil,
      relativeTo: nil
    )
    defaults.set(bookmark, forKey: Self.bookmarkKey)
  }

  func clearCodexHomeAccess() {
    defaults.removeObject(forKey: Self.bookmarkKey)
  }

  var suggestedCodexHomeURL: URL { systemCodexHomeURL }

  private func bundledRuntimeURL() throws -> URL {
    let url =
      runtimeURLOverride
      ?? Bundle.main.bundleURL.appending(
        path: "Contents/MacOS/CodexRuntime",
        directoryHint: .notDirectory
      )
    guard fileManager.isExecutableFile(atPath: url.path) else {
      throw CodexRuntimeError.bundledRuntimeMissing
    }
    return url
  }

  private func isAuthenticatedCodexHome(_ url: URL) -> Bool {
    fileManager.isReadableFile(atPath: url.appending(path: "auth.json").path)
  }

  private func selectedCodexHome() -> (url: URL, access: SecurityScopedAccess)? {
    guard let data = defaults.data(forKey: Self.bookmarkKey) else { return nil }
    var isStale = false
    guard
      let url = try? URL(
        resolvingBookmarkData: data,
        options: [.withSecurityScope, .withoutUI],
        relativeTo: nil,
        bookmarkDataIsStale: &isStale
      )
    else {
      clearCodexHomeAccess()
      return nil
    }

    let access = SecurityScopedAccess(url: url)
    guard isAuthenticatedCodexHome(url) else {
      clearCodexHomeAccess()
      return nil
    }
    if isStale {
      try? saveCodexHomeAccess(url)
    }
    return (url, access)
  }
}
