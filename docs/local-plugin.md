# codexm 0.3.1 로컬 플러그인과 계정 관리

CLI·사용자 서비스·MCP Apps 화면은 같은 Go 실행 파일에 포함된다. 공용 웹 화면과
TCP 수신 서버는 없다. 공식 Codex 설치가 필요하며 공식 CLI/ChatGPT 앱을 개조하지 않는다.

## 설치와 시작

```sh
codexm setup --manager account-backup
codexm service install
codexm plugin install
codex plugin marketplace list
codex plugin add codexm@local-personal
```

`local-personal`은 개인 marketplace가 없을 때 만드는 이름이다. 기존 marketplace가
있으면 그 이름을 사용한다. 등록 파일은 `~/.agents/plugins/marketplace.json`,
실제 자산은 `~/.local/share/codexm/plugins/<digest>/`이다. 다른 항목을 보존하고
변경 전 marketplace 사본을 `.codexm.previous`로 남긴다. 인증은 이 파일에 기록하지 않는다.

ChatGPT의 플러그인 화면에서 codexm을 설치/새로고침하고 관리 화면을 연다.
호스트가 로컬 stdio MCP 도구와 MCP Apps 화면을 모두 지원해야 한다. MCP 도구 연결은
화면 렌더링 성공과 다르다. `codexm plugin doctor`도 실제 호스트 렌더링을 성공으로
보고하지 않는다. 캐시된 플러그인은 원본만 변경해도 자동 갱신된다고 가정하지 않는다.

최초 관리자 계정이 없으면 화면에서 반드시 저장된 계정을 선택한다. 이미 저장돼 있으면
불러온다. 계정 추가·재로그인은 `codexm account NAME --login`으로 공식 인증을 사용한다.

## 계정 선택의 세 가지 범위

| 조작 | 적용 범위 |
|---|---|
| `codexm account account-secondary` / 설정의 기본 작업 계정 | 새 작업과 처음 등록하는 대화 |
| 선택 작업의 계정 버튼 / TUI `/account account-secondary` | 그 작업 하나 |
| 설정의 관리자 계정 | 명시적인 관리자 판단 호출 |

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
- 계정: 단기/장기 잔여 한도, 관측 시각, 초기화 시각과 관리 세션 수. 불명확한 값은 미확인.
- 사용 기록: 전체 또는 세션별 실행 시간, 계정과 시작/최근 잔여 관측, 전환 시점·사유.
  비율은 계정 전체 관측이므로 특정 세션의 정확한 소비 비율로 표시하지 않는다.
- 설정: 관리자/기본 작업 계정, 우선순위, 예비 계정, 복귀/상태/사용량 관측 주기,
  동시 실행 수. revision 충돌 시 재조회가 필요하며 다른 설정·인증 경로는 덮어쓰지 않는다.
- 입력: Enter 전송, Shift+Enter 줄바꿈, 한글 조합 중 Enter 보호, 세션별 초안과 입력 기록.
  입력창 방향키는 목록 선택에 사용하지 않는다.
- 응답 대기: 사용자 질문, 명령/파일 승인, 턴 한정 추가 권한, 기본 MCP 입력 폼.
  지원하지 않는 복잡한 요청은 공식 TUI에서 처리한다. 계정 전환 중인 오래된 승인 응답은 거부한다.

화면을 여는 것만으로 새 턴이 생성되지 않는다. UI 전용 도구는 모델 가시성에서 제외하고
대화 내용은 MCP 결과의 UI 전용 `_meta`에 전달한다. 전역 계정 로그인·공식 DB 스키마는
변경하지 않는다. 화면 새로고침은 로컬 RPC이며 별도 관리자 토큰 비용이 없다.

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

## 사용자 후속 확인

2026-10-12 사용자의 요청으로 추가 실사용 테스트는 별도 진행한다. 설치 후 실제
ChatGPT 관리 패널 렌더링, 선택 작업 전환·재개, 설정 저장, 로그 필터와 한글 입력을
확인할 대상이다. 이번 작업에서는 호스트 UI 렌더링 성공을 확인했다고 표시하지 않는다.
격리 모의 호스트 파일 생성기는 `node scripts/plugin-ui-fixture.mjs`이며 실제 계정/작업을
사용하지 않는다. 로컬 제품 자체가 웹 서버를 실행한다는 뜻은 아니다.


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
