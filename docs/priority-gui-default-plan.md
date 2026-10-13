# 작업 우선순위와 GUI 공용 default

2026-10-14. 사용자가 정의한 default는 GUI 앱이 사용하는 컴퓨터 공용 로그인이다.
codexm 작업은 관리 패널의 우선순위에서 사용 가능한 첫 계정을 선택하므로 별도
기본 작업 계정을 두지 않는다.

1. 기존 `default_account`를 무시해 읽고 설정을 저장할 때 제거한다. 설정 스키마,
   상태, CLI/TUI와 플러그인에서 기본 작업 계정 항목을 제거한다. 이전 UI가 보낸
   해당 필드에는 우선순위와 별개 동작이 발생하지 않게 명확한 호환 처리를 둔다.
2. 새 작업과 처음 등록한 대화는 자동 선택을 기본으로 둔다. 실제 실행 시 최신
   우선순위와 확인된 가용량을 점검한다. 기존 명시적 작업 계정·대화·목표·예산은
   보존한다. 한도 소진과 예비 계정 복귀 규칙도 유지한다.
3. `account NAME default`와 `--set-default`는 공용 GUI/native 로그인 변경의 의미를
   유지한다. 이전 계정 기능과 현재 GUI의 인증 경로를 읽기 전용으로 대조한다.
   앱의 외부 로그인/캐시 등으로 실제 GUI 전환을 확인하지 못한 부분을 구분한다.
   운영 계정이나 GUI 프로세스를 시험 목적으로 바꾸지 않는다.
4. 우선순위 변경·한도 소진·모두 소진·회복과 명시적 세션 계정 보존을 시험한다.
   Go race/vet, 관련 UI와 격리 공식 엔진 검사를 수행하고 저장 설정의 제거도 검증한다.
5. 검토한 소스를 commit/push하고 `.home` pin·예제·문서를 맞춘 뒤 로컬 설치를
   갱신한다. 관리 관찰자의 인계는 worker를 유지하는 기존 upgrade로 수행한다.
   다른 장비와 배포 게시, 무료 모델 대체는 이번 범위에 포함하지 않는다.

## 구현과 GUI 인증 경로 확인

0.5.5에서 작업 기본 계정 필드를 제거했다. 자동 새 작업은 실제 실행 직전에 최신
우선순위를 확인한다. 기존 작업은 지정 계정을 유지하며 진행 중인 worker를 재생성하지
않는다. 구버전 패널의 전체 설정 저장에 포함된 default_worker는 무시하고 안내한다.
해당 필드만 저장하려는 요청은 다시 패널을 열라는 오류로 거부한다.

설치기의 명시적 migration은 default_account만 제거하고 unknown 확장 설정을 보존한다.
원문은 config.toml.previous로 남기며 재실행은 변경하지 않는다. GUI 기본은 읽기 전용
native_default_account로 표시한다. 공용 인증 링크가 지정한 별칭을 우선 읽으므로 여러
별칭의 실제 계정 ID가 같아도 작업 우선순위로 GUI 기본을 추정하지 않는다.

이전 codex-accounts/default.go의 publishDefault도 공유 홈 auth.json을 named auth로
연결했다. 현재 로컬 GUI PID 6450은 ~/.codex/state_5.sqlite를 사용한다. 설치된 앱의
읽기 전용 bundle 검사에서 appServerClient.getAccount는 account/read,
getAuthToken은 내부 getAuthStatus를 사용하고 ChatGPT backend도 그 인증을 받아 쓰는
호출 흐름을 확인했다. 앱의 Codex 홈 resolver는 CODEX_HOME 또는 HOME/.codex다.
운영 config의 cli_auth_credentials_store는 file이며 기존 명령과 같은 인증 경로다.
앱 bundle을 수정하지 않았고 실제 운영 계정 변경이나 GUI 재시작 시험은 수행하지 않았다.

임시 공식 엔진 시험: tmux codexm-test-priority-20261014
(연결: tmux attach -t codexm-test-priority-20261014). 종료 후 정리한다.
비공개 증거: ~/.local/state/codexm/validation/priority-gui-default-20261014/.

## 최종 내부 검증

- Go 전체 race/vet 통과, UI 회귀 16개 통과. 최종 등록 origin·수동 계정 정책 수정
  후 accounts/config/CLI/plugin/supervisor/TUI race와 vet를 다시 통과했다.
- 공식 CLI 0.162.1에서 실제 worker가 account-work 소진 뒤 account-secondary로 시작하는 것을 확인했고,
  우선순위를 바꿔도 같은 live run/thread/account를 유지했다. 기존 대화 등록·목표와
  예산 보존·수동 계정 전환·writer 보호도 통과했다. 모델 generation은 실행하지 않았다.
- 원본 앱 엔진 0.162.0-alpha.17.2에서 합성 native 로그인/파일 갱신과 플러그인 발견이
  통과했다. 원본 앱 엔진을 worker용 별도 파일로 복사하는 추가 시험은 macOS가
  실행을 signal: killed로 거부했다. 이번에는 그 앱 bundle을 worker 엔진으로 채택하지
  않으며 기존 검증된 CLI pin을 유지한다. 원본 앱 GUI 인증 경로 확인과 복사된 engine
  실행은 다른 검사다. 첫 실패 로그도 private 폴더에 보존했다.
- GUI와 GUI child에 별도 CODEX_HOME/CODEX_ACCOUNT 환경 override가 없는 것을
  읽기 전용으로 확인했다. GUI 재시작·실제 다른 계정으로 로그인 화면 변경은
  사용자 작업을 보호하기 위해 직접 실행하지 않았다.
- 시험 tmux codexm-test-priority-20261014는 종료 뒤 사라졌다. 기존 worker 3개를
  종료하지 않았다. 최종 영수증은 native-final.log/native-final.exit다.
