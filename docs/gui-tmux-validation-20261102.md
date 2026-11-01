# 실행별 GUI 확인과 전용 tmux 적용 검증

2026-11-02. 사용자가 승인한 대상은 local M3 Air, `ssh mac-secondary`, `ssh mac-mobile`다.
현재 동작/명령은 [GUI 인계와 tmux 운영](gui-handoff.md)에 정리했다.
Homebrew·release/tag 공개 배포와 history-sync 재도입은 하지 않았다.

## 구현과 검증의 구분

- 잠금 없는 채팅의 직접 resume, 소유 공용 데몬의 선택 채팅 보관/복원은 유지한다.
- 별도 macOS GUI가 소유한 경우에만 서명·UID·PID 시작 시각·writer 파일·부모 관계를
  검증하고 매번 터미널 확인을 받는다. 기본값은 취소다. 다른 GUI 작업도 중단되며
  자동으로 계속되지 않는다는 문구를 확인창에 표시한다.
- 승인 후 SIGTERM, 잠금 해제 대기, 공식 CLI 인계, GUI 재실행을 구현했다.
  복구 도우미의 준비 응답 전에 GUI를 종료하지 않는다. 시작 실패·요청 프로세스 소멸·
  복구 기한 만료에는 종료된 GUI를 다시 연다. 독립 CLI와 공식 데몬은 종료하지 않는다.
- **이번 검증에서는 실제 사용 중인 GUI에 종료 신호를 보내지 않았다.** 전체 종료 후
  사용자 대화가 이어지는 성공 경로를 세 Mac에서 실사용 검증했다고 주장하지 않는다.
  이 작업은 매 실행마다 사용자의 확인을 요구하며, 다른 GUI 작업을 중단시킨다.

실행한 검사:

| 검사 | 결과 |
|---|---|
| codexm `go test -race ./...`, `go vet ./...` | 통과 |
| 추가 웹 소켓 변경 후 nativecli/webhost/cli race, 전체 vet | 통과 |
| `.home/tools/native` 전체 race/vet | 통과 |
| 실제 공식 CLI + 임시 HOME/mock model의 보관/복원 준비 검사 | 통과 |
| 실제 Rust TUI PTY의 탐색·계정 관리·CLI 인계·PTY 손실/SIGTERM 정리 | 통과 |
| 실제 전용 tmux에서 합성 대화 생성·문자 그대로 입력·종료 | 통과 |
| 새/기존 소켓의 조회·입력·연결·종료, 교차 서버 attach | 통과 |
| 사용자 GUI 소유자의 실제 PID/서명 읽기 전용 확인 | 통과 |
| 실제 사용자 GUI 대화의 확인창 표시 → N 취소 | GUI/CLI 유지, 종료/복구 helper 시작 없음 |
| 실제 복구 helper의 준비 응답 → 종료 요청 없는 취소 | 정상 종료, GUI/writer PID 유지 |
| 소유자 변경·종료 실패·잠금 유지·CLI 준비 실패·실행 프로세스 소멸 | 격리된 동작/상태 테스트 통과 |
| 전송된 빌드의 정상 설치·재설치·변조/누락된 TUI 거부 | 통과 |

일반 TUI 실행은 계속 공식 CLI로 직접 exec한다. 새 프로세스 관리자나 자동 재시작
서비스를 만들지 않는다. 세션 유지가 필요하면 `tmux -L codex new-session -s JOB` 안에서
codexm을 실행한다. 웹/`--terminal`의 CLI와 새 Discord 수신기도 전용 소켓을 사용한다.

## 세 Mac 적용 결과

| 항목 | local | mac-secondary | mac-mobile |
|---|---|---|---|
| Go/Rust 설치 쌍 | 동일 | 동일 | 동일 |
| 새 Discord 수신기 실행 파일 | 동일 | 동일 | 동일 |
| `~/.codex/AGENTS.md` | 공통 원본 링크 | 공통 원본 링크 생성 | 오래된 프로젝트 파일 링크 교체 |
| `homectl doctor` 실패 | 0 | 0 | 0 |
| 전역 지침의 `homectl plan` | ok | ok | ok |
| 기존 인증·config 파일 비교 | 11개 동일 | 12개 동일 | 2개 동일 |
| 이름별 계정의 공통 AGENTS 연결 | 4/4 | 4/4 | 이름별 계정 없음 |
| 임시 검증 세션 잔존 | 없음 | 없음 | 없음 |

각 장비의 공통 원본은 `~/.home/templates/codex/AGENTS.md`다. mac-mobile의 기존
`~/WebstormProjects/AGENTS.md`도 같은 원본으로 연결했다. 새 셸과 이름별 계정에서
지침이 갈라지지 않으며, 인증·모델/effort·대화 DB는 장비 간 복사하지 않았다.
앱 번들은 세 Mac 모두 기존 OpenAI 서명 검증을 통과했다.

최종 실행 파일 SHA-256:

- codexm: `d023f5eaa3420af8fff829f66cdb95bb187be2681f95b948ffae4d1fee2f0ab5`
- codexm-tui: `a7734e7381e5fd6826c6fdba02cee0f2b1153d35c4c951ef549489c20839474a`
- agent-discord: `[ChangeMe: local binary checksum]`

codexm 설치는 제품 설치기의 `--from-build`를 사용했다. `.home`은 제품을 빌드하지
않으며 자신의 Discord 도구만 관리한다. 두 원격 Mac의 기존 homectl/homeutil 파일은
그대로 보존한 새 artifact 디렉터리에 Discord 실행 파일만 갱신했다.

local의 기존 GUI/main writer, 공식 CLI, Discord 수신기의 PID와 시작 시각이 유지됐다.
mac-mobile의 carbonyl/DMZ 테스트 세션도 유지했다. 기존 default 서버의 실행 중인 세션은
강제로 옮기지 않았다. 종료된 codexm pane 5개만 화면·영수증을 백업한 뒤 정리했다.
새 수신기를 시작할 때만 codex 소켓을 사용하며 기존 default 수신기는 새 명령에서도
조회·중지할 수 있다. 동시에 두 서버에 수신기가 있으면 자동 선택하지 않는다.

private 검증·백업은 각 장비의 `~/.local/state/codexm/validation/gui-tmux-20261102/`다.
`instructions-backup/`, `installed-before.txt`, `baseline.json`/`after.json`,
`installed-after.json`에 이전 지침·설치 경로와 비교 증거를 남겼다.
local의 `closed-terminals/`는 종료된 pane의 기록이다. 대화 내용·인증·원본 로그는
Git에 넣지 않았다. 기존 설치 디렉터리도 삭제하지 않았다.
