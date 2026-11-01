# 기존 CLI 재연결과 archive 실패 구분

2026-11-02 KST, 로컬 Mac만 적용했다. 원격 장비·Homebrew·release/tag 배포는 하지 않았다.

## 확인한 원인

사용자가 보고한 `starcraft1` (`00000000-7251-7f33-9c18-3458468f1614`)은 기존
`codexm-cli-954dfe6a5f9b007c` tmux의 공식 CLI가 소유하고 있었다. 계정 표시는
`default`, pane `%17`, 프로세스 PID 74258이었다. 같은 PID가 이 대화의 writer 파일을
열고 있었고 화면은 완료된 작업 뒤 다음 입력을 기다리는 상태였다.

기존 웹의 `terminal.open`은 같은 터미널을 재사용했지만 foreground `resume`은
항상 새 `codex --no-daemon archive ID`를 먼저 실행했다. 소유한 CLI가 살아 있으므로
새 프로세스의 archive가 거부된 것이다.

이 오류 메시지 자체는 일반적인 archive 실패이므로 모든 경우를 writer 문제로
단정하지 않는다. 별도로 조회한 두 대화는 GUI app-server PID 28543이 writer 파일을
열고 있었다. 두 대화는 보관/중지하지 않았다.

## 수정한 경로

foreground `resume`은 먼저 일치하는 기존 codexm 터미널을 확인한다. 동일 저장소,
대화 ID, 계정으로 계산한 세션 이름과 영수증을 검증하고 현재 pane이 살아 있는지,
프로세스가 Codex인지, `lsof`가 반환한 writer 파일 보유 PID와 pane PID가 같은지 확인한다.
영수증만 남았거나 CLI가 다른 대화로 이동한 경우 자동 연결하지 않는다.
프로세스에 여러 대화 writer가 남아 있으면 현재 화면을 단정하지 않고 명시적 연결을
안내한다. OS 파일 목록은 실제 writer 디렉터리로 제한해 다른 경로의 파일과 혼동하지 않는다.

같은 계정이면 보관/복원/새 resume을 생략하고 기존 터미널에 붙는다. tmux 안에서는
`switch-client`, 바깥에서는 `attach-session`을 사용한다. 다른 클라이언트나 CLI를
종료하지 않는다. 다른 계정을 선택하면 기존 계정을 조용히 사용하지 않고 안내한다.
`default`도 실행 중 인증을 유지한다. 기본 인증을 바꾼 뒤 예전 default 터미널로
재연결한 것을 새 인증의 새 실행으로 간주하지 않는다.

자동 소유 확인에 필요한 `lsof`가 없으면 `codexm terminal SESSION`을 안내한다.
현재 터미널에서 새 CLI를 시작하는 일반 경로의 필수 의존성은 늘리지 않았다.

GUI가 소유한 대화는 원래 GUI를 통한 보관/복원 경로가 필요하다. 새 CLI의 archive와
소유 GUI의 archive는 다르며, 이번 수정에 GUI 제어 연결을 구현했다고 주장하지 않는다.
공식 [API 목록](https://learn.chatgpt.com/docs/app-server#api-overview)에서 archive,
unarchive, unsubscribe의 역할을 확인했다. 보관은 하위 대화에도 영향을 줄 수 있고,
GUI-managed worktree는 보관 시 별도 정리가 일어날 수 있어 자동 보관 대상을 넓히지 않는다.

## 종료 처리 후속 수정

이전 PTY 검사에서 닫힌 터미널의 Rust frontend가 입력 읽기 안에서 남아 있던 경우를
후속 프로세스 검사에서 발견했다. tmux 세션이 없어졌다는 것만으로 종료 완료를 판정한
기존 검증은 충분하지 않았다. 이번에는 Crossterm poll/read를 하나의 별도 스레드로
옮겨 주 루프가 SIGTERM/SIGHUP을 계속 처리하도록 했다. 이전 검사에서 생성한 것으로
확인한 프로세스만 정리했으며 실제 GUI와 공식 CLI에는 종료 신호를 보내지 않았다.

PTY 테스트는 이제 SIGTERM과 tmux 세션 폐쇄 양쪽에서 frontend/bridge PID가 모두
사라지는지 확인한다. 실패 정리도 임시 config 경로를 가진 소유 프로세스로 제한한다.

## 검증 결과

- `go test -race ./...`, `go vet ./...` 통과.
- Rust workspace 테스트 12개, fmt, clippy `-D warnings` 통과.
- Linux x86_64 Rust compile check 통과. Linux 실장비 실행/설치는 하지 않았다.
- 실제 tmux PTY의 계정 관리·인계·터미널 모드 복구·SIGTERM·PTY 폐쇄 검사 통과.
- 새 단위/프로세스 검사에서 닫힌 세션·다른 writer·다른 계정·여러 대화 보유·tmux 내부/외부
  재연결을 구분했고 archive/resume 대역 호출이 발생하지 않았음을 확인했다.
- 실제 `starcraft1`에 새 실행기의 직접 resume과 설치된 TUI의 ID 검색 후 Enter로
  연결했다. 최종 설치본에서도 직접 resume을 재검증했다. 모두 기존 CLI 화면을
  보여줬고 검사 클라이언트만 분리했다.
  작업 지시·보관/복원·프로세스 종료는 보내지 않았고 원래 PID 74258이 유지됐다.

`./install.sh --build`로 설치한 버전:

```text
v0.6.2-source-fdfee681501c4fd079c5725e48b549d7d7e01a6525b955bedcb7965c7243e94e
```

설치 직전/직후 인증·공통 설정·제품 설정 12개 파일의 내용과 symlink 대상이 같았다.
GUI와 공식 CLI 두 개의 PID도 동일했다. 검사 후 남은 Rust frontend는 0개였다.
이전 TUI 작업 종료 시점과 이번 설치 전 사이에 제품 config가 달라져 있었으므로 이번
설치 직전 값을 기준으로 비교하고 기존 변경을 보존했다. 인증 파일 내용은 출력하지 않았다.

private 증거는 `~/.local/state/codexm/validation/native-reconnect-20261102/`의
`live-reconnect.json`, `installed-tui-reconnect.json`, `final-installed-reconnect.json`,
`before-install.json`, `after-install.json`에 있다. 사용자 대화 출력·설정·인증 자료는
Git에 넣지 않는다.
