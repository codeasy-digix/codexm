import Foundation

enum BridgeValue: Encodable, Sendable {
  case string(String), bool(Bool), strings([String])
  func encode(to encoder: any Encoder) throws {
    var container = encoder.singleValueContainer()
    switch self {
    case .string(let value): try container.encode(value)
    case .bool(let value): try container.encode(value)
    case .strings(let value): try container.encode(value)
    }
  }
}

protocol CodexMServing: Sendable {
  func request(_ method: String, params: Data) async throws -> Data
  func shutdown()
}

extension CodexMServing {
  func call<T: Decodable & Sendable>(_ method: String, _ params: [String: BridgeValue] = [:], as: T.Type = T.self) async throws -> T {
    let data = try JSONEncoder().encode(params)
    return try JSONDecoder().decode(T.self, from: await request(method, params: data))
  }
}

enum CodexMBridgeError: LocalizedError {
  case unavailable, disconnected, timedOut, invalidResponse, incompatible, rejected(String)
  var errorDescription: String? {
    switch self {
    case .unavailable: return L10n.text("codexm.missing")
    case .disconnected: return L10n.text("codexm.disconnected")
    case .timedOut: return L10n.text("codexm.timeout")
    case .invalidResponse: return L10n.text("error.invalid_response")
    case .incompatible: return L10n.text("codexm.incompatible")
    case .rejected(let message): return message
    }
  }
}

struct CodexMLocator: Sendable {
  var environment = ProcessInfo.processInfo.environment
  var bundleURL = Bundle.main.bundleURL
  func locate() throws -> URL {
    let home = FileManager.default.homeDirectoryForCurrentUser.path
    let paths = [environment["CODEXM_BINARY"],
      bundleURL.appending(path: "Contents/Resources/native/codexm").path,
      "/opt/homebrew/opt/codexm-cli/bin/codexm", "/usr/local/opt/codexm-cli/bin/codexm",
      home + "/.local/bin/codexm", "/opt/homebrew/bin/codexm", "/usr/local/bin/codexm"]
    for path in paths.compactMap({ $0 }) where path.hasPrefix("/") && FileManager.default.isExecutableFile(atPath: path) {
      return URL(fileURLWithPath: path).resolvingSymlinksInPath()
    }
    throw CodexMBridgeError.unavailable
  }
}

// One serial owned stdio bridge. No tokens, native auth readers, HTTP service,
// shell evaluation, or automatic replay of a request after disconnection.
final class CodexMBridge: CodexMServing, @unchecked Sendable {
  private let queue = DispatchQueue(label: "codexm.desktop.bridge")
  private let locator: CodexMLocator
  private var process: Process?
  private var input: FileHandle?
  private var output: FileHandle?
  private var buffer = Data()
  private var nextID = 0
  private var stopped = false
  private let owner = UUID().uuidString

  init(locator: CodexMLocator = CodexMLocator()) { self.locator = locator }

  func request(_ method: String, params: Data) async throws -> Data {
    try Task.checkCancellation()
    return try await withCheckedThrowingContinuation { continuation in
      queue.async { [self] in
        do {
          guard !stopped else { throw CodexMBridgeError.disconnected }
          if process == nil { try start() }
          continuation.resume(returning: try exchange(method, params: params))
        } catch {
          closeProcess()
          continuation.resume(throwing: error)
        }
      }
    }
  }

  func shutdown() {
    queue.async { [self] in stopped = true; closeProcess() }
  }

  private func start() throws {
    let child = Process(), stdin = Pipe(), stdout = Pipe()
    child.executableURL = try locator.locate()
    child.arguments = ["bridge", "--stdio"]
    var env = locator.environment
    for key in ["CODEX_HOME", "CODEX_ACCOUNT", "CODEX_SQLITE_HOME", "CODEXM_NATIVE_ACCOUNT", "CODEXM_NATIVE_THREAD_ID"] { env.removeValue(forKey: key) }
    env["PATH"] = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:" + (env["PATH"] ?? "")
    child.environment = env
    child.currentDirectoryURL = FileManager.default.homeDirectoryForCurrentUser
    child.standardInput = stdin; child.standardOutput = stdout; child.standardError = FileHandle.nullDevice
    try child.run()
    process = child; input = stdin.fileHandleForWriting; output = stdout.fileHandleForReading
    let raw = try exchange("capabilities", params: Data("{}".utf8))
    struct Capabilities: Decodable { struct Method: Decodable { let name: String }; let protocolVersion: Int; let methods: [Method] }
    let capabilities = try JSONDecoder().decode(Capabilities.self, from: raw)
    guard capabilities.protocolVersion == 1,
      Set(["accounts.catalog", "accounts.usage", "accounts.presentation", "login.start", "accounts.setDefault"]).isSubset(of: Set(capabilities.methods.map(\.name)))
    else { throw CodexMBridgeError.incompatible }
  }

  private func exchange(_ method: String, params: Data) throws -> Data {
    guard let process, process.isRunning, let input, let output else { throw CodexMBridgeError.disconnected }
    nextID += 1
    let id = nextID
    let request: [String: Any] = ["protocolVersion": 1, "id": id, "method": method,
      "owner": owner, "operation_id": UUID().uuidString.replacingOccurrences(of: "-", with: ""),
      "params": try JSONSerialization.jsonObject(with: params)]
    var bytes = try JSONSerialization.data(withJSONObject: request); bytes.append(10)
    let timeout = DispatchWorkItem { if process.isRunning { process.terminate() } }
    DispatchQueue.global().asyncAfter(deadline: .now() + 100, execute: timeout)
    defer { timeout.cancel() }
    try input.write(contentsOf: bytes)
    while !buffer.contains(10) {
      guard let chunk = try output.read(upToCount: 16384), !chunk.isEmpty else { throw CodexMBridgeError.disconnected }
      buffer.append(chunk)
      guard buffer.count <= 2 * 1024 * 1024 else { throw CodexMBridgeError.invalidResponse }
    }
    let newline = buffer.firstIndex(of: 10)!
    let line = buffer.prefix(upTo: newline); buffer.removeSubrange(...newline)
    guard let response = try JSONSerialization.jsonObject(with: line) as? [String: Any],
      response["id"] as? Int == id, response["protocolVersion"] as? Int == 1
    else { throw CodexMBridgeError.invalidResponse }
    if let error = response["error"] as? [String: Any] { throw CodexMBridgeError.rejected(error["message"] as? String ?? "Request failed") }
    guard let result = response["result"] else { throw CodexMBridgeError.invalidResponse }
    return try JSONSerialization.data(withJSONObject: result, options: .fragmentsAllowed)
  }

  private func closeProcess() {
    try? input?.close(); try? output?.close()
    if let process, process.isRunning { process.terminate(); process.waitUntilExit() }
    process = nil; input = nil; output = nil; buffer = Data()
  }
}

struct DesktopAccount: Decodable, Sendable {
  let name: String
  let `default`: Bool
  let selected_nickname: String?
  let identity: String?
  let revision: String
  let email: String?
  let plan: String?
  let display_name: String?
  let workspace_name: String?

  var viewAccount: UsageAccount {
    var result = UsageAccount(id: name == "default" ? UsageAccount.systemDefaultID : name,
      kind: name == "default" ? .systemDefault : .managed, displayName: display_name,
      lastKnownEmail: email, lastKnownPlanType: plan, workspaceName: workspace_name,
      lastKnownWorkspaceFingerprint: identity, createdAt: .distantPast)
    result.revision = revision; result.selectedNickname = selected_nickname; result.isNativeDefault = self.default
    return result
  }
}

struct DesktopUsage: Decodable, Sendable {
  struct Snapshot: Decodable, Sendable {
    let email: String?
    let plan: String?
    let rateLimits: RateLimitSnapshot
    let rateLimitsByLimitId: [String: RateLimitSnapshot]?
    let rateLimitResetCredits: RateLimitResetCredits?
    let checked_at: String
    func usage() throws -> UsageSnapshot {
      try GetAccountRateLimitsResponse(rateLimits: rateLimits, rateLimitsByLimitId: rateLimitsByLimitId,
        rateLimitResetCredits: rateLimitResetCredits).usageSnapshot(account: CodexAccount(type: "chatgpt", email: email, planType: plan))
    }
  }
  let revision: String
  let snapshot: Snapshot
}

struct DesktopLogin: Decodable, Sendable {
  let account: String; let login_id: String; let status: String
  let auth_url: String?; let verification_url: String?; let user_code: String?; let error: String?
}

struct ToolbarImport: Decodable, Sendable {
  struct Item: Decodable, Sendable { let legacy_id: String; let account: String?; let action: String; let reason: String? }
  let applied: Bool; let items: [Item]
}
