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
  var workspaceName: String? = nil
  var lastKnownWorkspaceFingerprint: String? = nil
  var revision = ""
  var selectedNickname: String? = nil
  var isNativeDefault = false
  let createdAt: Date

  var nativeName: String { isSystemDefault ? "default" : id }
  var activationKey: String? { lastKnownWorkspaceFingerprint.map { "identity:" + $0 } }

  static var systemDefault: UsageAccount {
    UsageAccount(
      id: systemDefaultID,
      kind: .systemDefault,
      displayName: nil,
      lastKnownEmail: nil,
      lastKnownPlanType: nil,
      createdAt: .distantPast
    )
  }

  var isSystemDefault: Bool { kind == .systemDefault }
  var isManaged: Bool { kind == .managed }

  var workspaceDisplayLabel: String? {
    normalizedWorkspaceName.map { L10n.text("account.workspace_label", $0) }
  }

  var workspaceNameKey: String? { lastKnownWorkspaceFingerprint }

  var normalizedDisplayName: String? {
    let name = displayName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    return name.isEmpty ? nil : name
  }

  var normalizedWorkspaceName: String? {
    let trimmedName = workspaceName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    return trimmedName.isEmpty ? nil : trimmedName
  }

  var title: String {
    if let normalizedDisplayName { return normalizedDisplayName }
    if isSystemDefault { return L10n.text("account.default_name") }
    let email = lastKnownEmail?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    return email.isEmpty ? L10n.text("account.managed_name") : email
  }
}
