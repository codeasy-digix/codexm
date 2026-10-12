# codexm 0.4.0 앱 내 패널과 현재 대화 등록 검증

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
컴퓨터 제어의 native Codex 접근 제한은 우회하지 않았고 실제 앱 렌더링은 사용자 확인이
필요하다. 기존 GUI 대화의 투명한 계정 교체도 공개 API 경계로 아직 완료되지 않았다.

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
