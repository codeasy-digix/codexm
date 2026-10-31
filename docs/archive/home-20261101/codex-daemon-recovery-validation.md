> Historical record imported from `.home` at `896d075` on 2026-11-01.
> Current ownership and commands: [deployment](../../deployment.md), [history relay](../../history-sync.md).

# 공용 Codex 데몬 복구와 Discord 알림 로컬 검증

2026-10-12, mac-main에만 적용했다. 계정 확장 소스 커밋은
`548bc20` (`codeasy-digix/codex-accounts`), 설치 버전은 `0.3.0-local.8`이다.
공식 패키지/실행 서버는 `codex-cli 0.162.1`, 기존 `codexm`은 `0.1.2`다.
다른 장비와 공개 Homebrew 릴리스는 변경하지 않았다.

## 실행한 검사

- 계정 확장 `go test -race -count=1 ./...`, `go vet ./...` 통과.
- 이후 추가한 기존 셸 호환·데몬 옵션/도움말 회귀 검사도 race로 통과.
- `.home`의 `test_codex_shell.py` 11개, `test_agent_notify.py` 4개 통과.
- `.home/tools/native`의 `go test ./internal/manage`, 양쪽 `git diff --check` 통과.
- 알림 설정·실행 링크와 zsh/curl/jq 의존성에 대한 `homectl doctor` 검사 통과.

실제 공식 패키지를 임시 홈과 별도 tmux 소켓에서 실행했다. 해당 PID만 SIGKILL한
후 감시가 새 PID로 복구했고, 남은 PID 기록이 계정 확장의 버전 실행을 막지 않았다.
닉네임 환경에서 정식 `daemon stop` 후 6초 동안 중지가 유지됐고, `daemon start`는
같은 공용 시험 홈에서 서버와 자동 복구 설정을 다시 활성화했다. 기존 인증 파일과
실제 장비의 데몬 PID 기록이 이 시험 동안 바뀌지 않았음을 확인했다. 시험 서버와
tmux 세션은 모두 정리했다. 실제 장비 전체의 `killall codex`는 실행하지 않았다.

## 적용 후 확인

로컬 제어 실행 파일을 새 버전 경로에 설치하고 SHA-256을 확인했다. 이전
`0.3.0-local.7` 파일은 보존했다. 공용 데몬이 없는 상태에서 `codex --version`이
정상 실행됐으며, `codex-daemon-watchdog` 감시를 켠 뒤 실제 서버 버전 응답이
`running / 0.162.1`임을 확인했다. 도움말에는 사용자 명령 `codex account`만 나오고,
이미 로드된 예전 셸의 `--shell` 경로도 상태 문구를 shell 코드로 출력하지 않는다.

`codexm`은 connected 상태와 저장된 관리자 계정 account-backup을 유지한다. 기존 작업용
tmux 세션은 종료하지 않았다. 감시 자체는 모델을 호출하지 않으며, 공용 데몬의
정식 stop은 복구를 일시 중지한다. 감시 tmux를 종료하거나 재부팅한 뒤에는
`codex account --daemon-watchdog start`를 다시 실행한다.

Discord는 `--check`로 기존 웹훅 설정만 확인했다. 알림 함수 원본은 전용 관리 파일로
분리하고 CLI를 비대화형 셸에서 사용할 수 있도록 연결했다. 비밀 파일은 Git 제외
상태로 유지하고 권한을 0600으로 보정했다. JSON/한국어/줄바꿈 처리와 HTTP 실패
반환은 모의 전송으로 검사했으며, 검증 과정에서 추가 Discord 메시지는 보내지 않았다.

설치·검증 영수증과 결과는 Git 밖의
`~/.local/state/codex-accounts/validation/daemon-recovery-20261012/`에 보관한다.
