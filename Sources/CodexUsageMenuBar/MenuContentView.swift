import AppKit
import SwiftUI

struct MenuContentView: View {
  @ObservedObject var store: UsageStore
  @ObservedObject var preferences: AppPreferences

  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      header

      if store.isAuthenticating || store.deviceLoginInfo != nil {
        authenticationPanel
      }

      if let confirmation = store.pendingAccountConfirmation {
        accountConfirmationPanel(confirmation)
      }

      ScrollView {
        LazyVStack(spacing: 10) {
          ForEach(store.accountStates) { accountState in
            AccountUsageCard(viewState: accountState, store: store)
          }
        }
        .padding(.vertical, 1)
      }
      .frame(maxHeight: 500)

      if let error = store.authenticationError {
        Label(error, systemImage: "exclamationmark.circle.fill")
          .font(.caption)
          .foregroundStyle(.red)
          .fixedSize(horizontal: false, vertical: true)
      }

      Divider()
      accountActions
      launchPreferences
      footer
    }
    .padding(14)
    .frame(width: 390)
    .onAppear { store.refreshIfStale() }
    .alert(
      "계정 관리 오류",
      isPresented: Binding(
        get: { store.accountManagementError != nil },
        set: { if !$0 { store.clearAccountManagementError() } }
      )
    ) {
      Button("확인") { store.clearAccountManagementError() }
    } message: {
      Text(store.accountManagementError ?? "계정 정보를 저장하지 못했습니다.")
    }
  }

  private var header: some View {
    HStack(spacing: 10) {
      Image(systemName: "chart.bar.fill")
        .font(.system(size: 17, weight: .semibold))
        .foregroundStyle(.tint)

      VStack(alignment: .leading, spacing: 1) {
        Text("Codex Usage")
          .font(.headline)
        Text("계정 \(store.accountStates.count)개 · 주간 남은 사용량")
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
      Label("ChatGPT 계정 로그인", systemImage: "person.badge.key.fill")
        .font(.callout.weight(.semibold))

      if let info = store.deviceLoginInfo {
        Text("열린 브라우저에서 아래 코드를 입력하고, 추가할 계정으로 로그인하세요.")
          .font(.caption)
          .foregroundStyle(.secondary)
          .fixedSize(horizontal: false, vertical: true)
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

  private func accountConfirmationPanel(
    _ confirmation: UsageStore.PendingAccountConfirmation
  ) -> some View {
    VStack(alignment: .leading, spacing: 8) {
      Label("이 계정을 추가할까요?", systemImage: "person.crop.circle.badge.checkmark")
        .font(.callout.weight(.semibold))

      if let email = confirmation.snapshot.accountEmail {
        Text(email)
          .font(.callout.weight(.medium))
          .textSelection(.enabled)
      }
      if let plan = confirmation.snapshot.planDisplayName {
        Text("ChatGPT \(plan) 플랜")
          .font(.caption)
          .foregroundStyle(.secondary)
      }
      Text("주간 한도 \(confirmation.snapshot.remainingPercent)% 남음")
        .font(.caption)
        .foregroundStyle(.secondary)

      HStack {
        Button("계정 추가") { store.confirmPendingAccount() }
          .buttonStyle(.borderedProminent)
        Button("취소", role: .cancel) { store.cancelPendingAccount() }
      }
      .controlSize(.small)
    }
    .padding(11)
    .background(Color.green.opacity(0.09), in: RoundedRectangle(cornerRadius: 10))
  }

  private var accountActions: some View {
    Button {
      store.startAddingAccount()
    } label: {
      Label("계정 추가", systemImage: "person.crop.circle.badge.plus")
        .frame(maxWidth: .infinity)
    }
    .buttonStyle(.borderedProminent)
    .controlSize(.regular)
    .disabled(store.isAuthenticating || store.pendingAccountConfirmation != nil)
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

  @State private var isRenaming = false
  @State private var draftName = ""
  @State private var showDeleteConfirmation = false

  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
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
    .padding(12)
    .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 11))
    .overlay {
      RoundedRectangle(cornerRadius: 11)
        .stroke(.quaternary, lineWidth: 1)
    }
    .alert("계정을 삭제할까요?", isPresented: $showDeleteConfirmation) {
      Button("삭제", role: .destructive) {
        store.removeManagedAccount(accountID: viewState.id)
      }
      Button("취소", role: .cancel) {}
    } message: {
      Text("\(viewState.account.title)의 로그인 정보와 전용 Codex 저장소가 이 Mac에서 삭제됩니다.")
    }
  }

  @ViewBuilder
  private var accountHeader: some View {
    HStack(spacing: 7) {
      if isRenaming {
        TextField("계정 이름", text: $draftName)
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
        Button("이름 변경", systemImage: "pencil") {
          draftName = viewState.account.displayName ?? viewState.account.title
          isRenaming = true
        }
        Button("다시 로그인", systemImage: "person.badge.key") {
          store.relogin(accountID: viewState.id)
        }
        Divider()
        Button("계정 삭제", systemImage: "trash", role: .destructive) {
          showDeleteConfirmation = true
        }
      }
    } label: {
      Image(systemName: "ellipsis.circle")
    }
    .menuStyle(.borderlessButton)
    .fixedSize()
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
    HStack(alignment: .top, spacing: 14) {
      UsageRing(progress: Double(snapshot.remainingPercent) / 100) {
        VStack(spacing: 0) {
          Text("\(snapshot.remainingPercent)%")
            .font(.system(size: 19, weight: .bold, design: .rounded))
          Text("남음")
            .font(.caption2)
            .foregroundStyle(.secondary)
        }
      }
      .frame(width: 78, height: 78)

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

        if let resetsAt = snapshot.resetsAt {
          let countdown = snapshot.resetCountdown() ?? "곧 초기화"
          Label(
            countdown == "곧 초기화"
              ? "곧 초기화 · \(resetsAt.formatted(date: .abbreviated, time: .shortened))"
              : "초기화 \(countdown) 후 · \(resetsAt.formatted(date: .abbreviated, time: .shortened))",
            systemImage: "clock.arrow.circlepath"
          )
          .font(.caption)
          .foregroundStyle(.secondary)
          .fixedSize(horizontal: false, vertical: true)
        }

        resetCreditsView(snapshot)
      }
      .labelStyle(.titleAndIcon)
      .frame(maxWidth: .infinity, alignment: .leading)
    }
    .accessibilityElement(children: .combine)
    .accessibilityLabel(
      "\(viewState.account.title), Codex 주간 한도 \(snapshot.remainingPercent)퍼센트 남음"
    )
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
      Label("계정 연결이 필요합니다", systemImage: "person.crop.circle.badge.exclamationmark")
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
}

private struct UsageRing<Content: View>: View {
  let progress: Double
  @ViewBuilder let content: Content

  init(progress: Double, @ViewBuilder content: () -> Content) {
    self.progress = min(max(progress, 0), 1)
    self.content = content()
  }

  var body: some View {
    ZStack {
      Circle()
        .stroke(.quaternary, lineWidth: 8)
      Circle()
        .trim(from: 0, to: progress)
        .stroke(
          progress <= 0.1 ? Color.red : (progress <= 0.3 ? Color.orange : Color.accentColor),
          style: StrokeStyle(lineWidth: 8, lineCap: .round)
        )
        .rotationEffect(.degrees(-90))
      content
    }
  }
}
