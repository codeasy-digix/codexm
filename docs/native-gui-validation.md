# codexm 앱 내 패널·명령 스킬·현재 대화 등록 검증

## 0.5.3 모델 호출 없는 명령 훅 (2026-10-13)

정확한 `@codexm manage`, `@codexm-manage`, `@codexm accounts` 및 계정 전용 명령을
동기 `UserPromptSubmit`에서 처리한다. 공식 `codex://plugins/.../app/codexm_open`
딥링크로 화면을 요청하고, 성공·실패 모두 `decision: block`으로 모델 제출을 차단한다.
일반 입력은 `{}`로 통과한다. 이 핸들러는 관리 설정·계정·대화 DB를 읽지 않는다.

공식 CLI 0.162.1과 앱 번들 0.162.0-alpha.17.2 양쪽에서 다음을 확인했다.

- 플러그인 설치, MCP 연결, 설정 진입점·두 열기 도구·UI resource와 스킬 3개 발견.
- 설치 직후 명령 훅 1개가 `untrusted`로 발견됨. 테스트 전용 임시 HOME에서 해당
  fixture 해시만 신뢰한 뒤 실행했다. 운영 신뢰 설정은 수정하지 않았다.
- 일반 텍스트·Markdown 플러그인 멘션·구조화된 plugin/skill 입력을 포함한 명령 5개와
  링크 열기 실패: 모델 HTTP 요청 **0회**. 일반 입력은 로컬 가짜 모델 서버로 **1회**.
- 망가진 관리 설정에서도 설치 실행 파일의 일반 입력 훅은 정상 통과.
- 운영 계정 사용·운영 대화 턴 생성·실제 OS 링크 열기는 **0회**. 훅의 OS opener만
  시험 영수증 함수로 대체하고 나머지 공식 engine → command → block 흐름을 사용했다.

단위·회귀 검증: `go test -race ./...`, `go vet ./...`, JS 테스트 16개,
스킬 3개의 `quick_validate.py` 통과. UI는 초기 deep link와 후속 경로 변경으로
작업/계정 탭을 선택하고, 관련 없는 theme 갱신이 사용자 탭 선택을 덮어쓰지 않는다.
같은 명령을 반복해도 다시 탐색하도록 딥링크에 호출별 request 값을 붙이며, UI는
경로로 탭을 선택하고 전체 앱 상대 URL로 새 탐색과 기존 상태 갱신을 구분한다.

호환성 시험에서 root `plugin.json`을 둔 portable AgentPlugin 형식은 훅을 발견하지
못했다. 공식 [loader](https://github.com/openai/codex/blob/main/codex-rs/core-plugins/src/loader.rs)도
해당 형식의 훅 로드를 건너뛴다. 현재 설치는 공식 `.codex-plugin/plugin.json` 형식을
사용하며 root portable manifest를 설치하지 않는다. 기존 MCP Apps 기능은 두 엔진에서
다시 검증했다. 0.5.2의 동시 계정 기본값 수정도 병합해 유지했다.

운영 사용의 남은 단계: 사용자가 Settings → Hooks 또는 공식 CLI `/hooks`에서 현재
codexm 명령 훅을 검토·신뢰해야 한다. 새 명령 입력으로 실제 데스크톱 패널이 열린
화면은 직접 검증하지 못했다. 기존 패널 조작 기록과 이 새 명령 시험을 혼동하지 않는다.
`/manage`의 GUI slash 라우팅은 지원으로 보장하지 않는다.

private 실행 로그: `~/.local/state/codexm/validation/prompt-command-20261013/`.
계획은 [모델 없는 명령 계획](model-free-entrypoints-plan.md), 사용법은
[플러그인 안내](local-plugin.md#명령으로-화면-열기)를 따른다.

### 로컬 설치 결과

구현 `5cd57cf`를 `.home`에 고정해 CLI와 공식 plugin cache를 0.5.3으로 설치했다.
캐시 launcher가 최종 설치한 불변 바이너리를 가리키는지 확인했다. `plugin doctor`는
설치·활성·캐시 일치, 스킬 3개, 명령 훅 1개·enabled·untrusted를 반환했다. 설치기가
사용자의 훅 신뢰를 대신 설정하지 않았다. 실제 새 명령의 GUI 표시는 미확인이다.

관리 서비스 정의와 실행 중인 0.5.2 supervisor/watchdog는 그대로 유지했다. 설치 전후
작업 9개의 ID·대화·계정·목표·예산, worker PID 3개, 관리 설정·정책, 공식 CLI 경로·해시,
전역 인증 파일과 `.home` 동시 작업 파일 10개가 일치했다. 조회된 계정 ID도 모두 같았다.
account-backup의 인증 파일 해시는 관측 중 달라졌으므로 모든 인증 바이트가 같다고 기록하지 않는다.
임시 시험 tmux 세션은 종료됐다. `homectl plan` 통과, 전체 `homectl doctor`의 실패 3개는
기존 동시 작업인 markdown2pdf 실행 파일·스타일 경로이며 codexm 항목은 통과했다.
영수증은 위 private 폴더의 `local-install-receipt.json`이다. 다른 장비에는 배포하지 않았다.

2026-10-13, 로컬 macOS ARM64, 공식 Codex CLI/engine 0.162.1.
계획은 [native-gui-plan.md](native-gui-plan.md)에 먼저 commit했다.
별도 앱·제품 웹 서버·공개 포트와 앱 bundle 패치는 추가하지 않았다.

## 공식 엔진을 통한 설치·연결

`TestOfficialPluginDiscoversSettingsEntrypointsAndRegistrationSkill`은 임시 HOME,
빈 인증과 격리 Codex 저장소에서 실제 `codex plugin add`를 수행한다.
다음 항목을 공식 App Server의 응답에서 확인했다.

- portable MCP 서버 `codexm`과 bundled registration skill 1개 발견.
- 설치 캐시 버전 0.4.0, 연결 상태 connected.
- `codexm_open`의 global/thread 진입점과 **codexm 관리** 제목.
- 서버의 `openai/settings` capability와 read/update 도구.
- 실제 settings read 호출에서 계정 정책 필드와 관리 패널 UI action 반환.
- 빈 대화의 모델 turn 0개, 관리 서비스가 시작되지 않음.

기존 0.3.1 패키지는 portable MCP 명령에 외부 절대 경로를 사용했다. 공식 loader가
`Agent Plugins stdio command must be a bare executable name or a contained ./ path`
오류로 서버를 제외하는 것을 확인했다. 패키지 내부 `./bin/codexm` 실행 경로로 고쳤다.
공식 `codex`를 감싸거나 GUI PATH에 의존하지 않는다. `plugin doctor`에 공식 엔진의
MCP/skill 발견과 설치 캐시 버전 검사를 추가했다.

## 코어·사용자 서비스

`scripts/verify-services-local.sh`를 `codexm-test-services-20261013`에서 실행했다.
최종 exit 0. 빈 인증, 임시 저장소와 독립 LaunchAgent를 사용했다.

- `TestOfficialCurrentChatRegistrationAndPausedAccountChangePreserveGoal`: 기존 대화
  등록/idempotency, 원래 native writer 보존, 수동 변경 대기, 쓰기 권한 해제 뒤 계정 정책
  변경, 일시중지 목표와 30,000 토큰 예산 보존, 모델 turn 0개.
- `TestOfficialNativeWorkerLifecycleAndWriterLock`: 실제 native worker 계정 변경,
  목표/예산/사용량/모델/effort 유지, 외부 writer 보호.
- `TestOSServiceRestartsAndAdoptsOwnedNativeWorker`: SIGKILL 복구, 동일 worker/run/thread
  유지, observer 재시작, 실행 바이너리 경로 upgrade, 강제로 실패시킨 readiness의
  이전 정의 rollback, 명시적 shutdown 시 복구 해제.
- 등록 해제 중 in-flight dispatch 거부, 이미 취소한 큐의 오래된 snapshot 실행 방지.
- 한도 중단 관측 후 foreign writer 해제를 기다리는 인계 큐, paused/budgetLimited/
  blocked/complete 상태 보존, 중복 인계 방지 회귀 시험.

테스트의 첫 빌드 오류와 paused 작업에 worker가 있을 것으로 잘못 가정한 첫 테스트
오류를 수정하고 최종 명령을 다시 실행했다. failed 로그도 Git 밖 증거 폴더에 보존했다.

## 패널 조작

실제 MCP 서버·공식 native 엔진·격리 관리자에 연결한 **모의 호스트**를
`codexm-test-panels-20261013`에서 실행했다. 제품의 실제 HTML/CSS/JS resource를
iframe에 렌더링하고 컴퓨터 제어 도구의 브라우저 인터페이스로 조작했다.
이 localhost 웹은 테스트 도구이며 제품 설치물에 포함되지 않는다.

- 공식 settings 응답에서 생성한 필드/계정 선택 메뉴와 관리 패널 버튼 조작.
  600→900초 partial save 후 다른 값 유지.
- 전체 native 목록, 기존 대화 등록과 관리 수 증가, 현재 ID의 CLI 등록 2회가 동일 ID 반환.
- paused 목표의 account-secondary→account-main 변경, 목표 0/30,000과 paused 상태 유지.
- 새 빈 작업 생성, 추가 지시 입력값이 정확히 빈 문자열이며 포커스 true.
- `한글 초안\n둘째 줄`을 다른 세션에 갔다 돌아와 보존, 포커스 유지.
- 전체/선택 세션 로그, 계정 전환 시점·사유와 실행 시간/계정 한도 관측 확인.
- 390px 패널과 다크 테마 렌더링 확인.

첫 패널 시험은 idle 자동 종료 때문에 실패했고, 두 번째는 15분 제한이 지나 종료됐다.
fixture 종료 정책을 수정한 최종 시험은 PASS이며 관리 모델 호출 기록은 0개이다.
처음 확인한 새 대화의 이전 초안 유출은 `choose()` 경로와 포커스 처리로 수정하고
최신 빌드에서 다시 검증했다. 마지막 등록 안내 문구는 실행 계정 인계 시점을 명시했다.

시험용 HTTP/MCP/helper, native worker, OS unit과 두 tmux 세션을 정리했다.
운영 중인 legacy worker 세션 3개는 유지했다. 비공개 증거 위치:
`~/.local/state/codexm/validation/gui-20261013/`.

## 나머지 검증과 실제 앱의 경계

`go test -race ./...`, `go vet ./...`, `node --test plugin/ui/app.test.mjs`,
`git diff --check`가 통과했다. Discord 검사는 HOME을 격리하고 fake helper로
5h 소진 판정, 전체 계정/작업 포함, 내용 제외, 영속 중복 방지와 불확실 전송의
자동 재전송 금지를 확인했다. 이 시험은 실제 Discord로 전송하지 않았다.

컴퓨터 제어 도구는 `com.openai.codex`에 대한 접근을 차단한다. 사용자의 초기
**관리 패널이 열림** 응답을 실제 작업·계정 관리 화면의 확인으로 해석했으나, 후속 답변은
**플러그인 상세 화면에는 Try now만 있고 새 작업 버튼은 없다**였다. 앞선 해석을 정정하며
실제 앱의 관리 화면 진입·렌더링은 **미확인**으로 남긴다. 상세 화면을 관리 패널의 완료
증거로 사용하지 않는다.

이후 현재 채팅에 실제 `mcp__codexm__codexm_open` 도구가 연결됐고 열기 호출은
`structuredContent.ok=true`를 반환했다. 이는 MCP 도구 연결·호출의 증거이며 화면 표시의
증거는 아니다. 호출 후 MCP Apps side-panel tab 목록은 비어 있었다. inline 앱은 이
목록에 포함되지 않으므로 빈 목록만으로 렌더링 실패를 단정하지도 않는다. 기능별 조작은
앞서 실제 MCP/격리 native 서비스의 모의 호스트에서 검증했다. 앱 패치를 통한 우회나
별도 앱으로의 범위 변경은 하지 않았다.

읽기 전용 앱 패키지 진단에서 설치 버전은 26.1007.30912이고 공식 structured settings의
클라이언트 구현이 포함된 것을 확인했다. CLI의 기본 `enable_mcp_apps=false` 값과 앱의
자체 UI 기능 활성화 조건은 구분한다. 이 CLI 값만을 근거로 앱이 미지원이라고 판정하거나
기능 설정을 바꾸지 않았다. 실제 앱 설정의 관리 버튼 노출·렌더링 확인은 남은 작업이다.

로컬 설치/서비스/캐시 갱신은 `.home/docs/codexm.md`의 운영 반영 기록으로 구분한다. ibs 등 다른 장비에는
이번 버전을 배포하지 않는다. 앱 호스트의 공식 규격 변경은 UI 호환성 문제로 진단하고
독립 관리 서비스와 기존 worker/계정 상태는 유지하는 구조이다.

## 0.4.1 진입점·연결 진단 보완

사용자는 0.4.0의 열기 도구 호출 후 화면이나 안내 글도 표시되지 않았다고 확인했다.
실제 root rollout에는 해당 호출이 exec/custom tool output으로 남았고 독립 MCP tool
call은 없었다. 설치 앱은 독립 `mcpToolCall`을 인라인 화면 대상으로 처리한다. 텍스트로
출력한 JSON을 화면으로 재해석하지 않는다. 따라서 기존 호출 성공은 GUI 성공이 아니다.

현재 앱의 정적 parser는 local stdio의 global/thread/settings 진입점을 지원한다. 0.4.1은
settings entrypoint, 열기 결과의 resource URI fallback, resource의 inline/fullscreen
display modes를 추가했다. UI URI는 v3로 올리고 v1/v2 읽기를 유지한다. 별도 앱·웹 서버나
private rollout 설정 변경은 하지 않았다. 인라인 카드에는 별도 호스트 feature 조건이 있다.

연결 진단은 열기 요청·UI resource 응답·브리지 연결을 구분하고 현재 UI build를 확인한다.
시각·버전·허용된 bridge·visibility만 기록하며 `host_ui_verified`는 false로 유지한다.
오래된 캐시·임의 데이터 거부·동시 stage 보존·진단 저장 실패와 이전 호스트의 진단 부재가
기존 기능을 막지 않는 조건을 Go/JS 시험에서 확인했다.

격리 native 시험을 `codexm-test-ui-contract-20261013`에서 실행해 PASS/exit 0을 확인했다.
공식 engine의 plugin install, settings/global/thread metadata, `mcpServer/tool/call`,
`mcpServer/resource/read`와 synthetic UI bridge report를 모두 통과했다. 빈 인증·임시 HOME이며
모델 turn 0개, 관리 서비스 생성 없음이다. synthetic report의 visibility는 false였고 이를
실제 GUI 렌더링으로 판정하지 않았다. 시험 세션은 정상 종료 후 제거됐다.

`go test -race ./...`, `go vet ./...`, JS 5개 시험과 bundled skill validator가 통과했다.
기본 및 bundled Python에는 PyYAML이 없어 validator가 처음 실패했다. Git 밖 별도 venv에
PyYAML을 설치해 재실행한 최종 결과는 valid였다. 시스템 Python은 변경하지 않았다.

writer API 조사와 코드 검토는 읽기 전용 하위 에이전트가 병렬 수행했다. 현재 공개 API의
GUI 인계 부재는 [별도 경계 기록](gui-handoff-boundary.md)에 남긴다. 실제 설정 버튼 노출·
관리 화면 표시와 투명한 GUI 계정 인계는 아직 완료 검증을 통과하지 않았다.

## 0.5.0 계정 버튼·로그인·높이 수정 검증

2026-10-13. 기본 진입을 `codexm 관리`와 `계정 관리` 버튼으로, 패널 메뉴를
`작업·계정·사용 기록·설정`으로 통일했다. 설정 전용 계정 열기는 같은 v4 resource의
계정 탭을 선택한다. 기존 v1/v2/v3 resource도 읽는다. 새 native slash 명령은 선언하지 않는다.

실행한 검사는 다음과 같다.

- `go test -race ./...`, `go vet ./...`: 통과.
- `node --test plugin/ui/*.test.mjs`: 14개 통과. UI login/취소/지연 응답, 입력·포커스,
  민감정보 경계, URL origin 검증과 autosize ack 회귀를 포함한다.
- skill `quick_validate.py`와 `git diff --check`: 통과.
- 공식 설치 엔진 0.162.1에서 `TestOfficialPluginDiscoversSettingsEntrypointsAndRegistrationSkill`
  통과. 실제 MCP에서 두 열기 버튼과 app-only 로그인 도구, 초기 계정 탭, 설정과 resource를
  발견·호출했으며 모델 turn은 없다.
- `TestOfficialLoginBrokerPreviewAndCancelUseOnlyEmptyIsolatedAuth`: 통과. 빈 인증에서
  공식 로그인 preview/cancel 응답과 process Wait 종료, lock 해제·임시 폴더 정리를 검사했다.
  실제 계정 인증은 수행하지 않는다. 로그인 성공·원자 저장과 기존 ID·지문 충돌·취소는
  fake fixture로 검증했다.

모의 autosize host에 수정 전 UI를 연결하면 같은 3개 대화 DOM에서 height 알림이
34회 증가하여 iframe 2452px/body 약 2500px에 도달했다. 이는 목록 fetch가 아니라
viewport 높이 되먹임이다. 기존 모의 host는 size-changed 알림을 무시해 이 문제를 발견하지 못했다.

수정 후 실제 MCP와 격리 native supervisor를 연결한 브라우저 시험에서 계정 버튼으로
계정 탭을 열고, 계정 추가의 빈 browser preview를 취소했으며 저장 인증과 임시 로그인
폴더가 남지 않음을 확인했다. 설정의 아래 버튼으로 account-secondary/account-work 순서를 변경·저장했고
기존 작업 계정 account-work는 유지됐다. 자동 높이는 95→918px, 설정/공지 변경 후 959px로
수렴했으며 자동 증가 guard는 작동하지 않았다. 390×480px에서는 body 높이와 scrollHeight가
모두 480px, app clientHeight 333px/scrollHeight 1150px로 내부 스크롤을 제공했다.
선택 작업의 여러 줄 한글 초안과 입력 포커스도 유지됐다. 표의 계정 한도는 시험 데이터다.

시험은 named tmux `codexm-test-native-v050-20261013`, `codexm-test-panel-v050-20261013`에서
실행했다. 기록은 private `validation/gui-20261013/{native-ui-v050,login-native-v050}.*`와
`panel-v050/`에 남긴다. 중단된 하위 에이전트의 앞선 native panel fixture는 15분 timeout으로
실패했으며 그 host를 종료하고 새 격리 fixture에서 다시 검증했다. 그 실패를 통과로 표기하지 않는다.
최종 panel fixture는 정상 종료(239.45초, exit 0)했으며 위 임시 tmux와 이전 높이 시험
세션은 모두 정리됐다. 운영 legacy worker의 tmux 3개는 유지했다.

이 결과는 실제 Codex GUI 설정 화면의 버튼 표시·클릭 성공을 대신하지 않는다.
당시 실제 앱 렌더링은 미확인이었으며, 후속 사용자의 사진으로 관리 패널 표시가 확인됐다.
컴퓨터 제어의 native Codex 접근 제한은 우회하지 않았다. 기존 GUI 대화의 투명한 계정
교체도 공개 API 경계로 아직 완료되지 않았다.

### 0.5.0 로컬 설치·서비스 인계

구현 commit `5aed09430b60e829d8287fbb7a0b34c22f994661`을 `.home`의 고정 소스
복구·독립 installer로 빌드·적용했다. `codexm service upgrade`와 `codexm daemon watch`로
관찰자만 교체했다. supervisor PID 33175, watchdog PID 33231은 같은 0.5.0 불변 바이너리를
사용한다. 공식 공용 Codex daemon PID 13383은 유지됐다.

`codexm plugin install`과 공식 `codex --no-daemon plugin add codexm@local-personal --json`을
수행했다. `plugin doctor`는 설치/활성/캐시 일치, 버전 0.5.0, MCP 도구 11개와 등록 스킬
1개를 확인했다. UI resource가 준비됐지만 실제 화면은 확인하지 못해
`host_ui_verified:false`, `panel_connection_matches_current_ui:false`를 그대로 기록한다.

설치 전후 SQLite backup과 private 상태/인증 지문으로 작업 9개의 ID·대화·계정·실행 의도,
목표·예산·실행 ID와 엔진, 기존 worker pane PID 3개, config/알림 정책과 전역 로그인,
공식 Codex 바이너리를 비교하여 보존을 확인했다. `account-backup` named auth의 파일 지문은
기존 quota reader의 `account/read refreshToken:true`로 바뀌었다. 원래 관측한 account ID가
같음을 확인했으며 인증 파일이 byte 단위로 동일하다고 표시하지 않는다.
새 GUI 로그인은 시험용 빈 인증 홈에서만 preview/cancel했고 운영 계정 로그인은 하지 않았다.

운영 영수증은 private `validation/gui-20261013/production-v050-after/receipt.json`이다.
`.home`에서 동시 작업 중인 파일 10개의 지문도 병합 전후 같았다. `homectl plan`을 저장하고
독립 제품 설치기를 통해 codexm만 적용했다. `homectl doctor`의 codexm/공식 Codex 검사 2개는
통과했으며 전체 doctor의 실패 3개는 동시 개발 중인 `markdown2pdf` 경로다. 그 파일과
설치에는 개입하지 않았다. 다른 장비 배포나 정식 Release/npm/tap 게시도 수행하지 않았다.

## 0.5.1 명령 스킬과 최신 GUI 증거

사용자가 제공한 실제 Codex 앱 사진에는 `codexm 관리` 탭, 대화 오른쪽의 관리 패널,
작업·계정·사용 기록·설정, 새 작업 버튼과 관리 서비스 연결 상태가 표시됐다.
따라서 현재 패널의 실제 앱 표시를 확인했다. 사진의 경로는 자연어 요청이며 새로운 명령
스킬 호출, 소개 화면의 직접 버튼 노출·클릭 또는 원래 GUI 대화의 계정 교체를 증명하지 않는다.
위 이전 버전의 미확인 기록은 당시 상태를 설명한다.

0.5.1은 `@codexm manage` / `@codexm accounts` 라우팅과 전용 `codexm-manage` /
`codexm-accounts` 스킬을 추가했다. 열기 도구를 한 번 호출하고 응답을 끝내도록 한다.
이는 native slash 등록이 아닌 공식 스킬 호출이다. 계정 열기는 읽기 전용이며 로그인·변경
도구의 app-only 가시성과 UI-only metadata는 보존한다. 설치 digest는 모든 스킬 자산의
정렬된 경로·길이·내용을 포함하며 변경된 불변 자산은 설치가 덮어쓰지 않는다.

실행한 검증:

- `go test -race ./...`, `go vet ./...`, `git diff --check`: 통과.
- `node --test plugin/ui/*.test.mjs`: 14개 통과.
- `quick_validate.py`: 등록 스킬과 명령 스킬 3개 모두 valid.
- `TestOfficialPluginDiscoversSettingsEntrypointsAndRegistrationSkill`: 공식 CLI 엔진
  0.162.1과 실제 앱 내장 엔진 0.162.0-alpha.17.2 양쪽에서 PASS/exit 0. 등록 스킬 1개,
  명령 스킬 2개, MCP 연결·두 열기 호출·초기 계정 탭·설정·resource를 확인했다.
  빈 인증·임시 HOME이며 모델 턴과 운영 상태 변경은 없다.
- 설치 회귀 시험은 세 스킬의 패키징과 변조된 명령 스킬의 덮어쓰기 거부·marketplace
  보존을 검사했다.
- 독립 하위 에이전트는 manage/accounts·전용 스킬·명시적 현재 대화 등록의 다섯 요청을
  읽기 전용으로 검토했다. 호출 도구·인수·종료 동작이 일관됨을 확인했고, 도구 미노출을
  연결 실패로 단정하지 않으며 JSON 응답만으로 표시 성공을 주장하지 않도록 문구를 보완했다.

공식 엔진 시험은 `codexm-test-command-v051-20261013`과 문구 보완 뒤 최종 확인용
`codexm-test-command-v051-final-20261013`에서 실행한 후 정리했다.
private 증거는 `validation/gui-20261013/{native-ui-v051,native-app-ui-v051}.{log,exit}`다.
실제 GUI의 새 명령 호출 동작은 아직 직접 조작하지 못했다. 공식 엔진에서 스킬을 발견한
검증을 실제 GUI 호출 성공으로 표시하지 않는다. 기존 GUI writer 인계 감사 결과는
[현재 지원 경계](gui-handoff-boundary.md)에 기록한다.

### 0.5.1 로컬 설치·보존 확인

구현 commit `20f18d20286228473b55832e3f0ea95a840e9ae8`을 `.home` 고정 revision으로
복구·빌드·설치했다. `service upgrade`와 `daemon watch`로 관리 관찰자만 교체하고
`plugin install`, 공식 `codex --no-daemon plugin add codexm@local-personal --json`을 수행했다.
supervisor PID 48932와 watchdog PID 48999의 실제 실행 경로가 같은 0.5.1 불변 바이너리다.
공식 공용 daemon PID 13383과 기존 tmux worker 3개는 유지됐다.

`plugin doctor`는 버전 0.5.1, 설치·활성·캐시 일치, MCP 11개, 등록 스킬 1개·명령 스킬
2개와 이름 `codexm`, `codexm-accounts`, `codexm-manage`를 확인했다. 기존 열린 패널의
마지막 연결은 0.5.0이므로 최신 UI 일치 관측은 false다. 이를 실제 GUI의 새 명령 호출
실패나 성공으로 단정하지 않는다. 현재 앱 연결을 종료해 강제 갱신하지 않았다.

설치 전후 private SQLite backup·상태·지문 비교에서 작업 9개의 ID·같은 대화·계정·
실행 의도·목표·예산·설정·실행 ID·엔진·마지막 입력, manager/default/config/알림 정책,
전역 로그인·공식 CLI·legacy worker PID 3개, `.home` 동시 변경 파일 10개의 보존을 확인했다.
`account-backup` 인증 파일의 지문은 달라졌고 관측된 계정 ID는 동일했다. 기존 사용량 reader는
`account/read refreshToken:true`를 사용한다. 인증이 byte 단위로 그대로라고 주장하지 않는다.
운영 계정의 새 GUI 로그인은 수행하지 않았다.

`codexm doctor --json`은 통과했다. `homectl plan --json`을 저장한 뒤 외부 제품의 독립
설치기로 codexm만 적용했다. `homectl doctor --json`의 codexm·공식 Codex 검사는 통과했고,
전체 실패 3개는 동시 개발 중인 markdown2pdf 경로다. 관리 모델의 지난 한 시간 호출 수는
0이었다. 영수증은 private `validation/gui-20261013/production-v051-after/receipt.json`이다.
다른 장비 적용, 정식 배포 게시와 기존 GUI writer 강제 해제는 하지 않았다.

### 실제 앱의 기존 MCP Apps 패널 조작

후속 goal 턴에서 현재 대화의 MCP Apps side-panel 탭을 컴퓨터 제어 도구가 노출했다.
이번 검사는 모의 호스트가 아니라 실제 앱 패널이다. native Codex 앱 접근 차단을 우회하거나
앱 bundle을 변경하지 않았으며 공식 MCP Apps의 DOM 인터페이스만 사용했다.
연결 진단의 마지막 UI/resource 버전은 0.5.0이고 관리 서비스·설치 캐시는 0.5.1이다.
아래 결과를 새 0.5.1 스킬 호출의 증거로 사용하지 않는다.

- 설정에서 복귀 주기 600→660, 기본 작업 계정 account-secondary→account-work를 입력했다. 저장하지 않고
  저장된 값 다시 불러오기로 600·account-secondary 복원을 확인했다. 관리자·예비 계정과 5h 알림도 표시됐다.
- 계정 추가 창에서 별칭 입력·browser/device 선택·로그인 시작·닫기 버튼을 확인하고
  device 방식을 선택한 후 닫았다. 로그인은 시작하지 않았다.
- 새 작업의 첫 지시는 빈 문자열·줄바꿈 0개였다. 여러 줄 한글 입력·취소·재진입 뒤
  빈 문자열·줄바꿈 0개를 다시 확인했다. 작업 시작은 누르지 않았다.
- 전체 로그는 16행, 선택한 기존 작업의 로그는 2행이었다. 세션·계정·기간 필터와
  실행 구간·잔여 관측·전환 사유를 확인한 뒤 전체 세션 필터로 되돌렸다.
- 기존 관리 작업을 검색·선택하자 추가 지시 입력은 활성·빈 값·줄바꿈 0개·포커스 true였다.
  해당 작업의 계정 변경 창과 네 계정·예비 정책을 확인하고 취소했으며 포커스 true가 유지됐다.
  추가 지시 전송·계정 변경·재개·중지는 실행하지 않았다.
- 마지막 패널의 viewport/body 높이는 모두 1020px이었다. 계정 화면으로 돌아왔다.

private `live-ui-receipt.json`에 검사와 상태 비교를 저장했다. 설정 파일 지문과 작업 9개의
ID·대화·계정·실행 의도·목표·예산·실행 ID·엔진·입력은 이전 설치 검증과 동일했다.
관리 모델의 지난 한 시간 호출 수는 0이다. private 이미지 `live-settings-panel.jpg`,
`live-account-dialog.jpg`도 저장했다. 별도 서버·시험 tmux는 만들지 않았다.

현재 모델의 도구 목록은 `codexm_open`만 노출하고 `codexm_accounts_open`은 없었다.
0.5.1 스킬의 발견과 기존 MCP 연결의 새 도구 노출은 같은 검증이 아니다. 공식
`config/mcpServer/reload`는 디스크 설정을 재조회하고 loaded thread의 갱신을 예약하지만
params는 null이며 특정 서버·대화 선택 필드가 없다. 이 공식 설명은 GUI writer의 해제나
대화별 인증 교체를 제공하지 않는다. 운영 연결에 광범위 갱신을 실행하지 않았다.
[공식 API](https://learn.chatgpt.com/docs/app-server#api-overview)를 참고한다.
