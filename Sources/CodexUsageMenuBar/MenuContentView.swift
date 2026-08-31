import AppKit
import SwiftUI

struct MenuContentView: View {
  @ObservedObject var store: UsageStore
  @ObservedObject var preferences: AppPreferences
  @State private var pendingDeletionAccount: UsageAccount?

  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      header

      if store.isAuthenticating || store.deviceLoginInfo != nil {
        authenticationPanel
      }

      if let account = pendingDeletionAccount {
        deleteConfirmationPanel(account)
      }

      if let error = store.accountManagementError {
        statusPanel(
          error,
          systemImage: "exclamationmark.circle.fill",
          color: .red,
          onDismiss: store.clearAccountManagementError
        )
      } else if let notice = store.accountManagementNotice {
        statusPanel(
          notice,
          systemImage: "checkmark.circle.fill",
          color: .green,
          onDismiss: store.clearAccountManagementNotice
        )
      }

      accountList

      if let error = store.authenticationError {
        statusPanel(
          error,
          systemImage: "exclamationmark.circle.fill",
          color: .red,
          onDismiss: store.clearAuthenticationError
        )
      }

      Divider()
      accountActions
      automaticActivationPreferences
      launchPreferences
      footer
    }
    .padding(14)
    .frame(width: 420)
    .animation(.easeInOut(duration: 0.2), value: store.accountManagementError)
    .animation(.easeInOut(duration: 0.2), value: store.accountManagementNotice)
    .animation(.easeInOut(duration: 0.2), value: store.authenticationError)
    .onAppear { store.refreshAll() }
    .onChange(of: store.accountStates.map(\.id)) { accountIDs in
      if let pendingDeletionAccount,
        !accountIDs.contains(pendingDeletionAccount.id)
      {
        self.pendingDeletionAccount = nil
      }
    }
  }

  private var accountList: some View {
    ScrollViewReader { proxy in
      ScrollView {
        LazyVStack(spacing: 10) {
          ForEach(store.accountStates) { accountState in
            AccountUsageCard(
              viewState: accountState,
              store: store,
              onRequestDelete: { pendingDeletionAccount = accountState.account }
            )
            .id(accountState.id)
          }
        }
        .padding(.vertical, 1)
      }
      .frame(height: accountListHeight)
      .onAppear { revealRecentlyAddedAccount(using: proxy) }
      .onChange(of: store.recentlyAddedAccountID) { _ in
        revealRecentlyAddedAccount(using: proxy)
      }
    }
    .animation(.easeInOut(duration: 0.18), value: accountListHeight)
  }

  private var header: some View {
    HStack(spacing: 10) {
      Image(systemName: "chart.bar.fill")
        .font(.system(size: 17, weight: .semibold))
        .foregroundStyle(.tint)

      VStack(alignment: .leading, spacing: 1) {
        Text("Codex Usage")
          .font(.headline)
        Text("연결 \(store.accountStates.count)개 · 5시간/주간 남은 사용량")
          .font(.caption)
          .foregroundStyle(.secondary)
      }

      Spacer()
      OptionsMenu(preferences: preferences)

      Button { store.refreshAll() } label: {
        if store.isRefreshingAll {
          ProgressView()
            .controlSize(.small)
            .frame(width: 16, height: 16)
        } else {
          Image(systemName: "arrow.clockwise")
        }
      }
      .buttonStyle(.plain)
      .help("모든 계정 새로고침")
      .disabled(store.isRefreshingAll)
    }
  }

  private var authenticationPanel: some View {
    VStack(alignment: .leading, spacing: 8) {
      Label(store.authenticationTitle, systemImage: "person.badge.key.fill")
        .font(.callout.weight(.semibold))

      if let info = store.deviceLoginInfo {
        Text(store.authenticationInstruction)
          .font(.caption)
          .foregroundStyle(.secondary)
          .fixedSize(horizontal: false, vertical: true)
        Text(store.authenticationCompletionNote)
          .font(.caption2)
          .foregroundStyle(.secondary)
          .fixedSize(horizontal: false, vertical: true)
        if store.isAddingAccount {
          VStack(alignment: .leading, spacing: 4) {
            TextField(
              "워크스페이스 이름 (선택)",
              text: Binding(
                get: { store.pendingWorkspaceName },
                set: { store.setPendingWorkspaceName($0) }
              )
            )
            .textFieldStyle(.roundedBorder)
            .controlSize(.small)

            Text("예: 개인, 회사, 개발팀 · 입력하지 않아도 인증은 자동으로 완료됩니다.")
              .font(.caption2)
              .foregroundStyle(.tertiary)
          }
        }
        Text(info.userCode)
          .font(.system(.title3, design: .monospaced, weight: .bold))
          .textSelection(.enabled)
        HStack {
          Button("코드 복사") { store.copyDeviceLoginCode() }
          Button("인증 페이지 열기") { store.reopenDeviceLoginPage() }
          Spacer()
          Button("취소", role: .cancel) { store.cancelCurrentAuthentication() }
        }
        .controlSize(.small)
      } else {
        HStack(spacing: 8) {
          ProgressView().controlSize(.small)
          Text("로그인 준비 중…").font(.caption)
          Spacer()
          Button("취소", role: .cancel) { store.cancelCurrentAuthentication() }
            .controlSize(.small)
        }
      }
    }
    .padding(11)
    .background(Color.accentColor.opacity(0.09), in: RoundedRectangle(cornerRadius: 10))
  }

  private func deleteConfirmationPanel(_ account: UsageAccount) -> some View {
    VStack(alignment: .leading, spacing: 8) {
      Label("\(account.title) 연결을 삭제할까요?", systemImage: "trash.fill")
        .font(.callout.weight(.semibold))

      Text("이 Mac에 저장된 로그인 정보와 전용 Codex 저장소가 함께 삭제됩니다.")
        .font(.caption)
        .foregroundStyle(.secondary)
        .fixedSize(horizontal: false, vertical: true)

      if let workspaceLabel = account.workspaceDisplayLabel {
        Text(workspaceLabel)
          .font(.caption2.monospaced())
          .foregroundStyle(.secondary)
      }

      HStack {
        Button("취소", role: .cancel) { pendingDeletionAccount = nil }
        Spacer()
        Button("연결 삭제", role: .destructive) {
          store.removeManagedAccount(accountID: account.id)
          pendingDeletionAccount = nil
        }
        .buttonStyle(.borderedProminent)
        .tint(.red)
      }
      .controlSize(.small)
    }
    .padding(11)
    .background(Color.red.opacity(0.09), in: RoundedRectangle(cornerRadius: 10))
    .overlay {
      RoundedRectangle(cornerRadius: 10)
        .stroke(Color.red.opacity(0.22), lineWidth: 1)
    }
  }

  private func statusPanel(
    _ message: String,
    systemImage: String,
    color: Color,
    onDismiss: (() -> Void)?
  ) -> some View {
    HStack(alignment: .top, spacing: 8) {
      Image(systemName: systemImage)
        .foregroundStyle(color)
      Text(message)
        .font(.caption)
        .fixedSize(horizontal: false, vertical: true)
      Spacer(minLength: 4)
      if let onDismiss {
        Button(action: onDismiss) {
          Image(systemName: "xmark")
            .font(.caption.weight(.semibold))
        }
        .buttonStyle(.plain)
        .help("닫기")
      }
    }
    .padding(9)
    .background(color.opacity(0.08), in: RoundedRectangle(cornerRadius: 9))
  }

  private var accountActions: some View {
    Button {
      store.startAddingAccount()
    } label: {
      HStack(spacing: 7) {
        if store.isAuthenticating {
          ProgressView().controlSize(.small)
          Text("계정/워크스페이스 인증 중…")
        } else {
          Label("계정 또는 워크스페이스 추가", systemImage: "person.crop.circle.badge.plus")
        }
      }
      .frame(maxWidth: .infinity)
    }
    .buttonStyle(.borderedProminent)
    .controlSize(.regular)
    .disabled(store.isAuthenticating || pendingDeletionAccount != nil)
  }

  private var launchPreferences: some View {
    VStack(alignment: .leading, spacing: 5) {
      Toggle(
        "로그인할 때 자동으로 열기",
        isOn: Binding(
          get: { store.launchAtLoginEnabled },
          set: { store.setLaunchAtLogin($0) }
        )
      )
      .toggleStyle(.switch)
      .controlSize(.small)

      if let error = store.launchAtLoginError {
        Text(error)
          .font(.caption2)
          .foregroundStyle(.red)
          .fixedSize(horizontal: false, vertical: true)
      }
    }
  }

  private var automaticActivationPreferences: some View {
    VStack(alignment: .leading, spacing: 5) {
      Toggle(
        "5시간 한도 자동 시작 유지",
        isOn: Binding(
          get: { store.automaticActivationEnabled },
          set: { store.setAutomaticActivationEnabled($0) }
        )
      )
      .toggleStyle(.switch)
      .controlSize(.small)

      Text(
        "각 연결에 5시간마다 사용 가능한 가장 가벼운 모델의 low 산술 요청 1회를 보냅니다. 소량의 Codex 사용량을 소비하며 앱이 실행 중일 때만 동작합니다."
      )
      .font(.caption2)
      .foregroundStyle(.secondary)
      .fixedSize(horizontal: false, vertical: true)

      if store.automaticActivationEnabled {
        if !store.automaticActivationInProgressAccountIDs.isEmpty {
          HStack(spacing: 5) {
            ProgressView().controlSize(.mini)
            Text(
              "자동 요청 중 · \(store.automaticActivationInProgressAccountIDs.count)개 연결"
            )
          }
          .font(.caption2)
          .foregroundStyle(.secondary)
        } else if let nextDate = store.nextAutomaticActivationDate {
          Text(
            "다음 자동 요청 \(nextDate.formatted(date: .abbreviated, time: .shortened))"
          )
          .font(.caption2)
          .foregroundStyle(.tertiary)
        }
      }

      if let error = store.automaticActivationError {
        Text(error)
          .font(.caption2)
          .foregroundStyle(.red)
          .lineLimit(2)
          .fixedSize(horizontal: false, vertical: true)
      }
    }
  }

  private var accountListHeight: CGFloat {
    let spacing = CGFloat(max(0, store.accountStates.count - 1)) * 10
    let desiredHeight = store.accountStates.reduce(CGFloat.zero) { partial, viewState in
      partial + estimatedCardHeight(viewState)
    } + spacing + 2
    return min(max(118, desiredHeight), maximumAccountListHeight)
  }

  private var maximumAccountListHeight: CGFloat {
    let currentScreen = NSScreen.screens.first { $0.frame.contains(NSEvent.mouseLocation) }
      ?? NSScreen.main
    let visibleHeight = currentScreen?.visibleFrame.height ?? 900
    var reservedHeight: CGFloat = 315

    if store.isAuthenticating || store.deviceLoginInfo != nil {
      reservedHeight += store.deviceLoginInfo == nil ? 72 : (store.isAddingAccount ? 205 : 150)
    }
    if pendingDeletionAccount != nil { reservedHeight += 124 }
    if store.accountManagementError != nil || store.accountManagementNotice != nil {
      reservedHeight += 54
    }
    if store.authenticationError != nil { reservedHeight += 54 }

    return min(580, max(118, visibleHeight - reservedHeight))
  }

  private func estimatedCardHeight(_ viewState: UsageStore.AccountViewState) -> CGFloat {
    switch viewState.state {
    case .loaded(let snapshot):
      return ((snapshot.availableResetCredits ?? 0) > 0 ? 174 : 152) + 24
    case .loading, .needsAuthentication, .failed:
      return 132
    }
  }

  private func revealRecentlyAddedAccount(using proxy: ScrollViewProxy) {
    guard let accountID = store.recentlyAddedAccountID else { return }
    withAnimation(.easeOut(duration: 0.2)) {
      proxy.scrollTo(accountID, anchor: .bottom)
    }
    store.consumeRecentlyAddedAccount(accountID)
  }

  private var footer: some View {
    HStack {
      if let fetchedAt = store.latestFetchedAt {
        Text("업데이트 \(fetchedAt.formatted(date: .omitted, time: .shortened))")
          .font(.caption2)
          .foregroundStyle(.tertiary)
      }
      Spacer()
      Button("종료") { NSApplication.shared.terminate(nil) }
        .keyboardShortcut("q")
        .controlSize(.small)
    }
  }
}

private struct AccountUsageCard: View {
  let viewState: UsageStore.AccountViewState
  @ObservedObject var store: UsageStore
  let onRequestDelete: () -> Void

  @State private var isRenaming = false
  @State private var draftName = ""
  @State private var isRenamingWorkspace = false
  @State private var draftWorkspaceName = ""

  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
      accountHeader

      workspaceRow

      switch viewState.state {
      case .loading:
        loadingView
      case .loaded(let snapshot):
        usageView(snapshot)
      case .needsAuthentication:
        authenticationRequiredView
      case .failed(let error):
        errorView(error)
      }
    }
    .padding(12)
    .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 11))
    .overlay {
      RoundedRectangle(cornerRadius: 11)
        .stroke(.quaternary, lineWidth: 1)
    }
  }

  @ViewBuilder
  private var accountHeader: some View {
    HStack(spacing: 7) {
      if isRenaming {
        TextField("표시 이름", text: $draftName)
          .textFieldStyle(.roundedBorder)
          .onSubmit { saveName() }
        Button { saveName() } label: { Image(systemName: "checkmark") }
          .buttonStyle(.plain)
        Button { isRenaming = false } label: { Image(systemName: "xmark") }
          .buttonStyle(.plain)
      } else {
        Text(viewState.account.title)
          .font(.callout.weight(.semibold))
          .lineLimit(1)

        if viewState.account.isSystemDefault {
          Text("기본")
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 5)
            .padding(.vertical, 2)
            .background(.quaternary, in: Capsule())
        }

        Spacer()
        if viewState.isRefreshing {
          ProgressView().controlSize(.small)
        }
        accountMenu
      }
    }
  }

  private var accountMenu: some View {
    Menu {
      Button("새로고침", systemImage: "arrow.clockwise") {
        store.refresh(accountID: viewState.id)
      }

      if viewState.account.isManaged {
        Button("계정 표시 이름 변경", systemImage: "pencil") {
          draftName = viewState.account.displayName ?? viewState.account.title
          isRenaming = true
        }
        Button("다시 로그인", systemImage: "person.badge.key") {
          store.relogin(accountID: viewState.id)
        }
        Divider()
        Button("연결 삭제", systemImage: "trash", role: .destructive) {
          onRequestDelete()
        }
      }
    } label: {
      Image(systemName: "ellipsis.circle")
    }
    .menuStyle(.borderlessButton)
    .fixedSize()
  }

  @ViewBuilder
  private var workspaceRow: some View {
    HStack(spacing: 6) {
      if isRenamingWorkspace {
        Image(systemName: "building.2")
          .foregroundStyle(.secondary)
        TextField("워크스페이스 이름", text: $draftWorkspaceName)
          .textFieldStyle(.roundedBorder)
          .controlSize(.small)
          .onSubmit { saveWorkspaceName() }
        Button { saveWorkspaceName() } label: {
          Image(systemName: "checkmark")
        }
        .buttonStyle(.plain)
        .help("워크스페이스 이름 저장")
        Button { isRenamingWorkspace = false } label: {
          Image(systemName: "xmark")
        }
        .buttonStyle(.plain)
        .help("취소")
      } else {
        Label(
          viewState.account.workspaceDisplayLabel ?? "워크스페이스 이름 미지정",
          systemImage: "building.2"
        )
        .font(.caption2)
        .foregroundStyle(.secondary)
        .lineLimit(1)
        .help(workspaceHelp)

        Button {
          draftWorkspaceName = viewState.account.normalizedWorkspaceName ?? ""
          isRenamingWorkspace = true
        } label: {
          Image(systemName: "pencil")
            .font(.caption2.weight(.semibold))
        }
        .buttonStyle(.plain)
        .help("워크스페이스 이름 수정")

        Spacer(minLength: 4)

        if viewState.account.normalizedWorkspaceName != nil,
          let reference = viewState.account.workspaceReference
        {
          Text("#\(reference)")
            .font(.caption2.monospaced())
            .foregroundStyle(.tertiary)
            .help("중복 연결 판별용 로컬 참조값")
        }
      }
    }
  }

  private var workspaceHelp: String {
    if viewState.account.normalizedWorkspaceName != nil {
      return "이 Mac에서 직접 지정한 워크스페이스 표시 이름입니다."
    }
    return "실제 워크스페이스 이름은 자동 조회되지 않습니다. 연필 버튼으로 알아보기 쉬운 이름을 지정하세요."
  }

  private var loadingView: some View {
    HStack(spacing: 8) {
      ProgressView().controlSize(.small)
      Text("사용량을 확인하는 중…")
        .font(.caption)
        .foregroundStyle(.secondary)
    }
    .frame(maxWidth: .infinity, minHeight: 54, alignment: .center)
  }

  private func usageView(_ snapshot: UsageSnapshot) -> some View {
    HStack(alignment: .top, spacing: 12) {
      DualUsageRing(
        fiveHourLimit: snapshot.fiveHourLimit,
        weeklyLimit: snapshot.weeklyLimit
      )
      .frame(width: 98, height: 98)

      VStack(alignment: .leading, spacing: 6) {
        if let email = snapshot.accountEmail, !email.isEmpty {
          Label(email, systemImage: "person.crop.circle")
            .font(.caption.weight(.medium))
            .lineLimit(1)
            .truncationMode(.middle)
            .help(email)
        }

        if let plan = snapshot.planDisplayName {
          Label("ChatGPT \(plan)", systemImage: "creditcard")
            .font(.caption)
            .foregroundStyle(.secondary)
        }

        if let fiveHourLimit = snapshot.fiveHourLimit {
          limitResetView("5시간", limit: fiveHourLimit, color: .accentColor)
        }

        if let weeklyLimit = snapshot.weeklyLimit {
          limitResetView("주간", limit: weeklyLimit, color: .purple)
        }

        resetCreditsView(snapshot)
      }
      .labelStyle(.titleAndIcon)
      .frame(maxWidth: .infinity, alignment: .leading)
    }
    .accessibilityElement(children: .combine)
    .accessibilityLabel(accessibilityUsageLabel(snapshot))
  }

  private func limitResetView(
    _ title: String,
    limit: UsageLimitWindow,
    color: Color
  ) -> some View {
    HStack(alignment: .firstTextBaseline, spacing: 5) {
      Circle()
        .fill(color)
        .frame(width: 6, height: 6)

      if let resetsAt = limit.resetsAt {
        let countdown = limit.resetCountdown() ?? "곧 초기화"
        Text(
          countdown == "곧 초기화"
            ? "\(title) 곧 초기화 · \(resetsAt.formatted(date: .abbreviated, time: .shortened))"
            : "\(title) \(countdown) 후 · \(resetsAt.formatted(date: .abbreviated, time: .shortened))"
        )
      } else {
        Text("\(title) 초기화 시각 정보 없음")
      }
    }
    .font(.caption2)
    .foregroundStyle(.secondary)
    .fixedSize(horizontal: false, vertical: true)
  }

  private func accessibilityUsageLabel(_ snapshot: UsageSnapshot) -> String {
    var limits: [String] = []
    if let fiveHour = snapshot.fiveHourLimit {
      limits.append("5시간 한도 \(fiveHour.remainingPercent)퍼센트 남음")
    }
    if let weekly = snapshot.weeklyLimit {
      limits.append("주간 한도 \(weekly.remainingPercent)퍼센트 남음")
    }
    let workspace = viewState.account.workspaceDisplayLabel.map { ", \($0)" } ?? ""
    return "\(viewState.account.title)\(workspace), Codex " + limits.joined(separator: ", ")
  }

  @ViewBuilder
  private func resetCreditsView(_ snapshot: UsageSnapshot) -> some View {
    if let count = snapshot.availableResetCredits, count > 0 {
      VStack(alignment: .leading, spacing: 2) {
        Label("리셋 크레딧 \(count)개", systemImage: "arrow.counterclockwise.circle")
          .font(.caption.weight(.medium))

        if let expiresAt = snapshot.earliestResetCreditExpiration {
          Text(
            "\(snapshot.hasCompleteResetCreditDetails ? "가장 빠른 소멸" : "확인된 크레딧 소멸") "
              + expiresAt.formatted(date: .abbreviated, time: .shortened)
          )
          .font(.caption2)
          .foregroundStyle(.secondary)
          if let countdown = snapshot.resetCreditExpirationCountdown() {
            Text(countdown == "곧 소멸" ? countdown : "\(countdown) 후")
              .font(.caption2)
              .foregroundStyle(.secondary)
          }
        } else {
          Text("소멸 시각 정보 없음")
            .font(.caption2)
            .foregroundStyle(.secondary)
        }

        if snapshot.resetCreditDetails != nil,
          Int64(snapshot.availableResetCreditDetails.count) < count
        {
          Text("상세 정보 \(snapshot.availableResetCreditDetails.count)/\(count)개 제공됨")
            .font(.caption2)
            .foregroundStyle(.tertiary)
        }
      }
    }
  }

  private var authenticationRequiredView: some View {
    VStack(alignment: .leading, spacing: 8) {
      Label(
        "계정/워크스페이스 연결이 필요합니다",
        systemImage: "person.crop.circle.badge.exclamationmark"
      )
        .font(.caption.weight(.semibold))
        .foregroundStyle(.orange)

      if viewState.account.isSystemDefault {
        Text("현재 머신에서 Codex CLI가 사용하는 .codex 폴더를 연결합니다. 다시 로그인하지 않습니다.")
          .font(.caption2)
          .foregroundStyle(.secondary)
          .fixedSize(horizontal: false, vertical: true)
        Button("기본 Codex 로그인 연결") { store.connectExistingCodexLogin() }
          .controlSize(.small)
      } else {
        Button("다시 로그인") { store.relogin(accountID: viewState.id) }
          .controlSize(.small)
      }
    }
    .frame(maxWidth: .infinity, minHeight: 54, alignment: .leading)
  }

  private func errorView(_ error: CodexUsageError) -> some View {
    VStack(alignment: .leading, spacing: 8) {
      Label(
        error.errorDescription ?? "사용량을 가져오지 못했습니다.",
        systemImage: "exclamationmark.circle.fill"
      )
      .font(.caption.weight(.semibold))
      .foregroundStyle(.orange)
      .fixedSize(horizontal: false, vertical: true)

      Button("다시 시도") { store.refresh(accountID: viewState.id) }
        .controlSize(.small)
    }
    .frame(maxWidth: .infinity, minHeight: 54, alignment: .leading)
  }

  private func saveName() {
    store.renameManagedAccount(accountID: viewState.id, displayName: draftName)
    isRenaming = false
  }

  private func saveWorkspaceName() {
    store.renameWorkspace(accountID: viewState.id, workspaceName: draftWorkspaceName)
    isRenamingWorkspace = false
  }
}

struct DualUsageRing: View {
  let fiveHourLimit: UsageLimitWindow?
  let weeklyLimit: UsageLimitWindow?

  var body: some View {
    ZStack {
      Circle()
        .stroke(Color.accentColor.opacity(0.14), lineWidth: 8)
        .frame(width: 92, height: 92)
      progressRing(limit: fiveHourLimit, baseColor: .accentColor, size: 92, lineWidth: 8)

      Circle()
        .stroke(Color.purple.opacity(0.14), lineWidth: 7)
        .frame(width: 68, height: 68)
      progressRing(limit: weeklyLimit, baseColor: .purple, size: 68, lineWidth: 7)

      VStack(spacing: 1) {
        limitValue("5h", limit: fiveHourLimit, color: .accentColor)
        limitValue("7d", limit: weeklyLimit, color: .purple)
      }
    }
  }

  @ViewBuilder
  private func progressRing(
    limit: UsageLimitWindow?,
    baseColor: Color,
    size: CGFloat,
    lineWidth: CGFloat
  ) -> some View {
    if let limit {
      let progress = Double(limit.remainingPercent) / 100
      Circle()
        .trim(from: 0, to: progress)
        .stroke(
          progressColor(base: baseColor, progress: progress),
          style: StrokeStyle(lineWidth: lineWidth, lineCap: .round)
        )
        .rotationEffect(.degrees(-90))
        .frame(width: size, height: size)
    }
  }

  private func limitValue(
    _ label: String,
    limit: UsageLimitWindow?,
    color: Color
  ) -> some View {
    HStack(alignment: .firstTextBaseline, spacing: 3) {
      Text(label)
        .foregroundStyle(color)
      Text(limit.map { "\($0.remainingPercent)%" } ?? "—")
        .foregroundStyle(.primary)
    }
    .font(.system(size: 9.5, weight: .semibold, design: .rounded).monospacedDigit())
  }

  private func progressColor(base: Color, progress: Double) -> Color {
    if progress <= 0.1 { return .red }
    if progress <= 0.3 { return .orange }
    return base
  }
}
