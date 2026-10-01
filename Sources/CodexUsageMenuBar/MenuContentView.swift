import AppKit
import SwiftUI
import UniformTypeIdentifiers

struct MenuContentView: View {
  @ObservedObject var store: UsageStore
  @ObservedObject var preferences: AppPreferences
  @State private var pendingDeletionAccount: UsageAccount?
  @State private var draggedAccountID: String?
  @State private var previewOrder: [String] = []

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
    .onDisappear { endAccountDrag() }
    .onChange(of: store.accountStates.map(\.id)) { accountIDs in
      endAccountDrag()
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
          ForEach(visibleAccountStates) { accountState in
            AccountUsageCard(
              viewState: accountState,
              store: store,
              preferences: preferences,
              onRequestDelete: { pendingDeletionAccount = accountState.account },
              onBeginDrag: {
                previewOrder = store.accountStates.map(\.id)
                draggedAccountID = accountState.id
              },
              onEndDrag: endAccountDrag
            )
            .id(accountState.id)
            .opacity(draggedAccountID == accountState.id ? 0.6 : 1)
            .onDrop(
              of: [.codexUsageAccountOrder],
              delegate: AccountCardDropDelegate(
                targetID: accountState.id,
                draggedAccountID: $draggedAccountID,
                previewOrder: $previewOrder,
                onCommit: store.reorderAccounts
              )
            )
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

  private var visibleAccountStates: [UsageStore.AccountViewState] {
    guard draggedAccountID != nil else { return store.accountStates }
    let order = AccountOrder.normalized(previewOrder, availableIDs: store.accountStates.map(\.id))
    return order.compactMap { id in store.accountStates.first { $0.id == id } }
  }

  private func endAccountDrag() {
    draggedAccountID = nil
    previewOrder = []
  }

  private var header: some View {
    HStack(spacing: 10) {
      Image(systemName: "chart.bar.fill")
        .font(.system(size: 17, weight: .semibold))
        .foregroundStyle(.tint)

      VStack(alignment: .leading, spacing: 1) {
        Text(L10n.text("app.title"))
          .font(.headline)
        Text(L10n.text("header.connections", store.accountStates.count))
          .font(.caption)
          .foregroundStyle(.secondary)
          .lineLimit(1)
          .minimumScaleFactor(0.8)
      }

      Spacer()
      OptionsMenu(preferences: preferences, store: store)

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
      .help(L10n.text("header.refresh_all"))
      .accessibilityLabel(L10n.text("header.refresh_all"))
      .disabled(!store.canRequestManualRefresh)
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
              L10n.text("auth.workspace_optional"),
              text: Binding(
                get: { store.pendingWorkspaceName },
                set: { store.setPendingWorkspaceName($0) }
              )
            )
            .textFieldStyle(.roundedBorder)
            .controlSize(.small)

            Text(L10n.text("auth.workspace_examples"))
              .font(.caption2)
              .foregroundStyle(.tertiary)
              .fixedSize(horizontal: false, vertical: true)
          }
        }
        Text(info.userCode)
          .font(.system(.title3, design: .monospaced, weight: .bold))
          .textSelection(.enabled)
        HStack {
          Button(L10n.text("auth.copy_code")) { store.copyDeviceLoginCode() }
          Button(L10n.text("auth.open_page")) { store.reopenDeviceLoginPage() }
          Spacer()
          Button(L10n.text("common.cancel"), role: .cancel) { store.cancelCurrentAuthentication() }
        }
        .controlSize(.small)
      } else {
        HStack(spacing: 8) {
          ProgressView().controlSize(.small)
          Text(L10n.text("auth.preparing")).font(.caption)
          Spacer()
          Button(L10n.text("common.cancel"), role: .cancel) { store.cancelCurrentAuthentication() }
            .controlSize(.small)
        }
      }
    }
    .padding(11)
    .background(Color.accentColor.opacity(0.09), in: RoundedRectangle(cornerRadius: 10))
  }

  private func deleteConfirmationPanel(_ account: UsageAccount) -> some View {
    VStack(alignment: .leading, spacing: 8) {
      Label(L10n.text("account.delete_title", account.title), systemImage: "trash.fill")
        .font(.callout.weight(.semibold))

      Text(L10n.text("account.delete_explanation"))
        .font(.caption)
        .foregroundStyle(.secondary)
        .fixedSize(horizontal: false, vertical: true)

      if let workspaceLabel = account.workspaceDisplayLabel {
        Text(workspaceLabel)
          .font(.caption2.monospaced())
          .foregroundStyle(.secondary)
      }

      HStack {
        Button(L10n.text("common.cancel"), role: .cancel) { pendingDeletionAccount = nil }
        Spacer()
        Button(L10n.text("account.delete"), role: .destructive) {
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
        .help(L10n.text("common.close"))
        .accessibilityLabel(L10n.text("common.close"))
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
          Text(L10n.text("auth.signing_in"))
        } else {
          Label(L10n.text("auth.add"), systemImage: "person.crop.circle.badge.plus")
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
        L10n.text("options.launch"),
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
      HStack(spacing: 8) {
        Toggle(
          L10n.text("auto.title"),
          isOn: Binding(
            get: { store.automaticActivationEnabled },
            set: { store.setAutomaticActivationEnabled($0) }
          )
        )
        .toggleStyle(.switch)
        .controlSize(.small)

        Button { ProductInformationPresenter.shared.show(.automaticRefresh, preferences: preferences) } label: {
          Image(systemName: "questionmark.circle")
            .font(.system(size: 14))
            .frame(width: 22, height: 22)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(L10n.text("auto.help_button"))
        .accessibilityLabel(L10n.text("auto.help_button"))
      }

      if store.automaticActivationEnabled {
        if !store.automaticActivationInProgressAccountIDs.isEmpty {
          HStack(spacing: 5) {
            ProgressView().controlSize(.mini)
            Text(
              L10n.text("auto.in_progress", store.automaticActivationInProgressAccountIDs.count)
            )
          }
          .font(.caption2)
          .foregroundStyle(.secondary)
        } else if let nextDate = store.nextAutomaticActivationDate {
          Text(
            L10n.text("auto.next", L10n.date(nextDate))
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
    var reservedHeight: CGFloat = 280

    if store.isAuthenticating || store.deviceLoginInfo != nil {
      if store.deviceLoginInfo == nil {
        reservedHeight += 72
      } else {
        // Localized instructions can be substantially taller than Korean.
        reservedHeight += 142 + textHeight(store.authenticationInstruction, size: 11)
          + textHeight(store.authenticationCompletionNote, size: 10)
        if store.isAddingAccount {
          reservedHeight += 33 + textHeight(L10n.text("auth.workspace_examples"), size: 10)
        }
      }
    }
    if let account = pendingDeletionAccount {
      reservedHeight += 100 + textHeight(L10n.text("account.delete_title", account.title), size: 13)
        + textHeight(L10n.text("account.delete_explanation"), size: 11)
    }
    if let message = store.accountManagementError ?? store.accountManagementNotice {
      reservedHeight += max(54, textHeight(message, size: 11) + 26)
    }
    if let message = store.authenticationError {
      reservedHeight += max(54, textHeight(message, size: 11) + 26)
    }
    if store.automaticActivationEnabled { reservedHeight += 20 }
    if store.automaticActivationError != nil { reservedHeight += 34 }
    if let message = store.launchAtLoginError { reservedHeight += textHeight(message, size: 10) + 5 }

    return min(580, max(118, visibleHeight - reservedHeight))
  }

  private func textHeight(_ text: String, size: CGFloat) -> CGFloat {
    ceil((text as NSString).boundingRect(
      with: NSSize(width: 356, height: CGFloat.greatestFiniteMagnitude),
      options: [.usesLineFragmentOrigin, .usesFontLeading],
      attributes: [.font: NSFont.systemFont(ofSize: size)]).height)
  }

  private func estimatedCardHeight(_ viewState: UsageStore.AccountViewState) -> CGFloat {
    switch viewState.state {
    case .loaded(let snapshot):
      return snapshot.fiveHourLimit != nil && snapshot.weeklyLimit != nil ? 112 : 104
    case .loading, .needsAuthentication, .failed:
      return 106
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
        Text(L10n.text("usage.updated", L10n.date(fetchedAt, style: .omitted)))
          .font(.caption2)
          .foregroundStyle(.tertiary)
      }
      Spacer()
      Button(L10n.text("common.quit")) { NSApplication.shared.terminate(nil) }
        .keyboardShortcut("q")
        .controlSize(.small)
    }
  }
}

struct AccountUsageCard: View {
  let viewState: UsageStore.AccountViewState
  @ObservedObject var store: UsageStore
  @ObservedObject var preferences: AppPreferences
  let onRequestDelete: () -> Void
  var onBeginDrag: () -> Void = {}
  var onEndDrag: () -> Void = {}

  @State private var isRenaming = false
  @State private var draftName = ""
  @State private var isRenamingWorkspace = false
  @State private var draftWorkspaceName = ""

  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      accountHeader

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
    .padding(10)
    .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 11))
    .overlay {
      RoundedRectangle(cornerRadius: 11)
        .stroke(.quaternary, lineWidth: 1)
    }
  }

  @ViewBuilder
  private var accountHeader: some View {
    HStack(spacing: 5) {
      if isRenaming {
        TextField(L10n.text("account.display_name"), text: $draftName)
          .textFieldStyle(.roundedBorder)
          .onSubmit { saveName() }
        Button { saveName() } label: { Image(systemName: "checkmark") }
          .buttonStyle(.plain)
          .help(L10n.text("common.save"))
          .accessibilityLabel(L10n.text("common.save"))
        Button { isRenaming = false } label: { Image(systemName: "xmark") }
          .buttonStyle(.plain)
          .help(L10n.text("common.cancel"))
          .accessibilityLabel(L10n.text("common.cancel"))
      } else {
        Text(headerTitle)
          .font(.caption.weight(.semibold))
          .lineLimit(1)
          .truncationMode(.middle)
          .frame(maxWidth: .infinity, alignment: .leading)
          .help(headerTitle)

        if viewState.account.isSystemDefault {
          Text(L10n.text("common.default"))
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 4)
            .padding(.vertical, 2)
            .background(.quaternary, in: Capsule())
        }

        workspaceControl
        if let error = viewState.lastRefreshError {
          Image(systemName: "exclamationmark.triangle.fill")
            .font(.caption2)
            .foregroundStyle(.orange)
            .help(
              L10n.text("usage.last_error", error.errorDescription ?? L10n.text("common.unknown_error"))
            )
            .accessibilityLabel(L10n.text("usage.last_error_accessibility"))
        }
        Button { store.refresh(accountID: viewState.id) } label: {
          if viewState.isRefreshing {
            ProgressView().controlSize(.small).frame(width: 22, height: 22)
          } else {
            Image(systemName: "arrow.clockwise").frame(width: 22, height: 22)
              .contentShape(Rectangle())
          }
        }
        .buttonStyle(.plain)
        .disabled(viewState.isRefreshing || !store.canRequestManualRefresh)
        .help(L10n.text("common.refresh"))
        .accessibilityLabel(L10n.text("common.refresh"))
        accountMenu
        AccountDragHandle(
          accountID: viewState.id,
          label: headerTitle,
          onBegin: onBeginDrag,
          onEnd: onEndDrag
        )
      }
    }
  }

  private var headerTitle: String {
    let email: String?
    if case .loaded(let snapshot) = viewState.state {
      email = snapshot.accountEmail ?? viewState.account.lastKnownEmail
    } else {
      email = viewState.account.lastKnownEmail
    }
    guard let email, !email.isEmpty else { return viewState.account.title }
    if viewState.account.isManaged, viewState.account.title != email {
      return "\(viewState.account.title) · \(email)"
    }
    return email
  }

  private var accountMenu: some View {
    Menu {
      Button(L10n.text("account.rename_workspace"), systemImage: "text.cursor") {
        draftWorkspaceName = viewState.account.normalizedWorkspaceName ?? ""
        isRenamingWorkspace = true
      }

      if viewState.account.isManaged {
        Button(L10n.text("account.rename"), systemImage: "text.cursor") {
          draftName = viewState.account.normalizedDisplayName ?? ""
          isRenaming = true
        }
        Button(L10n.text("common.relogin"), systemImage: "person.badge.key") {
          store.relogin(accountID: viewState.id)
        }
        Divider()
        Button(L10n.text("account.delete"), systemImage: "trash", role: .destructive) {
          onRequestDelete()
        }
      }
    } label: {
      Image(systemName: "ellipsis.circle")
    }
    .menuStyle(.borderlessButton)
    .menuIndicator(.hidden)
    .fixedSize()
    .help(L10n.text("common.options"))
    .accessibilityLabel(L10n.text("common.options"))
  }

  @ViewBuilder
  private var workspaceControl: some View {
    HStack(spacing: 4) {
      if isRenamingWorkspace {
        TextField(L10n.text("account.workspace_name"), text: $draftWorkspaceName)
          .textFieldStyle(.roundedBorder)
          .controlSize(.small)
          .frame(width: 82)
          .onSubmit { saveWorkspaceName() }
        Button { saveWorkspaceName() } label: {
          Image(systemName: "checkmark")
        }
        .buttonStyle(.plain)
        .help(L10n.text("common.save"))
        .accessibilityLabel(L10n.text("common.save"))
        Button { isRenamingWorkspace = false } label: {
          Image(systemName: "xmark")
        }
        .buttonStyle(.plain)
        .help(L10n.text("common.cancel"))
        .accessibilityLabel(L10n.text("common.cancel"))
      } else if viewState.account.normalizedWorkspaceName != nil || viewState.account.isSystemDefault {
        Text(viewState.account.normalizedWorkspaceName ?? viewState.account.shortDisplayReference)
          .font(.caption2)
          .foregroundStyle(.secondary)
          .lineLimit(1)
          .frame(maxWidth: 78)
          .help(workspaceHelp)

      }
    }
    .font(.caption2)
    .fixedSize(horizontal: true, vertical: false)
  }

  private var workspaceHelp: String {
    if viewState.account.normalizedWorkspaceName != nil {
      return L10n.text("account.workspace_help_named")
    }
    return L10n.text("account.workspace_help_unnamed")
  }

  private var loadingView: some View {
    HStack(spacing: 8) {
      ProgressView().controlSize(.small)
      Text(L10n.text("usage.loading"))
        .font(.caption)
        .foregroundStyle(.secondary)
    }
    .frame(maxWidth: .infinity, minHeight: 54, alignment: .center)
  }

  private func usageView(_ snapshot: UsageSnapshot) -> some View {
    let ringSize: CGFloat = snapshot.fiveHourLimit != nil && snapshot.weeklyLimit != nil ? 64 : 56
    return HStack(alignment: .center, spacing: 10) {
      DualUsageRing(
        fiveHourLimit: snapshot.fiveHourLimit,
        weeklyLimit: snapshot.weeklyLimit,
        diameter: ringSize
      )
      .frame(width: ringSize, height: ringSize)

      VStack(alignment: .leading, spacing: 5) {
        if let fiveHourLimit = snapshot.fiveHourLimit {
          limitResetView("5h:", limit: fiveHourLimit, color: .accentColor)
        }

        if let weeklyLimit = snapshot.weeklyLimit {
          limitResetView(L10n.text("common.weekly"), limit: weeklyLimit, color: .purple)
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
        let countdown = limit.resetCountdown() ?? L10n.text("usage.reset_soon")
        Text(
          L10n.text(resetsAt <= Date() ? "usage.reset_line" : "usage.reset_after",
            title, countdown, CompactUsageDate.string(resetsAt))
        )
        .help(L10n.text("usage.reset_line", title, countdown, L10n.date(resetsAt, style: .complete)))
      } else {
        Text(L10n.text("usage.reset_unknown", title))
      }
    }
    .font(.caption2)
    .foregroundStyle(.secondary)
    .lineLimit(1)
    .minimumScaleFactor(0.8)
  }

  private func accessibilityUsageLabel(_ snapshot: UsageSnapshot) -> String {
    var limits: [String] = []
    if let fiveHour = snapshot.fiveHourLimit {
      limits.append(L10n.text("usage.five_remaining", fiveHour.remainingPercent))
    }
    if let weekly = snapshot.weeklyLimit {
      limits.append(L10n.text("usage.weekly_remaining", weekly.remainingPercent))
    }
    let workspace = viewState.account.workspaceDisplayLabel.map { ", \($0)" } ?? ""
    return "\(viewState.account.title)\(workspace), Codex " + limits.joined(separator: ", ")
  }

  @ViewBuilder
  private func resetCreditsView(_ snapshot: UsageSnapshot) -> some View {
    if let summary = CompactResetCreditSummary(snapshot: snapshot) {
      Text(summary.text)
        .font(.caption2)
        .foregroundStyle(.secondary)
        .lineLimit(1)
        .minimumScaleFactor(0.72)
        .help(summary.detail)
        .accessibilityLabel(summary.detail)
    }
  }

  private var authenticationRequiredView: some View {
    VStack(alignment: .leading, spacing: 8) {
      Label(
        L10n.text("auth.connection_required"),
        systemImage: "person.crop.circle.badge.exclamationmark"
      )
        .font(.caption.weight(.semibold))
        .foregroundStyle(.orange)

      if viewState.account.isSystemDefault {
        Text(L10n.text("auth.default_explanation"))
          .font(.caption2)
          .foregroundStyle(.secondary)
          .fixedSize(horizontal: false, vertical: true)
        Button(L10n.text("auth.connect_default")) { store.connectExistingCodexLogin() }
          .controlSize(.small)
      } else {
        Button(L10n.text("common.relogin")) { store.relogin(accountID: viewState.id) }
          .controlSize(.small)
      }
    }
    .frame(maxWidth: .infinity, minHeight: 54, alignment: .leading)
  }

  private func errorView(_ error: CodexUsageError) -> some View {
    VStack(alignment: .leading, spacing: 8) {
      Label(
        error.errorDescription ?? L10n.text("common.unknown_error"),
        systemImage: "exclamationmark.circle.fill"
      )
      .font(.caption.weight(.semibold))
      .foregroundStyle(.orange)
      .fixedSize(horizontal: false, vertical: true)

      Button(L10n.text("common.retry")) { store.refresh(accountID: viewState.id) }
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
  var diameter: CGFloat = 98

  private var hasBothLimits: Bool { fiveHourLimit != nil && weeklyLimit != nil }
  private var outerSize: CGFloat { diameter - 6 }

  var body: some View {
    ZStack {
      if hasBothLimits {
        ring(limit: fiveHourLimit, color: .accentColor, size: outerSize, lineWidth: diameter * 0.082)
        ring(limit: weeklyLimit, color: .purple, size: outerSize * 0.739, lineWidth: diameter * 0.071)
      } else {
        ring(
          limit: fiveHourLimit ?? weeklyLimit,
          color: fiveHourLimit != nil ? .accentColor : .purple,
          size: outerSize,
          lineWidth: diameter * 0.082
        )
      }

      VStack(spacing: 1) {
        if fiveHourLimit != nil || weeklyLimit == nil {
          limitValue("5h", limit: fiveHourLimit, color: .accentColor)
        }
        if weeklyLimit != nil {
          limitValue("7d", limit: weeklyLimit, color: .purple)
        }
      }
    }
    .frame(width: diameter, height: diameter)
  }

  private func ring(limit: UsageLimitWindow?, color: Color, size: CGFloat, lineWidth: CGFloat) -> some View {
    ZStack {
      Circle()
        .stroke(color.opacity(0.14), lineWidth: lineWidth)
        .frame(width: size, height: size)
      progressRing(limit: limit, baseColor: color, size: size, lineWidth: lineWidth)
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
    .font(
      .system(
        size: hasBothLimits ? max(7.8, 9.5 * diameter / 98) : 9.5,
        weight: .semibold, design: .rounded
      ).monospacedDigit()
    )
  }

  private func progressColor(base: Color, progress: Double) -> Color {
    if progress <= 0.1 { return .red }
    if progress <= 0.3 { return .orange }
    return base
  }
}
