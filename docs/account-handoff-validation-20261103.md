# 계정 교체 검증과 로컬 적용

2026-11-03. 로컬 Mac에만 적용했다. 원격 장비 설치·공개 배포·history-sync는 하지 않았다.
동작과 명령은 [같은 채팅의 계정 교체](account-handoff.md)를 기준으로 한다.

## 구현 범위

- `resume ID --account NAME --continue`: 공식 CLI에 계속 진행 지시를 한 번 전달.
- `handoff ID --account NAME [--if-low 5]`: codexm이 시작한 CLI의 명시적인 교체.
- `usage`, `handoff-status`: 실제 사용량과 접수/진행/실패 결과를 JSON으로 확인.
- helper는 `tmux -L codex`에서만 잠시 실행하며, 이전 프로세스와 writer의 해제를
  모두 확인한 뒤 같은 채팅 ID를 새 계정으로 실행한다.
- 기존 계정/인계/프로세스 식별 검증이 있는 Go nativecli 경로를 재사용했다.
  Rust TUI 소스·모델 관리자·스케줄러·상주 프로세스는 추가하거나 변경하지 않았다.
- 전역 개인 지침에는 codexm 실행 표식이 있는 CLI에서만 작업 구간마다 실제 한도를
  확인하고 5% 이하이면 명시적인 교체 명령을 요청하도록 추가했다.

## 실제 실행한 검사

| 검사 | 결과 |
|---|---|
| 전체 `go test -race ./...`, `go vet ./...` | 통과 |
| 최종 소유권 보강 후 nativecli/cli/accounts/runtime race 및 전체 vet | 통과 |
| 합성 native 프로세스가 자기 실행 중 handoff 명령 호출 | 종료/잠금 해제/같은 ID의 새 계정 실행 순서 통과 |
| 같은 로그인 별칭, 오래된 PID 식별자, 대상 잔여 5%, 현재 잔여가 지정 기준 초과 | 종료 전 거절, 원래 writer 유지 |
| SIGTERM을 무시하는 합성 프로세스 | 제한 시간 후 failed, 강제 종료·새 CLI 시작 없음 |
| 실제 공식 CLI 0.166.0의 진행 중 요청 인계 | 임시 HOME·가짜 로그인·로컬 mock 모델에서 통과 |
| 공식 CLI 재개 후 기록/지시 확인 | 원래 요청 유지, 계정 식별 변경, 같은 ID, 새 계속 지시 한 번 전달 |
| 기존 공식 archive/unarchive 준비 검사 | 통과 |
| 실제 Go/Rust 쌍의 TUI PTY 테스트 | 탐색·계정·인계 및 종료 검사 통과 |
| Linux amd64 Go 교차 빌드 | 통과; Linux 실제 프로세스 인계는 이 Mac에서 검증하지 않음 |
| 제품 설치기의 `--from-build` 설치 | 통과, 두 실행 파일의 immutable 설치 |
| 설치 전후 native auth/config 및 codexm config 해시 | 동일 |
| 로컬 설치의 help/status와 공통 AGENTS 링크 plan | 새 명령 확인, 링크 ok |

실제 사용자의 채팅/GUI를 종료하거나 모델 작업을 전송하는 방식으로 검증하지 않았다.
사용량 RPC와 실제 계정 교체 결과를 합성 값으로 속여 운영에 적용하는 우회 옵션은 없다.
공식 CLI 테스트에서는 사전에 준비한 요청으로 수명주기를 검증해 OpenAI 인증/사용량
서버를 호출하지 않았으며, 요청 경로의 quota preflight는 별도 합성 RPC 검사로 검증했다.

## 설치와 운영 경계

`install.sh --from-build`로 로컬의 실행 링크를 새 설치에 연결했다. 로그인·모델 설정과
대화 데이터는 복사하거나 수정하지 않았다. `.home`에는 개인 에이전트 지침만 변경했다.
진행 중인 기존 CLI는 재시작하지 않았다. 이전 버전으로 시작되어 소유권 영수증이 없는
CLI에는 강제 인계를 시도하지 않으므로 새 버전으로 시작한 codexm 실행부터 적용된다.

일반 터미널의 CLI를 교체하면 원래 셸은 돌아오고 새 CLI는 전용 tmux에서 계속된다.
전용 tmux 안에서 시작한 codexm 세션은 새 계정의 터미널로 연결을 넘긴다. GUI는
실행별 foreground 확인 규칙을 유지하며 이 검증에서는 GUI 종료를 실행하지 않았다.

private 빌드·검사 로그·이전 설치 경로와 설정 해시는
`~/.local/state/codexm/validation/handoff-20261103/`에 보관했다.
임시 검증용 tmux 세션은 검사 종료 후 제거한다. 실패/불명확한 인계는 상태를 남기고
자동 재시도하지 않으며, `started`는 새 writer 확인이지 작업 완료라는 뜻이 아니다.
