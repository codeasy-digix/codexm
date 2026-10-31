> Historical record imported from `.home` at `896d075` on 2026-11-01.
> Current ownership and commands: [deployment](../../deployment.md), [history relay](../../history-sync.md).

# codexm 0.2.1: remote-mac 적용 (2026-10-12)

로컬에서 검증한 독립 소스 `a225b84c40fe7e325a0d563394aed4c7528e1c91`을
Mac Studio `remote-mac`에 적용했다. `.home`은 배포 기록까지 두 Mac에서 같은
main 커밋을 사용한다. 제품 버전은 **0.2.1**이다.

## 데이터와 기존 환경

- 기존 Codex 대화 **545개**(보관 46개)를 원본 SQLite에서 직접 조회한다.
  전체 대화 복제나 일괄 작업 등록은 하지 않았다. 등록 관리 작업은 **0개**다.
- Codex DB는 `mode=ro`와 `query_only`로 읽는다. codexm 전용 계정·실행 로그·사건·
  재시도 큐와 재개용 스냅샷은 별도 `~/.local/state/codexm/state.sqlite`에 저장한다.
  같은 파일에 테이블 이름만 구분해 쓰면 writer lock과 파일 수명주기를 공유하므로,
  공식 Codex에 쓰기 영향을 주지 않기 위해 물리 파일을 분리한다.
- 공식 `codex` 진입점과 기본 로그인은 유지했다. 원격 공식 CLI는 **0.160.1**이고,
  codexm은 이미 설치된 공식 패키지 **0.162.1**을 검증·보관한 엔진으로 사용한다.
  원격 기본 auth 파일과 공식 CLI 파일 해시·링크가 적용 전후 동일했다.
- `account-backup` 관리자, `account-work` 기본 작업 계정, `account-work → account-secondary → account-main → account-backup`
  우선순위와 600초 복귀 확인 등 관리 정책이 로컬과 동일하다. 별도 로그인·계정
  교체를 하지 않았다. native `account/read(refreshToken=true)` 조회 중 account-backup의
  로그인 토큰은 정상 갱신됐으며 이를 계정 변경으로 간주하지 않는다.
- 원격 `.home/config/zsh/.zshrc`의 기존 미커밋 수정은 백업하고 3-way merge로
  보존했다. 원래 실행 중인 tmux 작업과 앱·DB 서비스는 종료하지 않았다.
- Discord 알림 코드와 전역 지시어를 관리 경로에 적용했다. 웹훅은 SSH stdin으로
  전달해 ignored `private/remote-mac/secrets.zsh`에 저장했으며 로그·Git에 출력하거나
  저장하지 않았다. `agent_job_done --check`만 실행하고 메시지는 보내지 않았다.
  원격 전역 지시어의 기존 conda 금지 규칙도 보존했다.

## 적용과 검증

원격 GitHub 직접 fetch가 실패해 소스 커밋과 SHA-256을 확인한 Git bundle을 SSH로
전달했다. GitHub 인증이나 SSH 연결 설정은 변경하지 않았다. `.home`은 fast-forward
후 최신 native 도구를 빌드하고 `homectl plan → apply → doctor`를 실행했다.
독립 `tools/codexm/install.sh --force --build`로 설치했으며 기존 버전은 보존했다.

- 두 Mac의 codexm 실행 파일 SHA-256이 동일하다:
  `b9403f27cd9aa7cf76aec375ab5cd9fa1e84b08355314ae242e3e1d416a6343c`.
  native homectl 실행 파일 해시도 동일하다.
- **원격에서** `go test -race ./...`, `go vet ./...`와 공식 0.162.1을 사용하는
  격리 native lifecycle/writer-lock/계정 전환/goal 보존 검사가 통과했다.
- 실제 원격 PTY에서 기본 전체 대화 목록과 `/` 명령 메뉴를 확인했다.
  대화 지시를 보내거나 기존 대화를 관리 대상으로 가져오지 않았다.
- `codexm doctor --json` 정상, `homectl doctor --profile remote-mac --json`
  **137개 검사 모두 정상**, 관리자 모델 호출 **0회**였다.
- 설치 중 생성한 상태 폴더의 0755 권한 때문에 첫 supervisor 시작이 보호 검사에서
  종료됐다. codexm 전용 상태·검증 폴더를 0700으로 수정하고 실제 기동을 확인했다.
- 격리한 공식 daemon PID만 강제 종료해 **15.72초** 안에 자동 복구됨을 확인했다.
  명시적 stop은 감시 간격 이후에도 중지 상태를 유지했고 수동 start도 정상이다.
  테스트 daemon·watchdog tmux는 모두 정리했다. 실제 공용 daemon은 강제 종료하지 않았다.

## 사용과 운영

```sh
codexm
codexm session list
codexm session list --managed
codexm daemon status
tmux attach -t codexm-daemon-watchdog
```

공용 daemon 복구 watcher는 `codexm-daemon-watchdog`에서 5초 간격으로 확인한다.
실제 daemon 시작에 시간이 필요하므로 복구 시간 자체가 항상 5초인 것은 아니다.
supervisor는 실행 중 `codexm-supervisor`에 있으며 `tmux attach -t codexm-supervisor`로
연결한다. 로컬과 동일하게 `exit_when_no_clients_and_no_pending_work=true`이므로,
열린 TUI와 관리 작업이 없으면 supervisor는 종료되고 다음 필요한 명령에서 다시 시작한다.

원격 백업·적용 영수증·검증 자료는 Git 밖의
`~/.local/state/codexm/validation/ibs-deploy-20261012/`에 보관한다.
원격의 실제 대화 제목·지시·goal 원문과 인증값은 검증 출력에 남기지 않았다.
다른 장비에는 적용하지 않았으며 release/tag/npm/tap도 게시하지 않았다.
