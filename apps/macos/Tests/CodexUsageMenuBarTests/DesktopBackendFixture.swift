import Foundation
import Testing
@testable import CodexUsageMenuBar

struct FixtureAccount: Sendable {
  var name: String
  var identity: String
  var revision: String
  var workspace = ""
  var display = ""
  var selected: String? = nil
  var json: [String: Any] {
    var result: [String: Any] = ["name": name, "default": name == "default", "identity": identity,
      "revision": revision, "email": identity + "@example.invalid", "plan": "team",
      "workspace_name": workspace, "display_name": display]
    if let selected { result["selected_nickname"] = selected }
    return result
  }
}

actor DesktopBackendFixture: CodexMServing {
  var accounts = [FixtureAccount(name: "default", identity: "alice", revision: "a1"),
    FixtureAccount(name: "work", identity: "bob", revision: "b1")]
  private(set) var calls: [String] = []
  private(set) var usageReads = 0
  private(set) var canceledLogins = 0
  private var slowUsage = false
  private var failUsage = false
  private var slowLogin = false
  private var failMutations = false
  private var labels: [String: String] = [:]
  func delayUsage() { slowUsage = true }
  func rejectUsage() { failUsage = true }
  func delayLogin() { slowLogin = true }
  func rejectMutations() { failMutations = true }
  func selectDefault(_ identity: String) {
    let i = accounts.firstIndex { $0.name == "default" }!
    accounts[i] = .init(name: "default", identity: identity, revision: identity + "-revision",
      workspace: labels[identity] ?? "")
  }
  nonisolated func shutdown() {}
  func request(_ method: String, params data: Data) async throws -> Data {
    calls.append(method)
    let params = try JSONSerialization.jsonObject(with: data) as! [String: Any]
    let name = params["account"] as? String ?? "default"
    let result: Any
    switch method {
    case "accounts.catalog": result = accounts.map(\.json)
    case "accounts.usage":
      let account = accounts.first { $0.name == name }!
      usageReads += 1
      if slowUsage { try await Task.sleep(for: .milliseconds(150)) }
      if failUsage { throw CodexMBridgeError.rejected("Offline") }
      result = ["revision": account.revision, "snapshot": ["email": account.identity + "@example.invalid", "plan": "team",
        "checked_at": "2026-01-01T00:00:00Z", "rateLimits": ["primary": ["usedPercent": 20, "windowDurationMins": 300, "resetsAt": 1900000000],
          "secondary": ["usedPercent": 40, "windowDurationMins": 10080, "resetsAt": 1900500000]]]]
    case "accounts.presentation", "accounts.remove", "accounts.setDefault":
      if failMutations { throw CodexMBridgeError.rejected("Rejected") }
      if let order = params["order"] as? [String] { accounts = order.map { name in accounts.first { $0.name == name }! } }
      else {
        let i = accounts.firstIndex { $0.name == name }!
        if let revision = params["revision"] as? String, revision != accounts[i].revision { throw CodexMBridgeError.rejected("Stale revision") }
        if method == "accounts.remove" { accounts.remove(at: i) }
        else if method == "accounts.setDefault" { selectDefault(accounts[i].identity) }
        else {
          if let label = params["workspace_name"] as? String { accounts[i].workspace = label; labels[accounts[i].identity] = label }
          if let label = params["display_name"] as? String { accounts[i].display = label }
        }
      }
      result = true
    case "login.start":
      if slowLogin { try await Task.detached { try await Task.sleep(for: .milliseconds(150)) }.value }
      result = ["account": name, "login_id": "test-login", "status": "pending",
        "auth_url": "https://auth.openai.com/oauth/authorize", "verification_url": "https://auth.openai.com/codex/device", "user_code": "TEST-CODE"]
    case "login.status": result = ["account": name, "login_id": "test-login", "status": "succeeded"]
    case "login.cancel":
      canceledLogins += 1
      result = ["account": name, "login_id": "test-login", "status": "canceled"]
    default: throw CodexMBridgeError.rejected("Unexpected method: " + method)
    }
    return try JSONSerialization.data(withJSONObject: result, options: .fragmentsAllowed)
  }
}

@MainActor
struct DesktopStoreFixture {
  let backend = DesktopBackendFixture()
  let defaults: UserDefaults
  let suite = "CodexMTests." + UUID().uuidString
  init() throws { defaults = try #require(UserDefaults(suiteName: suite)) }
  func store(notice: Duration = .seconds(5), error: Duration = .seconds(10)) -> UsageStore {
    UsageStore(backend: backend, authorizationPageOpener: { _ in true },
      automaticScheduleStore: .init(defaults: defaults), refreshSchedule: .init(defaults: defaults),
      noticeDismissDelay: notice, errorDismissDelay: error)
  }
  func remove() { defaults.removePersistentDomain(forName: suite) }
}

@MainActor
func waitForDesktop(_ condition: @MainActor () async -> Bool) async throws {
  let deadline = ContinuousClock.now + .seconds(5)
  while !(await condition()), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
  #expect(await condition())
}
