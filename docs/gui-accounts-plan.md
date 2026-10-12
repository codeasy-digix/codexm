# GUI 계정 관리와 실제 화면 진입 보완 계획

2026-10-13. 기존 목표의 추가 구현·검증 계획이며 원래 범위를 축소하지 않는다.
실제 설치 0.4.0에는 계정 GUI 로그인 버튼이 없고 상세 페이지에는 Try now만 보였다.
사용자가 다른 에이전트에서 연 화면은 아래로 계속 늘어나 중지했다. 재시작부터 하지 않고
진입 연결과 UI 오류를 먼저 고친다.

## 계정 관리

1. 플러그인 설정에 계정 관리 화면을 여는 직접 진입점을 제공한다. 같은 앱 내 패널에서
   저장 계정, 계정 추가·재로그인, 우선순위 위/아래/제외/추가, 관리자/기본/예비 계정과
   복귀 주기를 다룬다. 별도 웹사이트·앱·공식 Codex wrapper는 만들지 않는다.
2. 최초 관리자와 저장 계정이 없어도 공식 계정 로그인을 시작할 수 있다. MCP 연결의
   수명에 묶인 login broker를 사용해 관리자 service가 로그인 선행 조건이 되지 않게 한다.
3. 공식 App Server의 browser/device 로그인·완료·취소 API만 사용한다. 임시 0700
   CODEX_HOME/sqlite_home와 file credential store를 사용하며 공유 history와 연결하지 않는다.
   모델 turn과 quota 조회는 생성하지 않는다.
4. 별칭 login lock과 기존 인증 지문 비교를 사용한다. 공식 로그인 성공·account/read 검증·
   기존 account ID 일치 뒤에만 named auth를 원자 교체한다. 실패·취소·만료·잘못된 계정·
   중간 파일 변경이면 원래 인증을 유지한다. 등록 완료 전에는 auth.json을 만들지 않는다.
5. broker의 start/status/cancel/close 결과는 UI-only MCP metadata로 보낸다. 일반 모델
   응답·로그에는 URL/device code/token/email/원본 인증 에러를 넣지 않는다. UI는 사용자가
   공식 로그인 페이지에서 직접 인증하게 하고 성공·취소·실패를 구분해 표시한다.
6. OAuth 만료가 제공되지 않으면 broker의 15분 deadline임을 명시한다. UI 닫힘에는 취소를
   시도하고 MCP 종료·deadline에서는 owned 프로세스를 정리한다. 기존 GUI/worker는 종료하지 않는다.

## 화면 크기와 목록

1. 실제 UI resource를 사용하는 격리 host에 size-changed 자동 적용 모드를 추가한다.
   지금처럼 viewport 기반 높이가 host autosize에 되먹임되는지를 재현한다. 이전 모의
   host의 알림 무시를 정상 autosize 검증으로 취급하지 않는다.
2. inline과 fullscreen의 높이 계약을 구분하고 magic viewport 차감값을 제거한다.
   목록/대화의 내부 스크롤과 min-height:0 레이아웃으로 무한 높이 증가를 고친다.
3. 좁은창·긴 목록/메시지·공지·탭 변경·focus에서 크기가 안정되는지 확인한다.
   무조건적 높이 clamp나 목록 삭제로 요구를 대체하지 않는다.

## 분담·검증·반영

- login broker와 그 시험은 하위 에이전트, 높이/목록 오류는 다른 하위 에이전트가 맡는다.
  부모는 MCP/API 연결·계정 UI·설정 진입·문서·설치·통합 검증을 맡는다.
- 계획 commit/push 뒤 구현한다. fake auth fixture에서 성공의 원자 교체, 취소·실패·다른 ID·
  지문 충돌·동시 시작·connection 종료를 검증한다. 실제 공식 빈 인증으로 preview/cancel을
  검사하며 실제 계정 로그인/기존 인증 변경은 시험하지 않는다.
- Go race/vet, JS/skill 검사, 격리 native plugin·auth·autosize 화면 시험을 수행한다.
  임시 장시간 시험은 named tmux이며 종료 후 owned 시험만 정리한다.
- 실제 앱 설정/계정 화면 표시는 모의 host와 구분하여 검증한다. 소스·검증 기록을 commit/
  push하고 로컬 고정 설치와 플러그인 캐시를 갱신한다. 운영 작업·인증·목표·알림을 비교한다.
- 기존 GUI writer의 투명한 계정 교체는 현재 공개 API에 지원 경로가 없는 미완료 요구다.
  새 worker 대화나 모의 시험으로 완료 판정을 대체하지 않는다.
