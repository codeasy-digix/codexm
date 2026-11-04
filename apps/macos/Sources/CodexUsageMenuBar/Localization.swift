import Foundation

enum AppLanguage: String, CaseIterable, Sendable {
  case english = "en"
  case korean = "ko"
  case chinese = "zh-Hans"
  case hindi = "hi"

  static func resolve(preferredLanguages: [String]) -> AppLanguage {
    guard let preferred = preferredLanguages.first,
      preferred.range(of: "^[A-Za-z]{2,3}([_-][A-Za-z0-9]{2,8})*$", options: .regularExpression) != nil
    else { return .english }
    switch preferred.lowercased().split(whereSeparator: { $0 == "-" || $0 == "_" }).first {
    case "en": return .english
    case "ko": return .korean
    case "zh": return .chinese
    case "hi": return .hindi
    default: return .english
    }
  }
}

enum AppLanguagePreference: String, CaseIterable, Identifiable, Sendable {
  case system
  case english = "en"
  case korean = "ko"
  case chinese = "zh-Hans"
  case hindi = "hi"

  static let defaultsKey = "AppLanguagePreference"
  var id: Self { self }

  var title: String {
    guard let language = AppLanguage(rawValue: rawValue) else {
      return L10n.text("options.language_system")
    }
    // Native names stay recognizable even when the current UI language changes.
    return L10n.catalog(for: language)["language.name"] ?? language.rawValue
  }

  func resolve(preferredLanguages: [String]) -> AppLanguage {
    AppLanguage(rawValue: rawValue) ?? AppLanguage.resolve(preferredLanguages: preferredLanguages)
  }

  static func load(from defaults: UserDefaults = .standard) -> Self {
    defaults.string(forKey: defaultsKey).flatMap(Self.init(rawValue:)) ?? .system
  }
}

enum L10n {
  @TaskLocal static var languageOverride: AppLanguage?

  static var language: AppLanguage {
    // UserDefaults is thread-safe: background diagnostics and UI use the same
    // persisted preference without sharing mutable, actor-isolated state.
    languageOverride ?? AppLanguagePreference.load().resolve(preferredLanguages: Locale.preferredLanguages)
  }

  static var locale: Locale {
    let current = Locale.current
    let code = current.language.languageCode?.identifier
    let selectedCode = language.rawValue.split(separator: "-").first.map(String.init)
    return code == selectedCode ? current : Locale(identifier: language.rawValue)
  }

  static let resourceBundle: Bundle = {
    let name = "CodexUsageMenuBar_CodexUsageMenuBar"
    if let url = Bundle.main.url(forResource: name, withExtension: "bundle"),
      let bundle = Bundle(url: url)
    {
      return bundle
    }
    return Bundle.module
  }()

  private static let catalogs: [AppLanguage: [String: String]] = {
    Dictionary(uniqueKeysWithValues: AppLanguage.allCases.map { language in
      let url = resourceBundle.url(forResource: "Localizable", withExtension: "strings",
        subdirectory: "\(language.rawValue).lproj")
      let values = url.flatMap { try? Data(contentsOf: $0) }
        .flatMap { try? PropertyListSerialization.propertyList(from: $0, format: nil) }
        as? [String: String] ?? [:]
      return (language, values)
    })
  }()

  static func catalog(for language: AppLanguage) -> [String: String] { catalogs[language] ?? [:] }

  static var catalogsAreComplete: Bool {
    let keys = Set(catalog(for: .english).keys)
    return !keys.isEmpty && AppLanguage.allCases.allSatisfy {
      let values = catalog(for: $0)
      return Set(values.keys) == keys && values.values.allSatisfy { !$0.isEmpty }
    }
  }

  /// A non-network packaging diagnostic; never initializes account storage.
  static func diagnosticData() -> Data {
    let report: [String: Any] = [
      "language": language.rawValue, "locale": locale.identifier,
      "languagePreference": AppLanguagePreference.load().rawValue,
      "resources": resourceBundle.bundleURL.path,
      "catalogsComplete": catalogsAreComplete,
      "catalogCounts": Dictionary(uniqueKeysWithValues: AppLanguage.allCases.map {
        ($0.rawValue, catalog(for: $0).count)
      }),
      "autoRefreshTitle": text("auto.title"), "close": text("common.close"),
      "about": text("about.title"),
      "languageMenu": text("options.language"),
    ]
    return (try? JSONSerialization.data(withJSONObject: report, options: [.sortedKeys])) ?? Data()
  }

  static func text(_ key: String, _ arguments: CVarArg...) -> String {
    let format = catalogs[language]?[key] ?? catalogs[.english]?[key] ?? key
    return arguments.isEmpty ? format : String(format: format, locale: locale, arguments: arguments)
  }

  static func date(_ date: Date, style: Date.FormatStyle.DateStyle = .abbreviated,
    time: Date.FormatStyle.TimeStyle = .shortened) -> String
  {
    date.formatted(Date.FormatStyle(date: style, time: time).locale(locale))
  }

  /// Preserve original system/backend diagnostics, but always provide a
  /// translated app-authored explanation around errors outside our types.
  static func errorDescription(_ error: Error) -> String {
    if error is CodexUsageError || error is CodexRuntimeError || error is UsageAccountRegistryError {
      return error.localizedDescription
    }
    return text("error.detail", error.localizedDescription)
  }
}
