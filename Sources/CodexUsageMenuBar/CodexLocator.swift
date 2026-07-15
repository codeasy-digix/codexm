import Foundation

struct CodexLocator: @unchecked Sendable {
  private let fileManager: FileManager
  private let environment: [String: String]
  private let homeDirectory: URL

  init(
    fileManager: FileManager = .default,
    environment: [String: String] = ProcessInfo.processInfo.environment,
    homeDirectory: URL = FileManager.default.homeDirectoryForCurrentUser
  ) {
    self.fileManager = fileManager
    self.environment = environment
    self.homeDirectory = homeDirectory
  }

  func locate() -> URL? {
    for candidate in directCandidates() where fileManager.isExecutableFile(atPath: candidate.path) {
      return candidate.resolvingSymlinksInPath()
    }

    for candidate in versionManagerCandidates()
    where fileManager.isExecutableFile(atPath: candidate.path) {
      return candidate.resolvingSymlinksInPath()
    }

    return nil
  }

  private func directCandidates() -> [URL] {
    var paths: [String] = []

    if let override = environment["CODEX_CLI_PATH"], !override.isEmpty {
      paths.append(override)
    }

    if let path = environment["PATH"] {
      paths.append(contentsOf: path.split(separator: ":").map { "\($0)/codex" })
    }

    paths.append(contentsOf: [
      "/opt/homebrew/bin/codex",
      "/usr/local/bin/codex",
      homeDirectory.appending(path: ".local/bin/codex").path,
      homeDirectory.appending(path: ".volta/bin/codex").path,
      homeDirectory.appending(path: ".bun/bin/codex").path,
      homeDirectory.appending(path: ".asdf/shims/codex").path,
    ])

    var seen = Set<String>()
    return paths.compactMap { path in
      guard seen.insert(path).inserted else { return nil }
      return URL(fileURLWithPath: path)
    }
  }

  private func versionManagerCandidates() -> [URL] {
    let roots = [
      homeDirectory.appending(path: ".nvm/versions/node"),
      homeDirectory.appending(path: ".local/share/mise/installs/node"),
      homeDirectory.appending(path: ".asdf/installs/nodejs"),
    ]

    return roots.flatMap { root in
      let versions =
        (try? fileManager.contentsOfDirectory(
          at: root,
          includingPropertiesForKeys: nil,
          options: [.skipsHiddenFiles]
        )) ?? []

      return
        versions
        .sorted { lhs, rhs in
          lhs.lastPathComponent.compare(
            rhs.lastPathComponent,
            options: [.numeric, .caseInsensitive]
          ) == .orderedDescending
        }
        .map { $0.appending(path: "bin/codex") }
    }
  }
}
