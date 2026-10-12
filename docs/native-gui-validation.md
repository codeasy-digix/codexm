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

현재 컴퓨터 제어 도구는 `com.openai.codex`에 대한 접근을 차단한다. 따라서 실제
Codex 앱 설정/사이드바에서 버튼이 보이는 것과 그 패널의 렌더링·클릭은 아직 확인하지
못했다. 공식 엔진의 설치/연결과 모의 호스트 조작을 실제 앱 UI 확인으로 확대하지 않는다.
앱 패치를 통한 우회나 별도 앱으로의 범위 변경은 하지 않았다.

로컬 설치/서비스/캐시 갱신은 아래 운영 반영 기록으로 구분한다. ibs 등 다른 장비에는
이번 버전을 배포하지 않는다. 앱 호스트의 공식 규격 변경은 UI 호환성 문제로 진단하고
독립 관리 서비스와 기존 worker/계정 상태는 유지하는 구조이다.
