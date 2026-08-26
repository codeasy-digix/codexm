import Foundation

enum UsageAccountKind: String, Codable, Equatable, Sendable {
  case systemDefault
  case managed
}

struct UsageAccount: Codable, Equatable, Identifiable, Sendable {
  static let systemDefaultID = "system-default"

  let id: String
  let kind: UsageAccountKind
  var displayName: String?
  var lastKnownEmail: String?
  var lastKnownPlanType: String?
  let createdAt: Date

  static var systemDefault: UsageAccount {
    UsageAccount(
      id: systemDefaultID,
      kind: .systemDefault,
      displayName: "기본 계정",
      lastKnownEmail: nil,
      lastKnownPlanType: nil,
      createdAt: .distantPast
    )
  }

  var isSystemDefault: Bool { kind == .systemDefault }
  var isManaged: Bool { kind == .managed }

  var title: String {
    let trimmedName = displayName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    if !trimmedName.isEmpty { return trimmedName }
    if let email = lastKnownEmail, !email.isEmpty { return email }
    return isSystemDefault ? "기본 계정" : "추가 계정"
  }
}

enum UsageAccountRegistryError: LocalizedError, Equatable {
  case invalidAccountIdentifier
  case pendingAccountMissing
  case managedAccountMissing
  case unsafeManagedPath

  var errorDescription: String? {
    switch self {
    case .invalidAccountIdentifier:
      return "계정 식별자가 올바르지 않습니다."
    case .pendingAccountMissing:
      return "추가 중인 계정 정보를 찾을 수 없습니다."
    case .managedAccountMissing:
      return "저장된 계정 정보를 찾을 수 없습니다."
    case .unsafeManagedPath:
      return "안전하지 않은 계정 저장 경로입니다."
    }
  }
}

struct UsageAccountRegistry: @unchecked Sendable {
  private struct Payload: Codable {
    let version: Int
    var accounts: [UsageAccount]
  }

  private let fileManager: FileManager
  let applicationSupportURL: URL

  init(
    fileManager: FileManager = .default,
    applicationSupportURL: URL? = nil
  ) {
    self.fileManager = fileManager
    self.applicationSupportURL =
      applicationSupportURL
      ?? fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
      .appending(path: "Codex Usage", directoryHint: .isDirectory)
  }

  var accountsRootURL: URL {
    applicationSupportURL.appending(path: "Accounts", directoryHint: .isDirectory)
  }

  var stagingRootURL: URL {
    applicationSupportURL.appending(path: "Staging", directoryHint: .isDirectory)
  }

  var registryURL: URL {
    applicationSupportURL.appending(path: "accounts.json", directoryHint: .notDirectory)
  }

  func loadAccounts() throws -> [UsageAccount] {
    try prepareRootDirectories()
    try discardAbandonedPendingAccounts()
    try migrateLegacyManagedHomeIfNeeded()
    return [UsageAccount.systemDefault] + (try loadManagedAccounts())
      .sorted { $0.createdAt < $1.createdAt }
  }

  func beginManagedAccount() throws -> UsageAccount {
    try prepareRootDirectories()
    let identifier = UUID().uuidString.lowercased()
    let account = UsageAccount(
      id: identifier,
      kind: .managed,
      displayName: nil,
      lastKnownEmail: nil,
      lastKnownPlanType: nil,
      createdAt: Date()
    )
    let root = try validatedChildURL(root: stagingRootURL, identifier: identifier)
    let codexHome = root.appending(path: "CodexHome", directoryHint: .isDirectory)
    try createPrivateDirectory(at: codexHome)
    try writeManagedConfig(to: codexHome)
    var resourceValues = URLResourceValues()
    resourceValues.isExcludedFromBackup = true
    var accountRoot = root
    try? accountRoot.setResourceValues(resourceValues)
    return account
  }

  func pendingCodexHomeURL(for account: UsageAccount) throws -> URL {
    guard account.isManaged else { throw UsageAccountRegistryError.invalidAccountIdentifier }
    let root = try validatedChildURL(root: stagingRootURL, identifier: account.id)
    return root.appending(path: "CodexHome", directoryHint: .isDirectory)
  }

  func managedCodexHomeURL(for account: UsageAccount) throws -> URL {
    guard account.isManaged else { throw UsageAccountRegistryError.invalidAccountIdentifier }
    let root = try validatedChildURL(root: accountsRootURL, identifier: account.id)
    let codexHome = root.appending(path: "CodexHome", directoryHint: .isDirectory)
    if !fileManager.fileExists(atPath: codexHome.path) {
      try createPrivateDirectory(at: root)
      try createPrivateDirectory(at: codexHome)
      try writeManagedConfig(to: codexHome)
    }
    return codexHome
  }

  func commitPendingAccount(_ account: UsageAccount) throws {
    guard account.isManaged else { throw UsageAccountRegistryError.invalidAccountIdentifier }
    let source = try validatedChildURL(root: stagingRootURL, identifier: account.id)
    let destination = try validatedChildURL(root: accountsRootURL, identifier: account.id)
    guard fileManager.fileExists(atPath: source.path) else {
      throw UsageAccountRegistryError.pendingAccountMissing
    }
    guard !fileManager.fileExists(atPath: destination.path) else {
      throw UsageAccountRegistryError.invalidAccountIdentifier
    }

    try fileManager.moveItem(at: source, to: destination)
    do {
      var managedAccounts = try loadManagedAccounts()
      managedAccounts.append(account)
      try saveManagedAccounts(managedAccounts)
    } catch {
      try? fileManager.moveItem(at: destination, to: source)
      throw error
    }
  }

  func discardPendingAccount(_ account: UsageAccount) throws {
    guard account.isManaged else { return }
    let root = try validatedChildURL(root: stagingRootURL, identifier: account.id)
    if fileManager.fileExists(atPath: root.path) {
      try fileManager.removeItem(at: root)
    }
  }

  func updateManagedAccount(_ account: UsageAccount) throws {
    guard account.isManaged else { return }
    var managedAccounts = try loadManagedAccounts()
    guard let index = managedAccounts.firstIndex(where: { $0.id == account.id }) else {
      throw UsageAccountRegistryError.managedAccountMissing
    }
    managedAccounts[index] = account
    try saveManagedAccounts(managedAccounts)
  }

  func removeManagedAccount(_ account: UsageAccount) throws {
    guard account.isManaged else { return }
    let root = try validatedChildURL(root: accountsRootURL, identifier: account.id)
    let stagedRemoval = try validatedChildURL(root: stagingRootURL, identifier: account.id)
    var managedAccounts = try loadManagedAccounts()
    managedAccounts.removeAll { $0.id == account.id }

    var movedForRemoval = false
    if fileManager.fileExists(atPath: root.path) {
      if fileManager.fileExists(atPath: stagedRemoval.path) {
        try fileManager.removeItem(at: stagedRemoval)
      }
      try fileManager.moveItem(at: root, to: stagedRemoval)
      movedForRemoval = true
    }

    do {
      try saveManagedAccounts(managedAccounts)
    } catch {
      if movedForRemoval {
        try? fileManager.moveItem(at: stagedRemoval, to: root)
      }
      throw error
    }

    if movedForRemoval {
      // If immediate cleanup fails, the private staging directory is purged on
      // the next launch instead of restoring an account that is already removed.
      try? fileManager.removeItem(at: stagedRemoval)
    }
  }

  private func prepareRootDirectories() throws {
    try createPrivateDirectory(at: applicationSupportURL)
    try createPrivateDirectory(at: accountsRootURL)
    try createPrivateDirectory(at: stagingRootURL)
  }

  private func createPrivateDirectory(at url: URL) throws {
    try fileManager.createDirectory(
      at: url,
      withIntermediateDirectories: true,
      attributes: [.posixPermissions: 0o700]
    )
    try fileManager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: url.path)
  }

  private func discardAbandonedPendingAccounts() throws {
    let children = try fileManager.contentsOfDirectory(
      at: stagingRootURL,
      includingPropertiesForKeys: nil,
      options: [.skipsHiddenFiles]
    )
    for child in children {
      try fileManager.removeItem(at: child)
    }
  }

  private func writeManagedConfig(to codexHome: URL) throws {
    let configURL = codexHome.appending(path: "config.toml", directoryHint: .notDirectory)
    guard !fileManager.fileExists(atPath: configURL.path) else { return }
    let config = "cli_auth_credentials_store = \"file\"\n"
    try Data(config.utf8).write(to: configURL, options: .atomic)
    try fileManager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: configURL.path)
  }

  private func loadManagedAccounts() throws -> [UsageAccount] {
    guard fileManager.fileExists(atPath: registryURL.path) else { return [] }
    let data = try Data(contentsOf: registryURL)
    let decoder = JSONDecoder()
    decoder.dateDecodingStrategy = .iso8601
    let payload = try decoder.decode(Payload.self, from: data)
    return payload.accounts.filter { $0.isManaged }
  }

  private func saveManagedAccounts(_ accounts: [UsageAccount]) throws {
    try prepareRootDirectories()
    let encoder = JSONEncoder()
    encoder.dateEncodingStrategy = .iso8601
    encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
    let data = try encoder.encode(Payload(version: 1, accounts: accounts))
    try data.write(to: registryURL, options: .atomic)
    try fileManager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: registryURL.path)
  }

  private func validatedChildURL(root: URL, identifier: String) throws -> URL {
    guard UUID(uuidString: identifier) != nil else {
      throw UsageAccountRegistryError.invalidAccountIdentifier
    }
    let standardizedRoot = root.standardizedFileURL
    let candidate = standardizedRoot.appending(path: identifier, directoryHint: .isDirectory)
      .standardizedFileURL
    let rootPrefix = standardizedRoot.path.hasSuffix("/")
      ? standardizedRoot.path
      : standardizedRoot.path + "/"
    guard candidate.path.hasPrefix(rootPrefix) else {
      throw UsageAccountRegistryError.unsafeManagedPath
    }
    return candidate
  }

  private func migrateLegacyManagedHomeIfNeeded() throws {
    let legacyHome = applicationSupportURL.appending(
      path: "CodexHome",
      directoryHint: .isDirectory
    )
    let legacyAuth = legacyHome.appending(path: "auth.json", directoryHint: .notDirectory)
    guard fileManager.isReadableFile(atPath: legacyAuth.path) else { return }

    var managedAccounts = try loadManagedAccounts()
    let identifier = UUID().uuidString.lowercased()
    let accountRoot = try validatedChildURL(root: accountsRootURL, identifier: identifier)
    try createPrivateDirectory(at: accountRoot)
    let destination = accountRoot.appending(path: "CodexHome", directoryHint: .isDirectory)
    do {
      try fileManager.moveItem(at: legacyHome, to: destination)
      let account = UsageAccount(
        id: identifier,
        kind: .managed,
        displayName: "이전 앱 계정",
        lastKnownEmail: nil,
        lastKnownPlanType: nil,
        createdAt: Date()
      )
      managedAccounts.append(account)
      try saveManagedAccounts(managedAccounts)
    } catch {
      if fileManager.fileExists(atPath: destination.path),
        !fileManager.fileExists(atPath: legacyHome.path)
      {
        try? fileManager.moveItem(at: destination, to: legacyHome)
      }
      try? fileManager.removeItem(at: accountRoot)
      throw error
    }
  }
}
