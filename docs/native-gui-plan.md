# codexm 직접 GUI 진입과 현재 대화 등록 계획

작성: 2026-10-13, Asia/Seoul. 사용자가 계획 저장·구현·테스트를 승인했다.
기준: 제품 `59c6950`, `.home` `6483837`, 로컬 macOS 15.7.7.

## 완료 기준

1. Finder/Dock의 Codexm 앱과 `codexm gui`에서 채팅 요청 없이 관리 화면을 연다.
   로컬 macOS의 기본 `codexm` 진입은 GUI이며 `codexm tui`로 기존 TUI를 유지한다.
2. 동일 UI/관리 API로 전체 native 대화, 새 작업, 추가 지시, 선택 작업의 계정 변경,
   일시중지/재개, 계정 한도, 전체/세션 로그와 편집 가능한 설정을 제공한다.
3. 기존 채팅에서 `@codexm`을 명시적으로 호출하면 현재 대화를 등록한다.
   plugin에 실제 skill을 묶고 `CODEX_THREAD_ID`로 현재 대화를 확인하는 CLI를 제공한다.
   등록은 idempotent이며 새로운 대화·목표·모델 turn을 만들지 않는다.
4. 외부 공식 앱/CLI writer가 있는 대화는 즉시 감시 대상으로 등록하고 실행 주체를
   화면/응답에 표시한다. 해당 writer를 종료하거나 잠금을 삭제하지 않는다.
   허용된 상태 조회로 감시하고 계정 전환·재개는 writer가 해제된 뒤 같은 대화를
   인계한다. 인계 대기와 실제 codexm 실행을 혼동해서 표시하지 않는다.
5. 설정·인증·대화·목표 예산·기존 8개 작업과 살아 있는 worker를 유지한다.
   공식 `codex` 명령, 기본 로그인과 native SQLite의 스키마/내용을 건드리지 않는다.
6. 5h Discord 알림·중복 방지·전체 계정/작업 현황과 600초 fallback 복귀 정책을
   격리 fixture 및 실제 native 엔진/사용자 서비스로 확인한다. 모델 사용이 필요한
   테스트는 범위를 명시하고, 기존 운영 작업을 시험용으로 중단하지 않는다.
7. 직접 실행한 GUI의 렌더링·화면 조작을 확인한다. 플러그인의 skill 설치/현재 대화
   등록과 MCP 계약도 확인한다. 실제 공식 앱 iframe 지원이 확인되지 않으면 그 UI를
   완료 증거로 사용하지 않으며, 직접 GUI를 여는 plugin 진입을 기본으로 제공한다.
8. 소스·문서·테스트 결과를 commit/push하고 로컬 설치·서비스/플러그인 갱신을 수행한다.
   `.home`은 고정 revision과 앱 설치/복구 경로를 기록한다. 원격 장비 적용·공개 배포는 없다.

## 구현 방식

- 기존 Go 제품/서비스를 그대로 사용한다. 별도 웹사이트나 공개 포트를 만들지 않는다.
- macOS AppKit/WKWebView의 작은 Swift 앱이 Go 실행 파일의 stdio 관리 bridge와 연결한다.
  HTML/CSS/JS를 재사용하며 화면의 클릭은 로컬 RPC로 처리하고 관리 모델을 호출하지 않는다.
- 앱 종료는 GUI bridge만 닫는다. 사용자 서비스와 worker는 계속 실행된다.
- 앱 실행 파일·GUI 자산은 immutable 설치 payload 안에 포함하고 소유한
  `~/Applications/Codexm.app` 링크만 갱신한다. 다른 앱/수정된 설치는 보존한다.
- Linux CLI/사용자 서비스와 TUI는 유지한다. 이번 실제 GUI 설치/시험은 로컬 Mac이다.
- Plugin skill은 관리 등록/GUI 열기/현황/해제를 구분한다. 사용자의 `@codexm` 호출은
  현재 대화 등록을 명시적으로 허용한다. 현재 thread 정보가 없으면 임의 대화를 고르지 않는다.
- 현재 등록/외부 writer 감시는 영속 상태를 사용한다. writer 인계가 대기 중이어도
  모든 관리 동작이 가능한 것처럼 표시하지 않는다. 실행 의도와 목표 상태를 보존한다.

## 순서와 검증

1. 계획 commit/push → GUI shell/bridge·직접 진입·패키징.
2. 현재 대화 등록·plugin skill·외부 관측/인계·GUI의 누락/오류 보완.
3. Go race/vet, JS 동작, 설치 충돌/업데이트, native worker·서비스 복구/upgrade/rollback,
   계정 선택·quota/fallback·중복 Discord 전송·외부 writer 보존 회귀 시험.
4. named tmux 안의 격리 서비스로 실제 GUI를 실행하고 화면·입력·설정·계정/로그를 조작한다.
   시험용 서비스/worker/tmux는 종료 후 정리하고 결과를 docs/native-gui-validation.md에 남긴다.
5. 완료 항목별 증거 확인 → main merge/push → 고정 설치·서비스/플러그인 업데이트·인계 확인.

공식 참조: [plugin 구조/skill 설치](https://developers.openai.com/plugins/build/plugins),
[skill 묶기](https://developers.openai.com/plugins/build/skills#package-the-skill).
로컬 엔진 protocol schema는 Git 밖 `~/.local/state/codexm/validation/gui-20261013/schema/`에 생성했다.
