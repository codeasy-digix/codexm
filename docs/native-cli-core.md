# 공식 CLI를 중심으로 줄인 codexm

2026-11-01. 현재 기본 UI는 `crates/terminal/`의 Rust TUI다. Go 진입점은
`internal/cli/compact.go`와 `internal/nativecli/`다. [현재 TUI 동작](terminal-ui.md)을 따른다.
이전 관리형 구현은 전환 기록·테스트를 위해 소스에 남지만 새 명령에서 호출하지 않는다.
옛 CLI 진입 코드는 `legacy` build tag로 기본 빌드에서 제외한다. 현재 바이너리에는
supervisor·manager·policy·daemon·tui 패키지의 실행 심벌이 없다.
홈 환경 저장소나 원격 서버로 제품을 배포하는 변경은 포함하지 않는다.

## 실행 책임

1. Codex SQLite를 읽기 전용으로 조회하여 전체 로컬 프로젝트의 채팅 목록을 보여준다.
2. 과거 관리 DB와 작은 실행 영수증에서 알 수 있는 이전 계정만 표시한다.
3. TUI Enter는 이전 등록 계정 또는 default로 재개한다. Right는 이번 계정 선택,
   F2는 전체 계정 관리다. 계정 홈·공통 설정·대화 저장소 연결을 검증한다.
4. 같은 계정의 기존 codexm CLI가 실제로 이 채팅을 소유하면 tmux 재연결로 끝낸다.
   저장된 영수증·pane·프로세스·열린 writer 파일 PID를 함께 확인하며 다른 계정은 거부한다.
   그렇지 않으면 공식 archive/unarchive를 실행한다. 보관된 채팅에는 unarchive만 한다.
5. 현재 터미널에서는 `exec`로 `codex --no-daemon --no-alt-screen resume ID`에 교체된다.
   모델·effort·권한·목표를 codexm이 다시 지정하지 않는다.
6. 선택적 이전 웹에서는 같은 CLI를 이름 있는 tmux에서 실행하고 출력과 입력만 전달한다.

설치된 공식 CLI를 실행 시점에 찾으며 과거 관리 엔진 pin을 사용하지 않는다.
`codexm account`의 터미널 계정 선택·브라우저/기기 인증·기본 인증 변경은 유지한다.
등록 해제는 인증·대화·과거 작업을 보존하며 현재 실행을 자동 재배정하지 않는다.

## 제외한 기능과 데이터

관리자 모델·supervisor·worker·watchdog·스케줄링·자동 계정 교체·목표/예산 대리 관리·
자동 알림·관리 플러그인·장비 간 history-sync는 새 실행 경로에서 사용하지 않는다.
알 수 없는 예전 명령은 안내 후 실패하며 관리자나 모델을 자동으로 시작하지 않는다.
이전 prompt hook의 호출은 빈 허용 응답으로 종료해 공식 프롬프트를 방해하지 않는다.

현재 설정은 runtime의 shared_codex_home/accounts_dir/codex_binary/web_node_binary,
storage.state_dir, account_registry.removed뿐이다. 과거 설정 섹션은 실행 정책으로
읽지 않는다. 기존 공식 인증·대화·목표/예산은 삭제·변환하지 않으며 기존 관리 DB도
목록의 이전 계정 정보용으로만 읽는다. 원격 장비의 codexm 설치는 변경하지 않는다.

## 소유권과 오류

`--no-daemon unarchive`는 모든 잠금을 강제로 해제하는 API가 아니다. 설치된
Codex CLI 0.166.0을 임시 HOME/대화와 로컬 mock model로 검증한 결과:

- 보관되지 않은 채팅에 unarchive만 실행하면 실패했다.
- 원래 app-server가 여전히 대화를 소유한 경우 별도 CLI의 archive도 거부됐다.
- 원래 연결 종료 후 archive/unarchive 및 새 app-server의 즉시 resume은 성공했고,
  같은 thread ID·대화 하나·보관 해제 상태를 유지했다.

따라서 외부 writer를 종료하거나 잠금 파일을 지우는 우회 동작은 하지 않는다.
원래 GUI에서 보관하거나 원래 CLI를 종료한 다음 재개한다. 공식 명령 실패 시 새
resume은 실행하지 않는다. unarchive 단계가 실패하면 채팅이 보관 상태로 남을 수
있으며, 같은 ID로 보관 해제를 재시도한다. 성공한 것처럼 숨기거나 새 ID를 만들지 않는다.
같은 채팅에 여러 화면에서 동시에 지시하는 방식은 지원 범위가 아니다.

`starcraft1`의 2026-11-02 오류는 이미 실행된 codexm CLI에 새 CLI가 archive를
시도한 경우였다. 새 foreground 경로는 같은 터미널에 다시 연결한다. 이는 GUI에
보관 요청을 보내는 기능이 아니며 GUI의 writer 인계는 여전히 원래 앱에서 수행한다.
[수정 검증](reconnect-validation-20261102.md)을 참고한다.

codexm의 보관/복원 절차끼리는 별도 로컬 잠금으로 직렬화한다. 이 잠금은 codexm의
state 아래에만 두며 공식 writer lock과 무관하다. 브라우저 mutation에는 작업 ID를
붙여 중복 요청을 실행하지 않는다. 처리 중 연결이 끊기면 입력을 재전송하지 않는다.
실행 영수증·작업 ID 기록에는 프롬프트 본문이나 인증 토큰을 저장하지 않는다.

## 로컬 웹

[웹 안내](local-web.md)를 따른다. 원래의 HTTP/Origin/DDP 인증을 유지하고 Go bridge는
명시적인 메서드만 허용한다. 계정 사용량 확인은 명시적인 조회이며 모델 요청은 하지 않는다.
웹 본문은 tmux가 보여주는 공식 CLI 출력이다. 마지막 1,500줄을 표시하고 기본 키와
문자열 입력을 전달한다. 전체 터미널 기능은 표시한 attach 명령으로 사용한다.
웹/bridge 종료와 CLI 종료를 분리하며, 네이티브 CLI 종료만 사용자가 명시적으로 한다.

## 검증 범위

Go 전체 race/vet, 공식 CLI 격리 테스트, 계정 설정 보존 회귀, 웹 typecheck/단위 테스트,
실제 Chrome의 합성 채팅 선택→계정 선택→CLI 실행→한글/문자 그대로 전송→새로고침
재연결→보관 목록→종료 흐름을 검증한다. 실제 사용자 채팅에 archive/resume이나 모델
요청을 보내는 방식으로 테스트하지 않는다. Linux 실장비 검증이나 공개 패키징은 하지 않는다.

실행 증거·백업은 `~/.local/state/codexm/validation/native-cli-20261101/`와
`~/.local/state/codexm/migrations/20261101-native-cli/`에 보존하고 Git에 넣지 않는다.

2026-11-01 로컬 검증 결과:

| 검사 | 결과 |
|---|---|
| `go test -race ./...`, `go vet ./...` | 통과 |
| `go test -tags legacy ./internal/cli` | 과거 구현 호환 검사 통과 |
| 설치된 공식 CLI 0.166.0을 사용하는 격리 테스트 4개 | 인증 저장·공용 DB·설정 보존·보관/복원 후 재개 통과 |
| Node 24 웹 typecheck·단위 테스트 3개·production 빌드 | 통과 |
| production 웹의 Chrome 상호작용 검사 | 채팅/계정 선택·한글 IME·리터럴 입력·재연결·보관 목록·종료 후 재실행 통과 |
| 실제 tmux의 번호 선택 및 같은 operation ID 재전송 | CLI로 인계, 입력 한 번만 전달 |
| 닫힌 pane 또는 다른 세션을 가리키는 오래된 영수증 | 종료된 CLI는 재실행 가능, 소유권이 다른 세션은 변경 거부 |

테스트용 tmux 세션은 검증 후 종료한다. 실제 사용자 채팅의 archive/resume을 실행하지
않았다. 웹 화면은 터미널의 텍스트 뷰이며 모든 터미널 단축키를 구현한 에뮬레이터는 아니다.

## 로컬 적용 기록

구현 커밋 `c0ded48`을 main에 반영하고 제품의 `install.sh --build --web ...`로 설치했다.
설치된 바이너리는 같은 소스의 재빌드와 바이트 단위로 일치했다. 표기 버전은 0.6.2를
유지하며 공개 배포 버전으로 발행하지 않았다.

- 로컬 supervisor/watchdog에 실행 중인 worker가 없음을 확인한 뒤 두 user LaunchAgent를
  해제·비활성화하고 정의 파일은 백업으로 옮겼다. 공식 Codex·GUI는 종료하지 않았다.
- 공식 `codex --no-daemon plugin remove codexm@local-personal --json`으로 옛 관리 플러그인
  캐시를 제거하고, 이전 제품의 uninstall로 개인 marketplace 등록만 제거했다.
- codexm 설정은 필요한 runtime/storage 값만 남겼다. 이전 설정·설치 경로·서비스·플러그인
  등록은 private migration 폴더에 보존했다. 기존 관리 DB는 그대로 둔다.
- 기본/이름별 인증 파일의 대상·내용과 공식 설정의 플러그인 외 항목이 유지됐음을
  확인했다. 실행 중 공식 프로그램이 갱신하는 모델 캐시는 과거 파일로 덮어쓰지 않았다.
- 설치된 `doctor/status/list`, 보관 목록, 구형 명령 거부와 prompt hook no-op을 확인했다.
  확인 당시 로컬 채팅은 638개(보관 90개)였으며 조회 전후 관리 DB 내용은 동일했다.
- `codex`는 공식 실행 파일, `codexm`은 제품 셸 함수로 새 zsh에서도 구분됐다.
  기존 `.home`의 SSH·에디터 미커밋 변경은 보존했다. 테스트용 tmux는 모두 종료했다.

설치 바이너리 SHA-256: `6bac903f5ef2ad3ad9d75c235d9c1e890c075e7c5417a1a38034c80a08023dbe`.
웹 번들 digest: `8cadcccf9ff906d1c75dd992363f98f58fdeb53709d1251d538d22b83d529a84`.
다른 장비의 codexm 설치·Homebrew·release/tag/npm 배포는 하지 않았다.
