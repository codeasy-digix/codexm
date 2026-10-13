# 기존 GUI 대화의 계정 인계: 현재 지원 경계

2026-10-13, 공식 CLI 0.162.1, 설치 앱 26.1007.30912 / build 20156과
앱 내장 엔진 0.162.0-alpha.17.2의 읽기 진단.
기존 GUI 채팅에 `@codexm`을 적용하면 같은 대화에서 한도 후 자동으로 다른 계정이
계속 실행한다는 요구는 아직 완료되지 않았다. 새 대화를 fork하거나 별도 작업을 생성한
결과를 이 요구의 완료 증거로 사용하지 않는다.

## 공개 API에서 확인한 것

- `thread/unsubscribe`는 호출한 connection의 구독만 해제한다. codexm이 호출해도
  원래 GUI connection을 해제할 수 없다. 마지막 구독 해제 뒤에도 구독·활동이 없는
  상태가 30분 지속된 후 서버가 unload하고 `thread/closed`를 보낸다.
- 설치 엔진으로 stable 요청 105개, experimental 포함 170개의 schema를 생성했다.
  `thread/stop`, `thread/close`, `thread/unload`, `thread/handoff`와 대화별 account switch
  메서드는 목록에 없다. 이는 현재 공개 schema의 범위에 대한 결론이다.
- 명령 스킬 후속 작업 중 독립 감사에서 두 엔진의 schema를 새로 생성했다. 공식 CLI의
  주요 schema는 기존 진단과 일치하며 앱 내장 엔진도 experimental 요청 170개와 같은
  인증·writer 경계를 보였다. 별도 CLI가 오래돼서 생긴 차이로 판정하지 않는다.
- `thread/start`, `thread/resume`, `turn/start`, experimental settings update에는
  저장 account/auth/profile 선택 필드가 없다. `account/login/start`에는 대화 대상이 없다.
- `turn/interrupt`는 턴 중단이며 writer 해제를 약속하지 않는다. archive/delete는
  대화와 하위 대화의 저장 상태를 바꾸므로 계정 교체용으로 사용하지 않는다.
- `remoteControl/client/revoke`는 environmentId/clientId를 받는 원격 장비 관리 경로다.
  로컬 GUI 대화의 subscriber를 해제하는 근거가 없다.
- 앱의 `handoff_thread`는 다른 대화를 checkout/worktree·host 사이에서 옮긴다.
  계정 선택을 받지 않고 현재 대화 자신에게 사용할 수 없어 이 요구의 대체 API가 아니다.

공식 설명: [unsubscribe](https://learn.chatgpt.com/docs/app-server#unsubscribe-from-a-loaded-thread),
[auth](https://learn.chatgpt.com/docs/app-server#auth-endpoints),
[API 목록](https://learn.chatgpt.com/docs/app-server#api-overview),
[원격 제어](https://learn.chatgpt.com/docs/developer-commands#codex-remote-control).

## 설치 앱의 현재 동작

ASAR의 정적 읽기에서 inactive owner 구독 해제 TTL은 3시간이고 최대 inactive owner는
10개였다. 열린 대화·owner followers·진행 턴 등은 해제 후보에서 제외된다. 서버의 30분
grace와 별개이며, 화면 이동이나 한도 중단만으로 writer 해제를 보장하지 않는다.
정적 소스의 thread/stop 분기는 ephemeral durable host 조건이고 설치 엔진의 공개
experimental schema에도 없으므로 호출하거나 구현 후보로 쓰지 않았다.

현재 codexm은 metadata로 한도 중단을 감지하고 인계 의도를 저장한다. 외부 writer가
있으면 `external_writer`에 머물며, 실제 writer가 해제된 뒤 같은 thread ID를 계정별
공식 CLI worker에서 resume한다. 이 보존 동작을 투명한 GUI 계정 교체라고 안내하지 않는다.

## 남은 요구와 증거

현재 GUI 대화 하나의 실행을 안전하게 인계하는 공식 호스트 경로가 필요하다. 그런 경로가
추가되거나 사용자가 실행 방식을 변경하기 전에는 이 요구를 완료로 판정할 수 없다.
잠금 삭제, GUI 프로세스 강제 종료, 앱 패치나 전역 로그인 변경으로 대체하지 않는다.

비공개 schema artifact는 Git 밖
`~/.local/state/codexm/validation/gui-20261013/schema-handoff-experimental/`에 있다.
진단은 모델 턴·운영 API 변경 없이 수행했다. 대화·목표·계정 인증·writer·기존 worker는 유지했다.
