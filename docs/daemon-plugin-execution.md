# 로컬 서비스·플러그인 구현 기록

계획: [daemon-plugin-plan.md](daemon-plugin-plan.md). 시작일: 2026-10-12.
계정 관리가 제품의 핵심이며, 서비스와 UI는 같은 계정 정책/전환 경로를 사용한다.

## 검증한 관리 코어

- `codexm switch-account JOB ACCOUNT`, TUI `/account` 선택 메뉴와 `/account 이름` 추가.
  대상 작업의 실행 의도, 대화 ID, 목표와 예산을 보존하며 전환 이유를 기록한다.
- 수동 전환도 fallback 조건을 지킨다. 사용 가능 여부 확인 실패는 한도 소진이 아니다.
  중복 요청, 오래된 선택, 전환 도중 관리자 재시작을 다루는 영속 작업을 사용한다.
- TUI 입력 방향키는 기록/커서 이동, 목록은 Tab 또는 Alt+방향키로 이동한다.
- 설정 편집 API는 revision과 파일 잠금으로 동시 저장을 검사한다. 기존 작업 계정과
  런타임/인증/저장 경로를 변경하지 않는다.
- 새 worker는 PID 시작 시각·소유권 receipt를 갖는 독립 프로세스이다. 기존 tmux worker도
  재접속할 수 있다. OS 서비스는 launchd/systemd 사용자 단위로 정의한다.

## 실제 수행한 검증

- `go test -race ./...`, `go vet ./...`, `git diff --check` 통과.
- `scripts/verify-services-local.sh`를 `codexm-test-services-20261012`에서 실행했다.
  빈 인증 파일과 격리한 native 저장소/서비스 이름을 사용했고 모델 turn은 생성하지 않았다.
- `TestOSServiceRestartsAndAdoptsOwnedNativeWorker`: 실제 LaunchAgent SIGKILL 복구,
  관리자 재시작 후 동일 worker/run/thread와 쓰기 잠금 유지, 명시적 shutdown 시 복구 중지 통과.
- `TestOfficialNativeWorkerLifecycleAndWriterLock`: 실제 공식 Codex 엔진으로 수동 계정 변경,
  일시중지 목표와 예산/사용량/모델/effort 보존, 기존 native 대화 읽기/외부 writer 보호 통과.
- 시험용 LaunchAgent와 tmux 세션 정리 완료. 이 기록 시점에 실제 사용 중인 서비스와
  작업 엔진은 교체하지 않았다. 운영 적용과 `.home` revision 변경은 후속 검증 대상이다.

## 플러그인 구현과 추가 검증 범위

- 단일 실행 파일에 stdio MCP 서버와 MCP Apps UI를 포함했다. 계정/잔여 한도, 전체 대화,
  선택 계정 변경, 대화·추가 지시, 전체/세션 로그, 편집 가능한 설정을 연결했다.
- MCP 협상·취소, UI 전용 데이터 경계, 허용된 관리 명령, marketplace 보존/충돌/제거
  테스트와 JS 순수 함수 테스트를 통과했다. 전체 Go race/vet도 통과했다.
- 서비스 upgrade의 기존 worker 보존과 이전 정의 rollback을 구현했다. 추가 upgrade
  실서비스 통합 시험은 사용자 요청 이후 실행하지 않았다.
- 2026-10-12 사용자가 작업 완료 후 테스트를 직접 진행하겠다고 지시하여 추가 테스트를 중단했다.
  모의 호스트 파일은 생성했지만 GUI 렌더링 검증은 완료했다고 표시하지 않는다.
- 설치된 ChatGPT 앱(`com.openai.codex`)은 컴퓨터 제어 도구가 접근을 차단한다.
  공식 manifest와 MCP 협상 검증을 실제 ChatGPT 내 렌더링 성공으로 대신하지 않는다.
- 로컬 검증 후 관리 중인 worker를 보존하는 운영 인계, `.home` 연계, merge/push를 수행한다.
- ibs 등 다른 장비 적용은 이번 로컬 전환 검증에 포함하지 않는다.
