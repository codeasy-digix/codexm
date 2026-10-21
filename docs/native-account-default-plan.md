> 이전 구현 기록입니다. 현재 명령 의미는 [native 계정·터미널 선택](native-account-shell-20261022.md)과 [README](../README.md)를 따릅니다.

# Native Codex 기본 로그인 복구

2026-10-13. 사용자의 최종 범위는 `codexm account NAME default` 및
`--set-default`를 native Codex 기본 로그인 변경으로 복구하고 종료하는 것이다.
무료 모델 대체·한도 관리 모드 확장 구현은 이번 설치에 포함하지 않는다.

- 명시적 default 명령은 공유 Codex HOME의 파일 인증만 바꾼다. 공식 실행 파일,
  부모 셸, worker 계정/우선순위, 목표와 GUI 프로세스는 그대로 유지한다.
- 기존 named 인증을 검증하고 원래 native 인증을 private 백업한 뒤 원자적으로
  링크를 교체한다. native 토큰 갱신도 선택한 named 인증 파일에 반영된다.
- 파일 인증 저장소만 지원하며 keyring/auto/ephemeral 또는 관리 범위 밖 링크는
  조용히 성공 처리하지 않는다. 데몬 조회 실패가 기본 로그인 변경을 막지 않는다.
- `account NAME`은 named 로그인 확인·등록이며 기존 우선순위를 바꾸지 않는다.
  `default`는 codexm의 작업 기본값을 저장하지 않는다.
- 격리 fixture에서 두 명령 형식, 실패 시 원본 보존, 인증 링크/갱신, 우선순위
  보존을 시험한다. Go race/vet와 관련 공식 엔진 시험을 수행한다.
- commit/push 후 `.home` pin과 계정 사용법을 갱신해 로컬 설치만 반영한다.
  설치 과정에서는 실제 운영 계정 변경을 실행하지 않는다.

이미 진행 중인 GUI/터미널 프로세스의 메모리 인증은 강제 재시작하지 않는다.
새 native 실행이 해당 HOME에서 선택한 계정을 사용한다.

## 구현·검증

0.5.4에서 명시적 default를 native 파일 인증 링크 전환으로 연결했다. 기존 인증은
`~/.codex-accounts/.default-backups/`에 0700 디렉터리·0600 파일로 보관한다.
bare `account NAME`은 로그인 확인·등록이며 기존 순서를 바꾸지 않는다.
native 전환에는 supervisor 또는 공용 daemon RPC를 사용하지 않는다.

`go test -race ./...`, `go vet ./...`, UI 회귀 14개가 통과했다. 마지막 정책 검증
추가 뒤 관련 accounts/CLI race 검사를 다시 통과했다. 공식 CLI 0.162.1 및 앱 엔진
0.162.0-alpha.17.2에서 합성 인증의 native login status와 실제 파일 저장 경로를
검증했다. 두 엔진 모두 기본 링크를 유지하며 같은 named 파일을 갱신했다.
모델 turn·운영 로그인 변경·daemon/GUI 재시작은 실행하지 않았다.

명시적 default·두 dash 형식·로그인 실패·인수 오류·파일/keyring 정책·native workspace
강제 정책·외부 링크 거부·백업·우선순위 보존을 확인했다. 무료 모델 대체와 앞서 진행한
관리 모드 확장은 설치하지 않았다. 관련 미완료 변경은 private 검증 폴더에 보존했다.
검증 증거는 `~/.local/state/codexm/validation/native-default-20261013/`이다.
