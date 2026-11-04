import Foundation

struct CompactResetCreditSummary: Equatable {
  let text: String
  let detail: String

  init?(snapshot: UsageSnapshot, relativeTo date: Date = Date()) {
    guard let count = snapshot.availableResetCredits, count >= 0 else { return nil }
    var parts = [L10n.text("credit.count", count)]
    var details = [L10n.text("credit.count_detail", count)]
    if count > 0 {
      if let expiration = snapshot.earliestResetCreditExpiration {
        let scope = L10n.text(snapshot.hasCompleteResetCreditDetails ? "credit.earliest" : "credit.confirmed")
        parts.append(L10n.text("credit.expiration", scope, CompactUsageDate.string(expiration, relativeTo: date)))
        details.append(L10n.text(
          snapshot.hasCompleteResetCreditDetails ? "credit.earliest_detail" : "credit.confirmed_detail",
          L10n.date(expiration, style: .complete)))
        if let countdown = snapshot.resetCreditExpirationCountdown(relativeTo: date) {
          let remaining = expiration <= date ? countdown : L10n.text("credit.after", countdown)
          parts.append(remaining)
          details.append(remaining)
        }
      } else {
        parts.append(L10n.text("credit.expiration_missing"))
        details.append(L10n.text("credit.expiration_missing_detail"))
      }
      if snapshot.resetCreditDetails != nil,
        Int64(snapshot.availableResetCreditDetails.count) < count
      {
        parts.append(L10n.text("credit.coverage", snapshot.availableResetCreditDetails.count, count))
        details.append(L10n.text("credit.coverage_detail", snapshot.availableResetCreditDetails.count, count))
      }
    }
    text = parts.joined(separator: " · ")
    detail = details.joined(separator: ", ")
  }
}

enum CompactUsageDate {
  static func string(_ date: Date, relativeTo reference: Date = Date()) -> String {
    let formatter = DateFormatter()
    formatter.locale = Locale(identifier: "en_US_POSIX")
    let calendar = Calendar(identifier: .gregorian)
    formatter.calendar = calendar
    formatter.timeZone = .current
    formatter.dateFormat = calendar.component(.year, from: date) == calendar.component(.year, from: reference)
      ? "M/d HH:mm" : "yy/M/d HH:mm"
    return formatter.string(from: date)
  }
}
