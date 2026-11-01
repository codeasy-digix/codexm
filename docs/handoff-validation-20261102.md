# 잠금 상태에 따른 재개와 소유 서버 인계

2026-11-02 KST, 로컬 Mac 검증 기록. 원격 설치와 Homebrew 배포는 하지 않는다.

## 확인한 문제와 수정

이전 실행기는 잠금이 없는 일반 대화에도 새 프로세스로 `archive → unarchive`를
실행했다. 새 프로세스는 GUI나 독립 CLI가 가진 writer를 해제할 수 없었다.

현재 실행 순서는 다음과 같다.

1. 검증된 같은 대화·계정의 기존 codexm tmux CLI가 있으면 재연결한다.
2. 잠금 없는 일반 대화는 바로 공식 CLI로 resume한다. 이미 보관된 대화만 복원한다.
3. writer가 있으면 공식 `app-server daemon version`으로 실행 중인 공용 데몬을
   조회한다. 동일 사용자 소유의 해당 CODEX_HOME 소켓인지 확인한다. 데몬을 시작하거나
   재시작하지 않고 인증도 변경하지 않는다.
4. 데몬의 `thread/loaded/list`에서 선택한 ID를 확인한 경우에만 `thread/archive`를
   요청한다. 실제 writer 해제를 기다린 뒤 공식 unarchive와 새 계정의 CLI resume을 수행한다.
5. 연결되지 않은 GUI/독립 CLI writer는 오류에 ID와 다음 조치를 표시한다.
   불확실하게 끝난 archive를 재전송하거나 잠금 파일·DB를 수정하지 않는다.

control socket은 WebSocket을 사용한다. `app-server proxy`에 JSON-lines 초기화 메시지를
보내는 것은 이 연결 방식이 아니다. 공식 socket symlink의 실제 경로를 사용하여 긴
CODEX_HOME 경로의 Unix socket 길이 제한도 피한다.

기존 RPC/프로세스 코드가 있는 Go 모듈 안에서 경로를 수정했다. Rust TUI, 공식 Codex
실행 파일, 모델·effort 설정, 동기화 기능은 변경하지 않았다.

## 검증 완료 범위

- `go test -race ./...`, `go vet ./...` 통과.
- 격리된 공식 CLI 0.166.0 + 로컬 모의 모델에서 활성 응답을 owner-side archive로
  중단하고 같은 ID를 복원·resume했다. 후속 모델 요청에 기존 입력과 새 입력이 함께
  포함됐고 목표 내용·상태·token budget이 유지됐다. 다른 대화의 writer도 유지됐다.
- Unix WebSocket fixture로 데몬 탐색부터 선택한 ID의 archive, 잠금 해제 대기,
  unarchive까지 검사했다. 다른 ID, 페이지 탐색, 커서 반복, 조회 실패, archive 후
  연결 단절을 구분하고 archive를 재전송하지 않음을 확인했다.
- 연결되지 않은 writer에서 DB·잠금 inode·실행 기록이 바뀌지 않음을 확인했다.
- Rust/Go 설치 쌍의 실제 tmux PTY에서 목록·계정 관리·CLI 인계·종료 검사가 통과했다.
- 기존 실제 공용 데몬에는 읽기 전용 `thread/loaded/list` 연결만 검사했다.
  다른 사용 중 대화를 검증용으로 보관하지 않았다.

## 사용자가 지정한 실제 대화

`00000000-d582-7301-9a5a-6293e75e2562`, 앱 표시 제목
「한글 2014 VP 이미지 표시 문제 조사」를 확인했다. GUI app-server가 writer를
가지고 있었지만 마지막 turn은 완료 상태였다. 사용자가 이 대화의 중단·인계를 승인했다.

앱 안의 공식 `set_thread_archived` 도구로 이 ID를 보관한 뒤 공식 CLI로 복원했다.
같은 ID, `/Users/ChangeMe/ReaProjects/2014vp-os` 경로, 보관 해제 상태가 유지됐다.
설치된 codexm으로 이전 등록 계정 `account-backup`을 선택하여 실제 공식 CLI의 이력과
입력 대기 화면을 확인했다. 작업 지시는 보내지 않았으며, 확인용 CLI는 Ctrl-D로
종료하고 확인용 tmux 세션도 제거했다. 모든 과거 turn의 UI 표시를 전수 검사한 것은 아니다.

이는 앱 안에서 수행한 일회성 owner 조작이다. 이 사실만으로 독립 stdio GUI의
자동 인계가 codexm에 구현됐다고 볼 수 없다.

설치 전후 인증·공통 설정·제품 설정 13개 파일의 내용과 symlink가 동일했다.
GUI와 기존 공식 서버/CLI 프로세스는 설치·CLI 확인 과정에서 유지됐다.

최초 검증 설치 버전:
`v0.6.2-source-ffcfcb0da685bc68c17426b9075a1fa3a9ed23dfda491b30b88a648c561c3911`.
이후 도움말을 실제 분기와 맞춘 최종 로컬 설치 버전은
`v0.6.2-source-b8e5f7a549fab54a1a4680f4b9c2464626075125123d8f3e84e9f326e1471f69`다.
private 증거는 `~/.local/state/codexm/validation/native-handoff-20261102/`에 보관한다.
대화 화면·인증·개인 설정 원문은 Git에 포함하지 않는다.

## GUI 전체 자동 인계의 남은 검증

현재 앱 26.1027.30824의 외부 app-tools 파이프는 별도 프로세스의 연결을
`missing-code-signing-identity`로 거부했다. UI 자동화도 허용되지 않았다.
이 검증을 우회하거나 전체 GUI 프로세스를 강제 종료하는 fallback은 제품에 넣지 않았다.

설치된 앱에는 `CODEX_APP_SERVER_USE_LOCAL_DAEMON=1`일 때 공식 공용 데몬으로
연결하는 분기가 있다. 버전 호환, 로컬 호스트, 사용자 지정 CLI 및 config override
조건도 만족해야 하므로 환경변수를 설정했다는 사실만으로 적용됐다고 판단하지 않는다.

사용자는 다른 GUI 작업의 중단 가능성을 안내받은 후, 로컬 GUI 재시작을 포함한
이 연결 모드의 검증을 승인했다. 현재 설치된 제품은 이 환경변수를 자동으로 설정하지
않는다.

### 실제 GUI 재시작 결과

승인된 검증에서 GUI main PID 28440을 SIGTERM으로 종료하고 앱을 다시 실행했다.
새 main PID는 86555였으며, 앱은 다시 별도 CLI 서버 86743/87255를 만들었다.
공용 데몬의 loaded 목록에 대상 대화는 없었다. 따라서 GUI 자동 인계 검증은
**성공하지 않았다**. 시작 환경변수는 이전의 미설정 상태로 복원했고, GUI는 실행 중이다.
검증 tmux는 정리됐으며 Discord에는 완료가 아닌 대기 상태를 알렸다.

최초 설치 직후의 13개 파일 동일 검사와 GUI 재시작 이후 검사는 구분한다.
공통 `config.toml` 수정 시각은 03:40:37 KST로 GUI 재시작 시점과 일치하며,
03:52의 도움말 수정 재설치보다 앞선다. 최초 기준과 비교하면 공통 config 및 이를
참조하는 계정 config의 내용이 달라졌고 인증 파일은 동일했다. 해당 비교만으로
config 작성 주체를 확정하지 않으며, 이후의 설정 변경을 이전 값으로 덮어쓰지 않았다.

설치된 앱 코드를 추가 확인한 결과, 로컬 연결의 `getConfigOverrides`는 항상
`plugins.codex-app-tools@openai-bundled.mcp_servers.codex_app.enabled=...` 항목을
포함한다. 공용 데몬 분기는 동시에 override 배열이 비어 있기를 요구한다.
따라서 현재 앱 빌드의 이 실행 경로는 환경변수만으로 전환할 수 없다. 앱 코드나
서명 검증을 변경하거나 필수 설정을 제거하지 않았다.

근거 파일은 설치된 app.asar의
`.vite/build/early-bootstrap-439-a0f25ba32c465de1.js` (`s6`, `getConfigOverrides`)와
`.vite/build/early-bootstrap-824-3c24dda6dba622ec.js` (`V.connect`)다.
실제 실행 증거는 private `gui-before.json`, `gui-after.json`, `gui-notify.json`에 있다.
GUI 전체 재시작을 제품의 fallback으로 사용할지는 다른 GUI 작업도 중단하는 동작이므로
사용자에게 별도로 동작 방식을 확인 중이다. 현재 제품에는 자동 GUI 종료가 없다.

공식 프로토콜 참고: [App Server API](https://learn.chatgpt.com/docs/app-server#api-overview).
