import AppKit
import SwiftUI

struct MenuContentView: View {
  @ObservedObject var store: UsageStore
  @ObservedObject var preferences: AppPreferences

  var body: some View {
    VStack(alignment: .leading, spacing: 14) {
      header

      switch store.state {
      case .loading:
        loadingView
      case .loaded(let snapshot):
        usageView(snapshot)
      case .missingRuntime:
        missingRuntimeView
      case .failed(let error):
        errorView(error)
      }

      Divider()
      launchPreferences
      footer
    }
    .padding(16)
    .frame(width: 330)
  }

  private var header: some View {
    HStack(spacing: 10) {
      Image(systemName: "chart.bar.fill")
        .font(.system(size: 17, weight: .semibold))
        .foregroundStyle(.tint)

      VStack(alignment: .leading, spacing: 1) {
        Text("Codex Usage")
          .font(.headline)
        Text("주간 사용량")
          .font(.caption)
          .foregroundStyle(.secondary)
      }

      Spacer()

      OptionsMenu(preferences: preferences)

      Button {
        store.refresh()
      } label: {
        if store.isRefreshing {
          ProgressView()
            .controlSize(.small)
            .frame(width: 16, height: 16)
        } else {
          Image(systemName: "arrow.clockwise")
        }
      }
      .buttonStyle(.plain)
      .help("새로고침")
      .disabled(store.isRefreshing)
    }
  }

  private var loadingView: some View {
    HStack(spacing: 10) {
      ProgressView()
        .controlSize(.small)
      Text("Codex에서 사용량을 확인하는 중…")
        .font(.callout)
        .foregroundStyle(.secondary)
    }
    .frame(maxWidth: .infinity, minHeight: 92, alignment: .center)
  }

  private func usageView(_ snapshot: UsageSnapshot) -> some View {
    HStack(spacing: 18) {
      UsageRing(progress: Double(snapshot.remainingPercent) / 100) {
        VStack(spacing: 0) {
          Text("\(snapshot.remainingPercent)%")
            .font(.system(size: 22, weight: .bold, design: .rounded))
          Text("남음")
            .font(.caption2)
            .foregroundStyle(.secondary)
        }
      }
      .frame(width: 94, height: 94)

      VStack(alignment: .leading, spacing: 7) {
        if let plan = snapshot.planDisplayName {
          Label("ChatGPT \(plan)", systemImage: "person.crop.circle")
            .font(.callout.weight(.medium))
        }

        if let resetsAt = snapshot.resetsAt {
          Label {
            VStack(alignment: .leading, spacing: 1) {
              Text("초기화까지")
                .font(.caption)
                .foregroundStyle(.secondary)
              Text(snapshot.resetCountdown() ?? "곧 초기화")
                .font(.callout.weight(.semibold))
              Text(resetsAt.formatted(date: .abbreviated, time: .shortened))
                .font(.caption2)
                .foregroundStyle(.secondary)
            }
          } icon: {
            Image(systemName: "clock.arrow.circlepath")
          }
        }

        if let credits = snapshot.availableResetCredits, credits > 0 {
          Label("리셋 크레딧 \(credits)개", systemImage: "arrow.counterclockwise.circle")
            .font(.caption)
        }
      }
      .labelStyle(.titleAndIcon)
    }
    .frame(maxWidth: .infinity, alignment: .leading)
    .accessibilityElement(children: .combine)
    .accessibilityLabel(
      "Codex 주간 한도 \(snapshot.remainingPercent)퍼센트 남음, \(snapshot.usedPercent)퍼센트 사용")
  }

  private var missingRuntimeView: some View {
    VStack(alignment: .leading, spacing: 10) {
      Label("Codex 런타임이 없습니다", systemImage: "exclamationmark.triangle.fill")
        .font(.callout.weight(.semibold))
        .foregroundStyle(.orange)

      Text("앱 번들이 손상되었을 수 있습니다. App Store에서 Codex Usage를 다시 설치하세요.")
        .font(.caption)
        .foregroundStyle(.secondary)
        .fixedSize(horizontal: false, vertical: true)

      Button("다시 확인") { store.refresh() }
        .controlSize(.small)
    }
  }

  private func errorView(_ error: CodexUsageError) -> some View {
    VStack(alignment: .leading, spacing: 10) {
      Label(
        error.errorDescription ?? "사용량을 가져오지 못했습니다.", systemImage: "exclamationmark.circle.fill"
      )
      .font(.callout.weight(.semibold))
      .foregroundStyle(.orange)
      .fixedSize(horizontal: false, vertical: true)

      switch error {
      case .notAuthenticated:
        authenticationView
      case .unsupportedRuntime:
        Text("App Store에서 Codex Usage를 최신 버전으로 업데이트한 뒤 다시 시도하세요.")
          .font(.caption)
          .foregroundStyle(.secondary)
        Button("다시 시도") { store.refresh() }
          .controlSize(.small)
      default:
        Button("다시 시도") { store.refresh() }
          .controlSize(.small)
      }
    }
    .frame(maxWidth: .infinity, minHeight: 86, alignment: .leading)
  }

  @ViewBuilder
  private var authenticationView: some View {
    Text("Codex CLI에 이미 로그인했다면 계정 로그인 없이 기존 .codex 폴더를 한 번만 연결할 수 있습니다.")
      .font(.caption)
      .foregroundStyle(.secondary)
      .fixedSize(horizontal: false, vertical: true)

    if let info = store.deviceLoginInfo {
      VStack(alignment: .leading, spacing: 7) {
        Text("브라우저에서 아래 코드를 입력하세요")
          .font(.caption)
          .foregroundStyle(.secondary)
        Text(info.userCode)
          .font(.system(.title3, design: .monospaced, weight: .bold))
          .textSelection(.enabled)
        HStack {
          Button("코드 복사") { store.copyDeviceLoginCode() }
          Button("인증 페이지 열기") { store.reopenDeviceLoginPage() }
        }
        .controlSize(.small)
      }
    } else if store.isAuthenticating {
      HStack(spacing: 8) {
        ProgressView().controlSize(.small)
        Text("로그인 준비 중…").font(.caption)
      }
    } else {
      HStack {
        Button("기존 Codex 로그인 연결") { store.connectExistingCodexLogin() }
        Button("새로 로그인") { store.startDeviceLogin() }
      }
      .controlSize(.small)
    }

    if let authenticationError = store.authenticationError {
      Text(authenticationError)
        .font(.caption2)
        .foregroundStyle(.red)
        .fixedSize(horizontal: false, vertical: true)
    }
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
      if case .loaded(let snapshot) = store.state {
        Text("업데이트 \(snapshot.fetchedAt.formatted(date: .omitted, time: .shortened))")
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
        .stroke(.quaternary, lineWidth: 9)
      Circle()
        .trim(from: 0, to: progress)
        .stroke(
          progress <= 0.1 ? Color.red : (progress <= 0.3 ? Color.orange : Color.accentColor),
          style: StrokeStyle(lineWidth: 9, lineCap: .round)
        )
        .rotationEffect(.degrees(-90))
      content
    }
  }
}
