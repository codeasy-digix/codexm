# codexm 0.5.3 앱 내 플러그인과 계정 관리

CLI·사용자 서비스·MCP Apps 화면은 같은 Go 실행 파일에 포함된다. 공용 웹 화면과
TCP 수신 서버는 없다. 공식 Codex 설치가 필요하며 공식 CLI/ChatGPT 앱을 개조하지 않는다.

## 설치와 시작

```sh
codexm plugin install
codex plugin marketplace list
codex plugin add codexm@local-personal
```

`local-personal`은 개인 marketplace가 없을 때 만드는 이름이다. 기존 marketplace가
있으면 그 이름을 사용한다. 등록 파일은 `~/.agents/plugins/marketplace.json`,
실제 자산은 `~/.local/share/codexm/plugins/<digest>/`이다. 다른 항목을 보존하고
변경 전 marketplace 사본을 `.codexm.previous`로 남긴다. 인증은 이 파일에 기록하지 않는다.

플러그인은 **Codex 앱 → Plugins → codexm → codexm 관리 / 계정 관리** 버튼을 선언한다.
**codexm 관리**는 작업 탭, **계정 관리**는 계정 탭을 연다. 두 버튼은 같은 관리 패널을
사용하며 화면 안 메뉴는 **작업·계정·사용 기록·설정**으로 통일한다. 사이드바와 대화 옆
플러그인 패널에도 **codexm 관리** 진입점을 선언한다. 버튼은 모델을 거치지 않고
MCP 도구를 직접 호출한다. 말로 화면을 열어달라고 요청하는 흐름을 기본 사용법으로 쓰지 않는다.

설정 버튼은 `openai/settings`의 UI tool action과 `settings` entrypoint이며,
관리 패널은 global/thread entrypoint도 제공한다. 해당 진입점을 노출하는 호스트가 필요하다.
열기 결과에도 UI resource URI를 넣어 도구 목록 캐시가 없는 경우의 조회를 돕는다.
호스트가 로컬 stdio MCP 도구와 MCP Apps 화면을 모두 지원해야 한다. 버튼이 소개 화면에
노출되지 않는 호스트에서는 아래 명령 스킬로 진입한다. MCP 도구 연결은 화면 렌더링
성공과 다르다. `codexm plugin doctor`도 실제 호스트 렌더링을 성공으로
보고하지 않는다. 캐시된 플러그인은 원본만 변경해도 자동 갱신된다고 가정하지 않는다.

**Try now**가 있는 플러그인 소개·상세 화면은 작업 관리 화면과 구분한다. Try now는
codexm 작업 실행 명령이 아니며, 그것을 누르거나 플러그인을 선택했다고 관리 작업이
생성됐다고 안내하지 않는다. 실제 관리 화면에는 **작업·계정·사용 기록·설정·+ 새 작업**이
있어야 한다. 0.4.0에서 사용자가 확인한 화면은 Try now가 있는 상세 화면이었고,
열기 도구를 호출해도 화면이나 안내 글이 표시되지 않았다고 확인했다. 이후 사용자가
제공한 실제 앱 사진에는 대화 오른쪽에 작업·계정·사용 기록·설정과 새 작업 버튼,
서비스 연결 상태가 표시됐다. 이 사진은 관리 패널의 표시 증거이며 소개 화면의 직접
버튼 노출이나 새 명령 스킬의 실제 GUI 호출을 확인한 증거는 아니다.

후속 검사에서는 현재 대화의 MCP Apps 탭을 통해 실제 앱의 관리 메뉴·설정 입력·복원,
계정 추가 창·로그 필터·기존 작업 선택을 직접 조작했다. 기록은
[실제 앱 조작 검증](native-gui-validation.md#실제-앱의-기존-mcp-apps-패널-조작)을 따른다.
열린 기존 MCP 연결은 이전 버전을 유지하고 있었다. 스킬 목록·설치 캐시 갱신과 활성 연결의
도구 목록 갱신은 구분한다. 새 계정 열기 도구가 미노출이면 호스트에서 플러그인을 다시
연결해야 한다. 기존 worker나 계정 로그인을 종료하여 갱신하지 않는다.

## 명령으로 화면 열기

0.5.3은 정확한 명령을 **모델에 제출하기 전** 동기 `UserPromptSubmit` 훅에서 처리한다.
공식 플러그인 딥링크로 관리 화면을 요청하고 프롬프트 제출을 차단한다. 별도 모델 호출이나
자연어 해석은 하지 않는다. 로컬 GUI 채팅 입력창에 아래 명령 하나만 입력하고 전송한다.

| 화면 | GUI 호출 | GUI 전용 스킬 선택 | CLI/IDE 호출 |
|---|---|---|---|
| 작업 관리 | `@codexm manage` | `@codexm-manage` | `$codexm-manage` |
| 계정 관리 | `@codexm accounts` | `@codexm-accounts` | `$codexm-accounts` |

최초 한 번 **Settings → Hooks**에서 codexm의 `UserPromptSubmit` 명령 훅을 검토하고
신뢰한다. 같은 Codex 홈을 쓰는 공식 CLI에서는 `/hooks`로 검토할 수 있다. 설치만으로
자동 신뢰되지 않으며 정의가 바뀌면 다시 검토한다. codexm 설치기는 신뢰 기록을 수정하거나
신뢰 우회 옵션을 설정하지 않는다. `codexm plugin doctor`의
`prompt_command_hook_trust: trusted`와 `prompt_command_hook_enabled: true`를 확인한다.
다른 프로젝트에서는 훅 사용 여부가 달라질 수 있으므로 해당 대화의 Hooks 설정도 확인한다.

목록 조사·사용량 조회·브라우저 ID 찾기를 이어서 실행하지 않는다.
화면을 여는 명령은 현재 대화 등록, 새 작업 시작, 로그인 또는 계정 변경을 포함하지 않는다.

훅이 설치·활성·신뢰되어 있어야 모델 호출을 차단한다. 이 조건이 충족되지 않은 호스트나
클라우드 대화에서는 같은 텍스트가 모델에 전달될 수 있다. 그 경우 스킬은 설정이 필요함을
알리고 멈추며, 모델 호출을 직접 명령 실행으로 위장하지 않는다. 일반 대화와 명령에 추가
지시를 붙인 문장은 그대로 통과한다. 링크 열기 실패도 인식된 명령의 모델 제출을 차단한다.

native `/manage`·`/setting` 등록은 하지 않는다. GUI가 slash 입력을 먼저 처리할 수 있어
`/manage`는 지원 명령으로 보장하지 않는다. 공식 훅과 딥링크 API만 사용하며 앱 파일과
공식 `codex`를 수정하지 않는다. 근거는 [공식 플러그인 훅](https://developers.openai.com/plugins/build/plugins#configure-installed-server-policy),
[UserPromptSubmit](https://learn.chatgpt.com/docs/hooks#userpromptsubmit)을 따른다.
운영체제의 링크 열기 성공은 실제 패널 표시의 증거와 구분한다.

도구의 성공 응답은 화면 표시와 구분한다. 연결 도구가 없으면 한 번 안내하고 종료하며
재시도 루프나 별도 웹 화면을 만들지 않는다. 관리 메뉴가 나타나기 전에는 **+ 새 작업**을
현재 앱에서 사용할 수 있는 버튼으로 안내하지 않는다.
설치 앱은 독립 MCP tool call을 화면 대상으로 처리하며 `functions.exec` 출력 JSON을
관리 화면으로 바꾸지 않는다. 도구가 이 경로로 실행되는 환경에서는 플러그인 설정이나
사이드바의 직접 진입을 사용한다. 채팅 인라인 화면에는 호스트의 별도 기능 조건도 있다.

저장 계정이 없으면 **계정 관리 → 계정 추가 → 별칭과 로그인 방식 선택 → 로그인 시작 →
공식 로그인 페이지 열기** 순서로 인증한다. 비밀번호는 공식 페이지에서 입력한다.
device 방식은 공식 페이지에 이 창에 표시된 코드를 입력한다. 상태는 자동으로 확인하며
**로그인 취소**도 가능하다. 관리 서비스나 관리자 계정이 없어도 로그인할 수 있다.

로그인이 검증된 뒤에만 별칭 계정에 저장한다. 기존 별칭의 **재로그인**은 같은 계정임을
확인하며 실패·취소·만료·중간 인증 변경은 기존 인증을 보존한다. 로그인 자체는 관리자,
기본 계정, 우선순위나 기존 작업의 계정을 바꾸지 않는다. 최초 관리자 계정이 없으면
연결 화면에서 저장 계정을 선택하고 **관리 서비스 시작**을 누른다. 이미 저장돼 있으면 불러온다.
서비스 연결 후 **계정 → 우선순위·관리 설정** 또는 **설정**에서 관리자/기본/예비 계정과
계정 순서를 수정하고 **설정 저장**을 누른다. 우선순위는 위/아래/제외/추가 버튼을 사용한다.

터미널에서도 같은 제품의 명령을 사용할 수 있다.

```sh
codexm account NAME --login
codexm account NAME default         # 새 작업·처음 등록하는 대화의 기본 계정 저장
codexm setup --manager account-backup
codexm service install
```

## 작업은 어디서 실행되는가

| 시작 방법 | 실제 실행 주체 | 한도 중단 시 |
|---|---|---|
| 관리 패널 → 새 작업 | codexm이 실행한 별도 공식 Codex CLI App Server | 사용 가능한 계정으로 같은 대화를 재개 |
| 기존 GUI 채팅에 관리 등록 | 원래 GUI 실행이 계속 담당 | 원래 writer 해제 후 codexm의 CLI worker로 인계 |

자동 계정 교체를 처음부터 적용하려면 관리 패널의 **새 작업**에서 폴더·작업 계정을
선택하고 지시를 입력한다. 터미널의 대화형 TUI를 따로 열 필요는 없다. 패널은 조작과
표시를 맡고 독립 관리 서비스가 작업별 백그라운드 프로세스를 관리한다. 패널을 닫아도
작업은 계속된다. 이후 같은 패널에서 작업을 선택해 추가 지시·계정 변경·로그를 사용한다.

`@codexm`을 선택하거나 사용법을 묻는 것만으로 현재 채팅을 자동 등록하지 않는다.
기존 대화에는 아래처럼 관리 적용을 명시한다. 등록과 GUI 실행의 계정 교체는 구분한다.

## 현재 채팅에 관리 적용

로컬 Codex 채팅에서 `@codexm 이 대화를 관리해줘`라고 명시적으로 호출한다.
함께 설치된 스킬이 아래 명령으로 현재 thread를 등록한다.

```sh
codexm register --current --json
codexm register THREAD_ID --account account-work
codexm unregister JOB_ID
```

`--current`는 `CODEX_THREAD_ID`를 사용하며 없으면 임의 대화를 선택하지 않는다.
다시 호출해도 동일한 등록을 반환한다. 기존 채팅·목표·예산·사용량을 보존하고 등록만으로
새 턴을 생성하지 않는다. 현재 로그인 계정은 인증을 바꾸지 않고 확인되는 별칭만 표시한다.

공식 앱/CLI가 대화의 writer를 갖고 있으면 **원래 Codex에서 실행 중 / 인계 대기**이다.
이 상태에서는 현재 앱 에이전트의 계정이 즉시 변경됐다는 뜻이 아니다. 한도 중단은
메타데이터로 감시하지만 자동 계정 인계와 수동 변경은 원래 writer가 해제된 뒤 실행한다.
다른 Codex를 강제 종료하거나 잠금 파일을 삭제하지 않는다. 앱이 writer를 계속 보유하면
인계도 계속 대기한다. 처음부터 codexm이 실행한 작업은 이 외부 writer 대기가 없다.
관리 해제는 codexm의 실행을 정리하고 등록만 해제하며 원래 채팅과 목표는 보존한다.
현재 공식 API에서 GUI 대화 하나의 writer를 회수하거나 실행 계정만 교체하는 경로는
찾지 못했다. 화면 이동·답변 중단이 즉시 해제를 보장하지 않는다.
[GUI 인계의 현재 경계](gui-handoff-boundary.md)를 참고한다.

## 계정 선택의 세 가지 범위

| 조작 | 적용 범위 |
|---|---|
| `codexm account account-secondary` / 설정의 기본 작업 계정 | 새 작업과 처음 등록하는 대화 |
| 선택 작업의 계정 버튼 / TUI `/account account-secondary` | 그 작업 하나 |
| 설정의 관리자 계정 | 명시적인 관리자 판단 호출 |

`codexm account NAME`, `codexm account NAME default`, `codexm account NAME --set-default`는
같은 저장 동작이다. 계정 선택 후 `codexm account`로 저장값을 확인한다. `default`는 계정
별칭이 아닌 동작 이름이며 계정 이름 바로 뒤에 둔다. 기존 작업 계정은 그대로 유지한다.

CLI의 수동 전환은 `codexm switch-account JOB_ID ACCOUNT`이다. 활성 작업의 실행 의도,
동일 thread ID, 목표 예산과 사용량, 일시중지 상태를 보존한다. 전환은 영속 큐로 처리되어
완료 전에는 `현재 → 대상` 상태로 보인다. 오래된 현재 계정·중복 요청을 검사한다.

기본 우선순위는 account-work → account-secondary → account-main → account-backup이다. 상위 세 계정이 모두
소진된 것으로 확인될 때만 account-backup으로 이동한다. 조회 실패는 소진으로 처리하지 않는다.
예비 계정을 사용하는 동안 기본 600초마다 상위 계정으로 복귀할 수 있는지 확인한다.
수동 계정 선택도 같은 규칙을 따른다. 평상시 감시·전환은 관리자 모델을 호출하지 않는다.

## 관리 화면

- 작업: 전체 native 대화 검색/필터, 즐겨찾기, 기존 대화 관리, 새 작업, 추가 지시,
  일시중지/재개/중지. 대화·도구 결과는 선택한 세션에서 읽고 이전 내역을 더 불러온다.
- 계정: 우선순위에 없는 계정까지 전체 저장 계정, 추가·재로그인·취소, 단기/장기 잔여 한도,
  관측 시각, 초기화 시각과 관리 세션 수. 불명확한 값은 미확인.
- 사용 기록: 전체 또는 세션별 실행 시간, 계정과 시작/최근 잔여 관측, 전환 시점·사유.
  비율은 계정 전체 관측이므로 특정 세션의 정확한 소비 비율로 표시하지 않는다.
- 설정: 관리자/기본 작업 계정, 우선순위, 예비 계정, 복귀/상태/사용량 관측 주기,
  동시 실행 수. revision 충돌 시 재조회가 필요하며 다른 설정·인증 경로는 덮어쓰지 않는다.
- 입력: 선택 시 입력창 자동 포커스, 새 채팅은 빈 입력창, Enter 전송, Shift+Enter 줄바꿈,
  한글 조합 중 Enter 보호, 세션별 초안과 입력 기록. 커서가 입력의 시작/끝에 있을 때
  위/아래로 기록을 탐색하며 작성 중인 초안을 보존한다.
  입력창 방향키는 목록 선택에 사용하지 않는다.
- 응답 대기: 사용자 질문, 명령/파일 승인, 턴 한정 추가 권한, 기본 MCP 입력 폼.
  지원하지 않는 복잡한 요청은 공식 TUI에서 처리한다. 계정 전환 중인 오래된 승인 응답은 거부한다.

도구가 화면을 여는 것만으로 관리 대상 작업의 새 턴이 생성되지 않는다. 두 읽기 전용
화면 열기 도구는 명령 스킬에서 호출할 수 있다. 로그인·변경 도구는 모델 가시성에서 제외하고
대화 내용은 MCP 결과의 UI 전용 `_meta`에 전달한다. 전역 계정 로그인·공식 DB 스키마는
변경하지 않는다. 화면 새로고침은 로컬 RPC이며 별도 관리자 토큰 비용이 없다.

로그인 URL·인증 코드는 UI 전용 metadata와 해당 로그인 창에서만 사용하며 모델 응답,
로그, 저장된 widget state에 넣지 않는다. 이 연결의 로그인 요청은 최대 15분 후 정리되며
이 시각은 제공자의 인증 코드 만료 시각을 뜻하지 않는다. 화면 닫힘에는 취소를 시도하고
MCP 종료·deadline에는 소유한 임시 로그인 서버와 폴더를 정리한다.

긴 목록·대화는 패널 내부에서 스크롤한다. inline 높이를 host에 알릴 때 viewport 높이를
되먹임하지 않으며, fullscreen이나 고정 높이의 작은 창에서는 화면 안에서 스크롤한다.
계정 새로고침과 로그인 상태 조회는 입력/포커스를 보존하며 결과를 계속 덧붙이지 않는다.

## 서비스 제어와 업데이트

```sh
codexm service status
codexm service logs
codexm service restart       # 관찰자만 재시작, 기존 worker 유지
codexm service upgrade       # 새 codexm 바이너리로 교체, 실패 시 이전 정의 복구
codexm service stop          # 관리 작업 중단 후 자동 재시작 해제
codexm service start
codexm daemon status         # 별개인 공식 Codex 공용 데몬의 복구 감시
```

macOS: `~/Library/LaunchAgents/org.codeasy.codexm.{supervisor,watchdog}.plist`.
Linux: 사용자 systemd 단위이며 로그인 사용자 환경을 사용한다. 로그인 전 실행은 보장하지 않는다.
서비스 receipt·로그는 `~/.local/state/codexm/services/`, worker receipt는 `workers/`에 둔다.
강제 종료 시 OS가 관리 서비스를 복구한다. 명시적인 stop은 복구를 해제한다.

기존 0.2의 tmux supervisor는 일반 종료 과정에서 worker도 닫으므로 `service install`만으로
인계했다고 간주하면 안 된다. 설정·관리 DB를 백업하고 기존 observer PID와 실행 파일을
확인하여 worker를 보존하는 인계를 수행한다. 기존 tmux worker는 자연 종료까지 관찰하며
새 실행만 독립 프로세스로 생성한다. writer lock 파일을 지우거나 다른 writer를 종료하지 않는다.

`codexm plugin uninstall`은 codexm이 등록한 개인 marketplace 항목만 제거한다.
공식 호스트의 설치 캐시는 `codex plugin remove codexm@MARKETPLACE`에서 제거한다.
기존 작업·인증·대화·로그와 복구 가능한 플러그인 자산은 유지한다.

## 앱 업데이트와 독립성

앱 bundle·내부 JS·비공개 설정 경로를 패치하지 않는다. GUI가 종료되거나 업데이트되어도
launchd/systemd의 관리 서비스, 계정 정책과 작업 기록은 유지된다. stdio MCP와 공식
MCP Apps 브리지로만 연결하고, 패널은 필요한 호스트 기능을 확인한다. 공식 CLI 업데이트도
진행 중인 작업의 보관 엔진을 교체하지 않는다. 제품과 엔진 갱신은 각각 명시적으로 수행한다.

플러그인은 digest별 불변 자산과 패키지 내부 실행 경로를 사용한다. GUI의 PATH나 앱 설치
위치에 의존하지 않고 codexm의 고정 바이너리를 실행한다. UI는 버전별 resource URI를
사용하고 이전 resource URI도 읽을 수 있게 유지한다. 0.5.3은 `manager-v4.html`을 제공하며
v1/v2/v3 URI도 계속 읽을 수 있다. 도구와 관리 API를 확장할 때
이전 계약을 유지하고 API 버전에 따라 새 기능을 표시한다.

호스트가 향후 공식 확장을 제거·변경하는 경우까지 무조건 동작한다고 보장하지 않는다.
그 경우 UI 연결만 진단하고 관리 서비스/기존 작업은 유지한다. 앱 업데이트 후에는:

```sh
codexm service status
codexm plugin doctor
```

`doctor`는 실제 공식 엔진의 패키지 해석 결과, MCP 발견, 등록 스킬 1개·명령 스킬 2개와
전체 스킬 이름, 설치 캐시 버전 일치와 명령 훅의 발견·활성·신뢰 상태를 확인한다.
`host_ui_verified: false`는 이 명령으로 앱 렌더링을 확인할 수 없다는
뜻이다. MCP/캐시 문제가 있으면 `codexm plugin install`과 `codex plugin add`로 갱신한다.
UI가 지원되지 않더라도 CLI/TUI와 독립 서비스로 동일 계정·작업을 관리할 수 있다.

`plugin doctor`는 `panel_health`에 `mcp_initialized`, `open_requested`,
`resource_served`, `panel_connected`의 마지막 관측을 구분해 표시한다. 패널은 UI 브리지
초기화 후 현재 UI build를 보고한다. 이 기록은 현재 패널이 살아 있다는 증거나 화면 조작
성공이 아니며 `host_ui_verified`는 계속 false다. `panel_connection_matches_current_ui`가
false면 오래된 UI 또는 아직 연결되지 않은 상태를 구분해 확인한다.

관측 파일은 `~/.local/state/codexm/plugin-health/`에 시각·제품 버전·UI build·브리지 종류·
visibility만 저장한다. RPC 인수·대화·인증 원문은 저장하지 않는다. 진단 저장 실패나
이전 호스트의 진단 도구 부재가 관리 화면의 초기화를 막지 않는다.

## 검증 범위

[GUI 검증 기록](native-gui-validation.md)에 공식 엔진의 설치·MCP·설정 발견,
서비스 복구/업데이트, 현재 대화 등록과 격리 패널 조작 결과를 기록한다. 테스트용
`scripts/plugin-panel-host.mjs`는 모의 호스트이며 제품 설치에 포함되지 않는다.
그곳의 브라우저 렌더링을 실제 Codex 앱 설정 화면의 렌더링 성공으로 표시하지 않는다.


## Discord 한도 알림

```sh
codexm notify five-hour             # 모든 저장 계정, 이후 추가되는 계정도 포함
codexm notify five-hour account-work account-secondary  # 명시한 계정으로 한정
codexm notify status
codexm notify off
```

설정 화면에도 5시간 한도 알림 켜기/끄기가 있다. 계정의 300분 창이 100% 사용된 것으로
관측되면 `.home`의 `~/.local/bin/agent_job_done`으로 보낸다. 메시지에는 전체 등록 계정의
단기/장기 잔여량, 초기화·관측 시각, 관리자·기본 계정과 현재 작업별 계정/상태를 함께 넣는다.
조회 실패는 미확인으로 표시하고 대화 내용·목표 원문·인증 정보는 보내지 않는다.

worker의 rolling 한도 이벤트와 기존 사용량 조회에서 알림을 영속 큐에 기록한다.
별도 모델 호출 없이 약 1분 간격으로 계정 메타데이터를 보완하고 대기 알림을 전달한다.
같은 계정/초기화 구간을 다시 보내지 않으며, 계정 전환이나 서비스 재시작 후에도 대기
알림을 보존한다. webhook은 전송 idempotency를 제공하지 않으므로 전송 중 종료/실패는
`delivery_unconfirmed` 또는 `sending`으로 남기고 자동 재전송하지 않는다. 전달 결과는
서비스 로그와 codexm 전용 `quota_notifications` 테이블에 기록한다.

인증 설정은 `.home/config/agent-notify/discord.zsh`와 ignored private 프로필이 소유한다.
수신자 URL을 codexm 설정이나 서비스 정의에 복사하지 않는다. 기본값은 알림 비활성이다.
이번 로컬 설치에서는 사용자가 요청한 전체 계정 알림을 활성화한다.
