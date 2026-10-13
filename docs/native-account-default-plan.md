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
