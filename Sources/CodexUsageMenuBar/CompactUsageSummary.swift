import Foundation

struct CompactResetCreditSummary: Equatable {
  let text: String
  let detail: String

  init?(snapshot: UsageSnapshot, relativeTo date: Date = Date()) {
    guard let count = snapshot.availableResetCredits, count >= 0 else { return nil }
    var parts = ["리셋 \(count)개"]
    var details = ["리셋 크레딧 \(count)개"]
    if count > 0 {
      if let expiration = snapshot.earliestResetCreditExpiration {
        let scope = snapshot.hasCompleteResetCreditDetails ? "최초" : "확인분"
        parts.append("\(scope) \(CompactUsageDate.string(expiration, relativeTo: date)) 소멸")
        details.append("\(snapshot.hasCompleteResetCreditDetails ? "가장 빠른 소멸" : "확인된 크레딧 소멸") \(expiration.formatted(date: .complete, time: .shortened))")
        if let countdown = snapshot.resetCreditExpirationCountdown(relativeTo: date) {
          let remaining = countdown == "곧 소멸" ? countdown : "\(countdown) 후"
          parts.append(remaining)
          details.append(remaining)
        }
      } else {
        parts.append("소멸 시각 미제공")
        details.append("소멸 시각 정보 없음")
      }
      if snapshot.resetCreditDetails != nil,
        Int64(snapshot.availableResetCreditDetails.count) < count
      {
        let coverage = "상세 \(snapshot.availableResetCreditDetails.count)/\(count)"
        parts.append(coverage)
        details.append("\(coverage)개 제공됨")
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
