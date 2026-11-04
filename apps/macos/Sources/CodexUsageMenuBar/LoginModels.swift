import Foundation

enum CodexLoginMethod: String, CaseIterable, Identifiable, Sendable {
  case browser
  case deviceCode

  var id: Self { self }
  var requestType: String { self == .browser ? "chatgpt" : "chatgptDeviceCode" }
  var title: String { L10n.text(self == .browser ? "auth.method_browser" : "auth.method_device") }
  var systemImage: String { self == .browser ? "globe" : "key.viewfinder" }
}

struct DeviceLoginInfo: Equatable, Sendable {
  let loginId: String
  let verificationURL: URL
  let userCode: String
}

struct BrowserLoginInfo: Equatable, Sendable {
  let loginId: String
  let authorizationURL: URL
}

enum CodexLoginChallenge: Equatable, Sendable {
  case browser(BrowserLoginInfo)
  case deviceCode(DeviceLoginInfo)

  var loginId: String {
    switch self {
    case .browser(let info): return info.loginId
    case .deviceCode(let info): return info.loginId
    }
  }

  var authorizationURL: URL {
    switch self {
    case .browser(let info): return info.authorizationURL
    case .deviceCode(let info): return info.verificationURL
    }
  }

  static func parse(_ result: [String: Any], method: CodexLoginMethod) throws -> Self {
    guard result["type"] as? String == method.requestType,
      let loginId = result["loginId"] as? String, !loginId.isEmpty
    else { throw CodexUsageError.invalidResponse }
    switch method {
    case .browser:
      return .browser(BrowserLoginInfo(loginId: loginId,
        authorizationURL: try loginURL(result["authUrl"])))
    case .deviceCode:
      guard let code = result["userCode"] as? String, !code.isEmpty else {
        throw CodexUsageError.invalidResponse
      }
      return .deviceCode(DeviceLoginInfo(loginId: loginId,
        verificationURL: try loginURL(result["verificationUrl"]), userCode: code))
    }
  }

  static func loginURL(_ value: Any?) throws -> URL {
    // Do not launch file/custom schemes or send the user to an unexpected login host.
    guard let string = value as? String, let url = URL(string: string),
      url.scheme?.lowercased() == "https",
      ["auth.openai.com", "chatgpt.com"].contains(url.host?.lowercased() ?? ""),
      url.user == nil, url.password == nil, url.port == nil || url.port == 443
    else { throw CodexUsageError.invalidResponse }
    return url
  }
}
