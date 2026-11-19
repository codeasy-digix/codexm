# 웹 채팅 핵심 기능 구현

목표는 같은 머신의 Codex 채팅·프로젝트를 Meteor + React 웹에서 관리하고 이어가는
작은 GUI다. 기존 CLI/TUI와 설치는 보존하며, 웹은 공식 app-server 프로토콜을 사용한다.
검증 대상은 macOS arm64, 공식 Codex runtime 0.169.0, Meteor 3.5.2와 Node 24다.

## 완료 기준

- Recents / Projects / Pinned와 검색, 보관 목록을 제공한다.
- 기존 채팅의 마지막 구간만 먼저 읽고, 위로 스크롤하면 이전 구간을 이어 읽는다.
  긴 단일 턴의 도구 출력도 처음부터 전부 가져오지 않는다.
- 사용자/최종 응답을 우선 표시하고 도구·명령·진행 세부 내용은 접힌 아코디언에 둔다.
- 새 채팅/프로젝트 선택, 프로젝트 이동, 고정/해제, 보관/복원, 확인 후 영구 삭제를 제공한다.
- Enter는 진행 중인 턴에 steer, 쉬고 있으면 새 턴을 시작한다. ⌘Enter는 공식
  입력 큐를 사용하고 Shift+Enter는 줄바꿈이다. IME와 모바일 입력도 지원한다.
- 브라우저 종료가 실행 중 작업을 멈추지 않으며, 진행 상태·실패·큐 접수를 구분한다.
- 인증/loopback/Origin/입력 검증과 불확실한 쓰기의 자동 재시도 금지를 유지한다.
- 합성 데이터의 실제 공식 runtime 시험, 웹 단위/타입 검사, 데스크톱/모바일 브라우저
  시험, Go race/vet를 거쳐 로컬에서 실행 가능한 번들을 만든다.

## 현재 확인

- 기존 웹은 terminal 화면 복사 방식이다. 이번 웹은 이를 구조화된 채팅 화면으로 교체한다.
- 설치된 공식 runtime 0.169.0에서 turn/item 페이지, thread delete, sections, projects,
  queue와 steer의 JSON schema를 생성했다. projects/queue의 일부는 experimental API다.
- 프로젝트/고정/보관/삭제 변경은 공식 API로 처리한다. 직전 웹 계정만 private 로컬
  표시 설정이다. 목록 조회는 native DB와 기존 GUI 연결 정보를 읽기 전용으로 사용한다.
- 실행 중 GUI/CLI의 소유권은 보존한다. 접근 가능한 공식 소유자 연결을 검증하며,
  임의 GUI 종료나 잠금 제거를 제어 수단으로 쓰지 않는다.

## 요구사항별 확인

| 요구 | 결과와 증거 |
|---|---|
| 머신의 Recents/Projects/Pinned/보관함 | native 읽기 전용 목록, GUI 프로젝트 연결과 고정 섹션. 전체 실제 이력·보관·프로젝트·고정 ID를 대조하고 본문도 공식 API로 확인 |
| 새 채팅·프로젝트·위치 선택·이동 | 공식 create/metadata API. 새 프로젝트 → 해당 프로젝트로 새 채팅 → 검색 브라우저 검사 통과. 이동은 소속 변경이며 폴더/파일 이동이 아님 |
| 고정·보관·복원·삭제 | 확인 창 취소/실행과 대상 ID 유지, 최종 목록 상태까지 브라우저 검사 통과. 보관 상태 그대로 내용도 읽음 |
| 긴 이력 지연 읽기 | 26턴 중 8턴부터 시작, 16/24/26으로 증가. 이전 구간 추가 후 스크롤 위치 유지 확인 |
| 접힌 도구 출력 | 최근 요약에는 명령 출력 없음. 과정 → 개별 명령을 펼칠 때만 5만 자 출력을 읽는 실제 runtime/브라우저 검사 통과 |
| Enter / ⌘Enter / Shift+Enter / IME | 새 턴·진행 중 expected-turn steer·공식 queue·줄바꿈과 한글 조합 보호 |
| 브라우저 종료 후 작업 유지 | 모델 응답을 보류한 채 탭을 닫고 재접속, 동일 ID 작업/큐 유지 후 완료 확인 |
| 모바일 | 375×667, 390×844에서 탐색/대화/입력, 가로 넘침·문서 높이·스크롤 및 화면 캡처 확인 |
| 인증/안전 | 로그인 전/로그아웃 후 호출 차단, HTTPS/Origin/쿠키/티켓·재전송 금지 단위 검사 통과 |
| 기존 작업 보존 | 공식 소켓의 상대 PID·실제 writer·현재 턴을 확인한 뒤 외부 현재 턴에 개입. stdio 소유자는 공식 큐로 전달. 기본 로그인 교체 시 기존 턴과 큐가 끝난 뒤 해당 웹 연결만 갱신 |
| 프로세스 수명 | 격리 launchd 운영 번들로 시작/종료/등록 제거. 소유 Node·bridge·app-server 3개가 모두 종료됨을 확인 |

## 실행한 검사

- `go test -race ./...`, `go vet ./...`: 통과. 이후 변경한 nativecli/cli/catalog/
  service/webhost는 race/vet와 격리 공식 runtime 검사를 다시 통과했다.
- 웹 TypeScript 검사와 단위 검사 10건: 통과.
- 운영 Meteor 번들의 Playwright 브라우저 검사 **7건**: 통과. 실제 계정/대화나 유료
  모델 호출을 시험에 사용하지 않고, 격리 HOME·공식 runtime·로컬 합성 모델을 사용한다.
- 실제 머신에서 모델 호출·대화 변경 없이 사용자/워크스페이스 일치, 최근/보관 목록,
  프로젝트 28개와 기존 보관 대화의 내용 읽기를 확인했다.
- Meteor 운영 서버의 `npm ci --omit=dev` 감사 결과는 취약점 0건이었다.
- Go CLI와 Rust TUI를 함께 빌드하고 SHA-256을 생성했다. Rust 제품 코드는 변경하지 않았다.
- 로컬 Mac의 기존 실행 링크/설정을 private 경로에 백업한 뒤, 검증한 동일 버전
  CLI/TUI/웹을 새 immutable 디렉터리에 설치했다. `http://127.0.0.1:4317`의
  `org.codeasy.codexm.web` launchd 서비스가 실행 중임을 확인했다. 다른 장비와
  Homebrew 배포는 변경하지 않았다.

보관 대화 회귀 검사에서는 native `thread/queue/list`가 보관 대화를 거부하지만
내용 페이지는 읽을 수 있음을 확인했다. 보관 화면은 큐를 조회하지 않고 보관 상태를
유지한다. 이 경우를 공식 runtime/브라우저 검사 양쪽에 추가했다.

브라우저 검증은 저장소에서 재현할 수 있다. 먼저 Node 24 환경에서 제품과 웹 번들을
빌드하고, 아래 명령을 default/codexm의 이름 있는 개발 작업 창에서 실행한다.

```sh
node web/tests/run-chat-gui.mjs \
  --binary /absolute/verified-build/codexm \
  --runtime /absolute/official/codex \
  --bundle /absolute/dist/web/bundle \
  --output /private/path/outside/source
```

Playwright 브라우저가 준비돼 있어야 한다. 설치된 Chrome을 쓸 때는
`CODEXM_BROWSER_CHANNEL=chrome`을 지정한다. 실행기는 합성 계정/이력의 일시적인
OS 사용자 서비스를 쓰고 검사 후 등록을 제거한다. 결과·스크린샷은 Git 밖에 남는다.

`CODEXM_WEB_TEST_MODE=catalog`과 `--mongod /absolute/mongod`를 지정하면 공식 런타임에서
채팅 제목을 바꾼 뒤 로그인, 채팅 진입, 브라우저 종료 후 주기 갱신을 검사한다.
로그인과 채팅 진입 시에는 이전 목록이 바로 열리고, 갱신한 제목이 웹과 MongoDB에
반영되는지 확인한다.

## 한도 0% 인계 재현

같은 실행기에 환경 변수 `CODEXM_WEB_TEST_MODE=quota`와
`CODEXM_WEB_QUOTA_WINDOW=5h` 또는 `week`를 지정한다. 로컬 응답 서버가 테스트
계정의 사용량 100%와 429를 제공한다. 공식 Codex 런타임이 한도 오류를 기록하고 기존
writer 잠금을 유지한 채 웹에서 다른 계정을 선택한다. 계정 전환 전후의 채팅 ID,
기존 writer, 새 계정으로 전송된 횟수, 같은 서버의 다른 채팅 상태를 검증한다.

`CODEXM_WEB_QUOTA_BASELINE=1`을 지정하고 수정 전의 바이너리와 번들을 전달하면
기존 잠금에 막히는 동작을 재현한다. `CODEXM_WEB_QUOTA_OWNER=stdio`는 공식 제어
소켓을 제공하지 않는 외부 실행을 재현한다. 모두 합성 로그인, 임시 대화, loopback
서버를 사용한다. 스크린샷과 확인 결과는 `--output`으로 지정한 private 경로에 남긴다.

## 경계

- 외부 GUI/CLI의 진행 중 턴은 writer와 일치하는 공식 Unix app-server 연결에서 제어한다.
  외부 stdio만 있는 경우에는 현재 턴에 연결할 수 없으며, 원래 소유자의 공식 큐를 사용한다.
- 브라우저 종료와 웹 서비스 종료는 다르다. 웹 서비스 중지는 웹 소유 작업도 중단한다.
- 고정은 같은 머신의 native 섹션이다. 계정 서버의 GUI 설정이나 다른 장비의 목록을 동기화하지 않는다.
- Linux 사용자 systemd 정의의 인수/경로 처리는 단위 검사했으며, 이번 실제 실행 검증은
  로컬 Mac이다. 다른 장비 설치·공개 HTTPS 노출·Homebrew 재배포는 이 작업에 포함하지 않는다.
