import Foundation

enum AccountOrder {
  static func normalized(_ proposed: [String], availableIDs: [String]) -> [String] {
    let available = Set(availableIDs)
    var seen = Set<String>()
    return (proposed + availableIDs).filter {
      available.contains($0) && seen.insert($0).inserted
    }
  }

  static func moving(_ accountID: String, to targetID: String, in order: [String]) -> [String] {
    guard accountID != targetID,
      let source = order.firstIndex(of: accountID),
      let target = order.firstIndex(of: targetID)
    else { return order }
    var result = order
    result.remove(at: source)
    result.insert(accountID, at: target)
    return result
  }
}
