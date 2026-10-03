import AppKit
import SwiftUI
import Testing

@testable import CodexUsageMenuBar

@Suite("Release localization")
struct LocalizationTests {
  @Test("Resolves OS languages and falls back to English")
  func resolvesPreferredLanguages() {
    #expect(AppLanguage.resolve(preferredLanguages: ["ko-KR"]) == .korean)
    #expect(AppLanguage.resolve(preferredLanguages: ["en-GB"]) == .english)
    #expect(AppLanguage.resolve(preferredLanguages: ["zh-Hans-CN"]) == .chinese)
    #expect(AppLanguage.resolve(preferredLanguages: ["zh-Hant-TW"]) == .chinese)
    #expect(AppLanguage.resolve(preferredLanguages: ["hi_IN"]) == .hindi)
    #expect(AppLanguage.resolve(preferredLanguages: ["fr-FR", "ko-KR"]) == .english)
    #expect(AppLanguage.resolve(preferredLanguages: ["", "ko-KR"]) == .english)
    #expect(AppLanguage.resolve(preferredLanguages: ["not a language"]) == .english)
    #expect(AppLanguage.resolve(preferredLanguages: []) == .english)
  }

  @Test("Explicit choices override OS language and use stable native menu names", arguments: AppLanguagePreference.allCases)
  func resolvesLanguagePreference(_ preference: AppLanguagePreference) {
    if preference == .system {
      #expect(preference.resolve(preferredLanguages: ["ko-KR"]) == .korean)
      #expect(preference.resolve(preferredLanguages: ["fr-FR"]) == .english)
      #expect(preference.resolve(preferredLanguages: []) == .english)
    } else {
      let language = AppLanguage(rawValue: preference.rawValue)
      #expect(preference.resolve(preferredLanguages: ["fr-FR"]) == language)
      #expect(preference.resolve(preferredLanguages: ["ko-KR"]) == language)
      for currentLanguage in AppLanguage.allCases {
        L10n.$languageOverride.withValue(currentLanguage) {
          #expect(preference.title == L10n.catalog(for: preference.resolve(preferredLanguages: []))["language.name"])
        }
      }
    }
  }

  @Test("Every language has the same nonempty keys and safe format arguments", arguments: AppLanguage.allCases)
  func validatesCatalogs(_ language: AppLanguage) throws {
    let english = L10n.catalog(for: .english)
    let values = L10n.catalog(for: language)
    #expect(english.count >= 130)
    #expect(Set(values.keys) == Set(english.keys))
    let regex = try NSRegularExpression(pattern: "%(@|lld)")
    func placeholders(_ value: String) -> [String] {
      let string = value as NSString
      return regex.matches(in: value, range: NSRange(location: 0, length: string.length))
        .map { string.substring(with: $0.range) }
    }
    for (key, value) in values {
      #expect(!value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, "Empty: \(key)")
      #expect(placeholders(value) == placeholders(english[key] ?? ""), "Format mismatch: \(key)")
    }
  }

  @Test("All source localization references exist in every catalog")
  func validatesSourceKeys() throws {
    let project = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
      .deletingLastPathComponent().deletingLastPathComponent()
    let sources = project.appending(path: "Sources/CodexUsageMenuBar")
    let files = try FileManager.default.contentsOfDirectory(at: sources, includingPropertiesForKeys: nil)
      .filter { $0.pathExtension == "swift" }
    let regex = try NSRegularExpression(pattern: #"L10n\.text\("([^"]+)""#)
    for file in files {
      let source = try String(contentsOf: file, encoding: .utf8)
      let text = source as NSString
      let matches = regex.matches(in: source, range: NSRange(location: 0, length: text.length))
      for match in matches {
        let key = text.substring(with: match.range(at: 1))
        for language in AppLanguage.allCases {
          #expect(L10n.catalog(for: language)[key] != nil, "Missing \(language.rawValue): \(key)")
        }
      }
      // Non-English app-authored UI must live in the catalogs, not in source.
      #expect(source.range(of: "[가-힣]", options: .regularExpression) == nil, "Unlocalized: \(file.lastPathComponent)")
    }
    for kind in ProductInformationKind.allCases {
      for key in kind.paragraphKeys {
        #expect(L10n.catalog(for: .english)[key] != nil)
      }
    }
  }

  @Test("Formats countdowns, credit expiry, accessibility and errors in each language", arguments: AppLanguage.allCases)
  func validatesUserFacingFormats(_ language: AppLanguage) {
    L10n.$languageOverride.withValue(language) {
      #expect(L10n.locale.language.languageCode?.identifier == language.rawValue.split(separator: "-").first.map(String.init))
      #expect(L10n.text("common.close") == L10n.catalog(for: language)["common.close"])
      let now = Date(timeIntervalSince1970: 1_790_000_000)
      let limit = UsageLimitWindow(usedPercent: 30, windowDurationMinutes: 300,
        resetsAt: now.addingTimeInterval(3660))
      #expect(limit.resetCountdown(relativeTo: now) == L10n.text("duration.hours_minutes", 1, 1))
      #expect(MenuBarIndicator.limits(fiveHour: 70, weekly: 40).accessibilityLabel
        == L10n.text("usage.dual_remaining", 70, 40))
      #expect(CodexUsageError.timedOut.errorDescription == L10n.text("error.timeout"))
      #expect(CodexUsageError.serverError("Technical detail").errorDescription
        == L10n.text("error.server_detail", "Technical detail"))
      #expect(UsageAccountRegistryError.invalidAccountIdentifier.errorDescription
        == L10n.text("account.invalid_id"))
      let expired = UsageSnapshot(weeklyLimit: limit, planType: nil, availableResetCredits: 1,
        resetCreditDetails: [ResetCreditDetail(id: nil, status: "available", grantedAt: nil,
          expiresAt: now.addingTimeInterval(-1), title: nil, description: nil)],
        accountEmail: nil, fetchedAt: now)
      let summary = CompactResetCreditSummary(snapshot: expired, relativeTo: now)
      #expect(summary?.text.contains(L10n.text("credit.expiring_soon")) == true)
      #expect(summary?.text.contains(L10n.text("credit.after", L10n.text("credit.expiring_soon"))) == false)
    }
  }

  @Test("Clearing names keeps the email and never exposes identifiers")
  func unnamedAccountFallback() throws {
    var account = UsageAccount(id: "00000000-0000-0000-0000-000000000001", kind: .managed,
      displayName: " ", lastKnownEmail: "owner@example.com", lastKnownPlanType: nil, createdAt: Date())
    let initial = account.title
    #expect(initial == "owner@example.com")
    #expect(account.workspaceDisplayLabel == nil)
    account.displayName = "Development"
    #expect(account.title == "Development")
    account.displayName = ""
    #expect(account.title == initial)
    #expect(account.lastKnownEmail == "owner@example.com")
    account.lastKnownWorkspaceFingerprint = String(repeating: "abcdef12", count: 8)
    #expect(account.title == initial)
    #expect(account.workspaceDisplayLabel == nil)
    account.lastKnownEmail = nil
    #expect(account.title == L10n.text("account.managed_name"))
  }
}

@Suite("Localized layout")
@MainActor
struct LocalizedLayoutTests {
  @Test("An already-hosted information panel observes language changes without being recreated")
  func observesLiveLanguageChanges() async throws {
    let suiteName = "LocalizedLayoutTests.liveLanguage.\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: suiteName))
    defer { defaults.removePersistentDomain(forName: suiteName) }
    let preferences = AppPreferences(defaults: defaults)
    var languageChanges = 0
    let hosting = NSHostingView(rootView: ProductInformationView(kind: .about,
      preferences: preferences, onLanguageChange: { languageChanges += 1 }))
    hosting.frame = NSRect(x: 0, y: 0, width: 440, height: 620)
    hosting.layoutSubtreeIfNeeded()
    let bitmap = try #require(hosting.bitmapImageRepForCachingDisplay(in: hosting.bounds))
    hosting.cacheDisplay(in: hosting.bounds, to: bitmap)

    preferences.languagePreference = .hindi
    for _ in 0..<25 {
      hosting.layoutSubtreeIfNeeded()
      if languageChanges > 0 { break }
      try await Task.sleep(for: .milliseconds(10))
    }
    #expect(languageChanges == 1)
    #expect(AppLanguagePreference.load(from: defaults) == .hindi)
  }

  @Test("Renders compact account cards in every language", arguments: AppLanguage.allCases)
  func rendersCards(_ language: AppLanguage) throws {
    try L10n.$languageOverride.withValue(language) {
      let root = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
      defer { try? FileManager.default.removeItem(at: root) }
      let store = UsageStore(registry: UsageAccountRegistry(applicationSupportURL: root))
      let now = Date()
      var account = UsageAccount.systemDefault
      account.workspaceName = "Development"
      let snapshot = UsageSnapshot(
        fiveHourLimit: UsageLimitWindow(usedPercent: 20, windowDurationMinutes: 300, resetsAt: now.addingTimeInterval(15000)),
        weeklyLimit: UsageLimitWindow(usedPercent: 40, windowDurationMinutes: 10080, resetsAt: now.addingTimeInterval(500000)),
        planType: "team", availableResetCredits: 3,
        resetCreditDetails: [ResetCreditDetail(id: nil, status: "available", grantedAt: nil,
          expiresAt: now.addingTimeInterval(172800), title: nil, description: nil)],
        accountEmail: "owner@example.com", fetchedAt: now)
      let card = AccountUsageCard(viewState: UsageStore.AccountViewState(account: account,
        state: .loaded(snapshot), isRefreshing: false), store: store,
        preferences: AppPreferences(), onRequestDelete: {})
        .frame(width: 392).padding(14).background(Color(nsColor: .windowBackgroundColor))
        .environment(\.locale, L10n.locale).environment(\.colorScheme, .light)
      let hosting = NSHostingView(rootView: card)
      hosting.frame = NSRect(origin: .zero, size: hosting.fittingSize)
      hosting.layoutSubtreeIfNeeded()
      let bitmap = try #require(hosting.bitmapImageRepForCachingDisplay(in: hosting.bounds))
      hosting.cacheDisplay(in: hosting.bounds, to: bitmap)
      let image = NSImage(size: hosting.bounds.size)
      image.addRepresentation(bitmap)
      #expect(image.size.width == 420)
      #expect(image.size.height <= 156)
      try savePreview(image, name: "card-\(language.rawValue)")
    }
  }

  @Test("Renders scrollable help and product information without widening the panel", arguments: AppLanguage.allCases)
  func rendersInformation(_ language: AppLanguage) throws {
    try L10n.$languageOverride.withValue(language) {
      for kind in [ProductInformationKind.about, .automaticRefresh] {
        let content = ProductInformationView(kind: kind, preferences: AppPreferences())
          .frame(width: 440, height: 620)
          .background(Color(nsColor: .windowBackgroundColor)).environment(\.colorScheme, .light)
        // ImageRenderer does not render native ScrollView/Menu controls. Use
        // AppKit's offscreen display cache to verify the actual hosted view.
        let hosting = NSHostingView(rootView: content)
        hosting.frame = NSRect(x: 0, y: 0, width: 440, height: 620)
        hosting.layoutSubtreeIfNeeded()
        let bitmap = try #require(hosting.bitmapImageRepForCachingDisplay(in: hosting.bounds))
        hosting.cacheDisplay(in: hosting.bounds, to: bitmap)
        let image = NSImage(size: hosting.bounds.size)
        image.addRepresentation(bitmap)
        #expect(image.size == NSSize(width: 440, height: 620))
        try savePreview(image, name: "\(kind == .about ? "about" : "help")-\(language.rawValue)")
      }
    }
  }

  private func savePreview(_ image: NSImage, name: String) throws {
    guard let directory = ProcessInfo.processInfo.environment["CODEX_LOCALIZATION_PREVIEW_DIR"],
      let data = image.tiffRepresentation, let bitmap = NSBitmapImageRep(data: data),
      let png = bitmap.representation(using: .png, properties: [:]) else { return }
    try FileManager.default.createDirectory(atPath: directory, withIntermediateDirectories: true)
    try png.write(to: URL(fileURLWithPath: directory).appending(path: name + ".png"))
  }
}
