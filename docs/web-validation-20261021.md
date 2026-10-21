# 0.6.0 로컬 웹 검증 기록

2026-10-21, `remote-mac`의 macOS arm64에서 격리 worktree로 구현하고 검사했다.
기준 소스는 `e24492d`이며 Meteor 3.5.2, Node 24.15.0, Go 1.26.3을 사용했다.
아래 결과는 HTTP 200 확인과 실제 UI·공식 엔진 동작을 구분한다.

| 검사 | 실제 결과 |
| --- | --- |
| `go test -race ./...`, `go vet ./...` | 통과 |
| TypeScript typecheck, Node 단위 검사 3개 | 통과 |
| Go→웹 계약 목록 일치 | 생성된 계약과 Go allowlist 일치 |
| 관리자 설정 전 웹 | 실제 배포 번들의 인증·계정·설정 접근, 로그인 폼 표시 통과 |
| 접근 보호 | 미인증 DDP method, 다른 HTTP Origin·WebSocket Origin 거절; 코드·ticket 재사용/만료·owner 분리 검사 통과 |
| 브라우저 입력 | 기본 화면, 한글 IME Enter, Shift+Enter, 초안 새로고침, 직접 `/accounts`, 390px 화면 검사 통과 |
| 두 탭의 관리 작업 | 같은 ID의 계정 변경 결과 재사용, stale settings revision 거절, 기존 대화 등록/해제, 그룹 생성 통과 |
| 동일 전송·미확정 결과 | 동시 전송 12개 요청이 action 1개, dispatch 후 결과 저장 실패·브리지 종료 시 재전송 차단 |
| journal | 100건을 넘는 replay/paging, DB 교체·누락 resync, WAL 유효 백업, 이전 writer의 변경 기록 통과 |
| 목표와 실행 소유권 | paused 목표의 동일 thread·누적 사용량 보존, 브라우저에서 30,000→31,000→30,000 예산 편집 후 paused·모델 턴 0개 유지, 오래된 Run ID 거절, 기존 대화의 목표 누락 시 자동 초기화 거절 |
| 서비스와 웹 수명 | 실제 launchd 재시작 후 owned worker 재연결, 브리지 SIGKILL 복구, 웹 stop/start 후 같은 thread·Run ID 유지 |
| 공식 플러그인 | native 설치·MCP/settings/skill 발견 및 기존 관리 명령 훅 회귀 통과 |
| 모델 호출 분리 | native fixture의 관리자 호출 0회; 명령 훅 9종·등록 4종·열기 실패의 inference 0회. 일반 입력은 격리 sentinel에만 2회 |
| 설치 실패/복구 | 파일 충돌 보존, 실패한 child, public 링크 교체 실패 시 rollback, 번들 변조·외부 symlink 거절과 실제 Meteor 실행 확인 |
| 실제 설치본 | codexm 0.6.0 / supervisor API 2·web API 1, user/502 launchd 실행, Node는 127.0.0.1:4317에만 LISTEN |
| 환경 | `homectl plan`: 72개 모두 ok, 불필요한 apply 없음. `homectl doctor`: 139/139 정상 |

`scripts/verify-services-local.sh`는 임시 계정·상태·서비스를 사용하며 공식 CLI 0.163.0에서
통과했다. 기존 named 계정이나 실작업의 소진·강제 종료로 검증하지 않았다. 기존 실행에
고정되어 있던 엔진은 0.162.1이며 동일한 공식 엔진 검사도 별도로 통과했다
(`native-pinned.log`). 이 pin을 유지하고 제품 doctor에서 digest/protocol을 확인했다.
최종 목표 편집 검사에서 parked 작업의 변경이 대기 상태에 남던 문제를 발견해 수정했다.
명시적 목표 수정은 paused 상태에서도 처리하되 native 상태를 읽고 보존하며, 변경을
요청한 경우에만 상태를 바꾼다. 수정 후 전체 Go race/vet와 공식 0.163.0 브라우저 fixture,
고정 엔진 0.162.1의 worker·목표·writer 회귀를 다시 통과했다.

예열된 대화 1,000개 fixture의 첫 50개 페이지는 비-race 실행에서 9.3~13.1ms였다.
두 탭을 연 실제 Chromium의 native fixture에서는 목록 14~19ms, 최근 기록 19~22ms,
그룹 생성 후 다른 탭의 목록 반영 621~1,233ms를 관측했다. 최종 목표 편집 검사는
Go 회귀와 동시에 실행했으며, 모든 부하에서 1초 이내 사건 반영을 보장하지 않는다.
이 수치는 초기 빌드·로그인·모델 응답을
제외한 로컬 측정이다. 공유 watcher를 매 snapshot마다 다시 시작하지 않도록 변경한 뒤
기존 1,071ms 반영 지연을 줄였다. CI의 병렬 race/build 부하에서는 시간 제한을 강제하지
않고 `CODEXM_PERF_TEST=1`로 따로 측정한다.

설치 전 SQLite backup API와 integrity_check로 관리 DB를 백업했다. 기존 작업 0개를
유지했으며 실제 관리 DB의 관리자 모델 호출도 0개였다. 보호한 파일 8개 중 codexm
실행 파일만 바뀌었다. 공식 CLI·named 인증·동시 수정 중인 `.zshrc`는 같은 해시였다.
config에서는 폐기된 worker `default_account`만 원문 백업 후 제거했고, 나머지 파싱된
설정은 같았다. 기존 공용 daemon/watchdog와 인증 파일 경로는 유지했다.

private 원본 증거는 이 머신의 `~/.local/state/codexm-web-validation/20261021/`에 있다.
`go-race.log`, `go-vet.log`, `native.log`, `build-web.log`, `browser-installed-final.log`,
`connected-final/`, `goal-ui-fixed/`, `native-goal-edit-pinned.log`, `go-race-final.log`,
`performance.log`, `production-backup/`, `production-final.json`과
각 exit 파일을 보관했다. 런타임 DB·접속 코드·인증·스크린샷은 Git에 넣지 않는다.
개발·fixture·빌드 tmux는 종료하고 실제 사용을 위한 `codexm-web`만 유지한다.

목표 fork, 시간 예산 편집, 첨부와 복합 elicitation의 브라우저 입력은 capability 경계로 남긴다.
실제 OAuth 인증 완료와 유료 모델 작업은 사용자 계정을 소비하지 않기 위해 수행하지 않았다.
기존 로그인 broker의 격리 회귀와 owner 분리 검사는 통과했다. 개발 Rspack 의존성에는
npm audit 경고가 남아 있으며, 앱 production 의존성과 고정한 서버 배포 의존성의
`--omit=dev` audit는 0건이었다. 공개 release/tag/npm 게시와 주 Mac 웹 설치는 수행하지 않는다.
