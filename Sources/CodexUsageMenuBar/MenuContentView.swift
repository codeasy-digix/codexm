import AppKit
import SwiftUI

struct MenuContentView: View {
  @ObservedObject var store: UsageStore

  var body: some View {
    VStack(alignment: .leading, spacing: 14) {
      header

      switch store.state {
      case .loading:
        loadingView
      case .loaded(let snapshot):
        usageView(snapshot)
      case .missingCLI:
        missingCLIView
      case .failed(let error):
        errorView(error)
      }

      Divider()
      preferences
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
      UsageRing(progress: Double(snapshot.usedPercent) / 100) {
        VStack(spacing: 0) {
          Text("\(snapshot.usedPercent)%")
            .font(.system(size: 22, weight: .bold, design: .rounded))
          Text("사용")
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

        Label("\(snapshot.remainingPercent)% 남음", systemImage: "battery.75percent")
          .font(.callout)

        if let resetsAt = snapshot.resetsAt {
          Label {
            VStack(alignment: .leading, spacing: 1) {
              Text("초기화")
                .font(.caption)
                .foregroundStyle(.secondary)
              Text(resetsAt.formatted(date: .abbreviated, time: .shortened))
                .font(.caption.weight(.medium))
            }
          } icon: {
            Image(systemName: "clock.arrow.circlepath")
          }
        }
      }
      .labelStyle(.titleAndIcon)
    }
    .frame(maxWidth: .infinity, alignment: .leading)
    .accessibilityElement(children: .combine)
    .accessibilityLabel(
      "Codex 주간 사용량 \(snapshot.usedPercent)퍼센트, \(snapshot.remainingPercent)퍼센트 남음")
  }

  private var missingCLIView: some View {
    VStack(alignment: .leading, spacing: 10) {
      Label("Codex CLI 설치가 필요합니다", systemImage: "exclamationmark.triangle.fill")
        .font(.callout.weight(.semibold))
        .foregroundStyle(.orange)

      Text("터미널에서 아래 공식 npm 명령으로 설치한 뒤 Codex Usage를 새로고침하세요.")
        .font(.caption)
        .foregroundStyle(.secondary)
        .fixedSize(horizontal: false, vertical: true)

      Text("npm install -g @openai/codex")
        .font(.system(.caption, design: .monospaced))
        .textSelection(.enabled)
        .padding(8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.quaternary, in: RoundedRectangle(cornerRadius: 7))

      HStack {
        Button("명령 복사") { store.copyInstallCommand() }
        Button("Terminal 열기") { store.openTerminal() }
        Spacer()
        Link("공식 안내", destination: URL(string: "https://developers.openai.com/codex/cli/")!)
      }
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
        Text("터미널에서 `codex login`을 실행한 뒤 새로고침하세요.")
          .font(.caption)
          .foregroundStyle(.secondary)
        commandButtons(copyTitle: "로그인 명령 복사", action: store.copyLoginCommand)
      case .unsupportedCLI:
        Text("Codex CLI를 최신 버전으로 업데이트한 뒤 다시 시도하세요.")
          .font(.caption)
          .foregroundStyle(.secondary)
        commandButtons(copyTitle: "업데이트 명령 복사", action: store.copyUpdateCommand)
      default:
        Button("다시 시도") { store.refresh() }
          .controlSize(.small)
      }
    }
    .frame(maxWidth: .infinity, minHeight: 86, alignment: .leading)
  }

  private func commandButtons(copyTitle: String, action: @escaping () -> Void) -> some View {
    HStack {
      Button(copyTitle, action: action)
      Button("Terminal 열기") { store.openTerminal() }
    }
    .controlSize(.small)
  }

  private var preferences: some View {
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
          progress >= 0.9 ? Color.red : (progress >= 0.7 ? Color.orange : Color.accentColor),
          style: StrokeStyle(lineWidth: 9, lineCap: .round)
        )
        .rotationEffect(.degrees(-90))
      content
    }
  }
}
