> Historical record imported from `.home` at `896d075` on 2026-11-01.
> Current ownership and commands: [deployment](../../deployment.md), [history relay](../../history-sync.md).

> 이전 이동 기록입니다. 현재 native 계정·터미널 선택은 [계정 명령](codex-accounts.md)을 따릅니다.

# codexm 독립 제품 이동 점검 (2026-10-12)

`.home`의 `73ba838:tools/codexm`을 subtree split하여 원본 이력을 보존했다.
원본 구성 파일 49개는 모두 독립 저장소에 있으며 Go 기능을 재작성하면서 삭제한 것이 아니다.
기존 public CLI/TUI 관리 명령도 유지하고 account/daemon 관리 진입점을 추가했다.

| 항목 | 보존·변경과 검증 |
| --- | --- |
| 일반 채팅·단발 작업·선택적 goal | 기존 Job 종류·API·상태 전이와 테스트 보존 |
| 관리자 최초 선택·저장·재사용 | 기존 설정 스키마 1, manager 계정·호출 제한 보존 |
| 계정 우선순위·account-backup 조건 | account-work/account-secondary/account-main 우선, fallback은 전체 소진 시, 600초 복귀 점검 테스트 보존 |
| 사건·로그·재시작 복구 | SQLite 스키마와 incident/action 큐·멱등 처리 보존 |
| 실행 구간·계정 변경·사용량 | 전체/선택 로그, 읽기 쉬운 usage, 계정 전체 % 표시와 테스트 보존 |
| 새/기존 대화와 공식 화면 | 동일 thread ID, 이름과 빈 대화 rollout 저장, 별도 Terminal/tmux 인계 보존 |
| 목표 상태·예산·모델 설정 | 실제 공식 CLI 격리 시험으로 계정 교체와 완료/중지 목표 열람 시 유지 확인 |
| 대화 공유·동시 실행 방지 | 첫 계정에서도 shared sessions/locks/config 연결, 충돌 파일은 보존하고 실패 |
| 기본 계정 선택 | codexm 설정만 저장. 기존 작업·manager·공식 기본 로그인·부모 셸 유지 테스트 |
| 빠른 화면 열기 | 최근 60초의 검증된 primary quota만 재사용, 뷰 준비 확인 0.2초 간격 |
| 입력 포커스·↑↓ | 입력 초점과 작성 중 내용 유지하면서 목록 선택, slash 메뉴 방향키 우선 테스트 |
| 초기 줄바꿈 | 실제 PTY의 큐에 LF를 넣은 격리 시험: 초기 LF 제거, 실행 후 새 입력 보존 |
| 공식 공용 데몬 | 강제 종료한 격리 PID 복구 15.27초, 수동 stop 유지/start 재개, 모델 호출 0 |
| 공식 npm 설치 | Node 런처 대신 nested/hoisted/vendored native optional package 탐색 및 실제 npm 설치 0.160.0의 protocol/hash 보관 검사 |
| 공식 codex 독립 실행 | account helper 실패에도 원래 인자·출력·종료 코드 유지하는 zsh 회귀 시험 |
| Codex 자동 업데이트 | 엔진 receipt·hash·protocol 검증과 기존 작업별 엔진 유지, 명시적 refresh 보존 |
| 도움말·표기 | 현재 사용자 안내는 codexm account, legacy 표기는 이전 검증·이동 설명에서만 사용 |
| .home 복구 | 제품 source Git commit 고정, 독립 설치기 호출, account 재설치/복사 경로 제거 |
| 다른 작업자의 변경 | 40c495a Discord source/CLI/캡슐·데몬 검증 기록을 통합. 별도 history-sync 유지 |
| 배포 | account 태그 자동 release·공식 생성기 은퇴. codexm 4플랫폼 archive/hash/Brew/npm 로컬 준비 |

검사: `go test -race ./...`, `go vet ./...`, opt-in 공식 CLI worker/writer-lock
integration (0.162.0), 공용 daemon 복구 (0.162.1), npm 엔진 보관 (0.160.0),
account/login conflict·cancel/default 보존, native shell 회귀 검사.
단순 PTY 입력 시험의 연결 완료 시간은 1.45초였으며 실제 긴 대화·네트워크의 전체
열기 시간에 대한 보장은 아니다. 다른 세 플랫폼은 cross compile이며 실장비 실행은 하지 않았다.

원본 detailed design은 독립 저장소의 `docs/manager.md`, 이전 0.1.x 검증·인계·TUI 기록은
`docs/codexm-*-validation.md`에 남겼다. 기존 root 설정·인증·대화·실행 중인 작업과
설치된 0.1.2는 보존했다. 검증은 별도 설정·작업·tmux에서 수행하고 모두 정리했다.

이번 작업은 소스 통합과 배포 준비이다. Release/tag/npm/tap 게시, 기존 공개 artifact
삭제나 다른 장비 적용은 하지 않는다. npm manifest는 `private: true`, CI는 tests만 있다.
원래 `codex account`의 부모 셸 변경/장비 기본 로그인 교체는 새 제품으로 가져오지
않는다. 공식 CLI 소유권을 유지하라는 사용자 지시에 따라 선택은 manager 내부로 한정한다.

## 후속 로컬 설치 검증

앞선 배포 준비 후 `8703889`의 소스를 실제 GitHub에서 복구해 이 Mac에
`codexm 0.2.0`을 설치했다. 0.1.2 관리자에 진행 중인 턴과 연결 화면이 없는 것을
확인한 뒤 정상 종료하고, 새 관리자를 동일한 설정과 DB로 시작했다.

작업 5개의 ID·계정·초기 계정·목표 전체·예산·사용량·모델·추론 설정·엔진을
백업과 비교해 유지한 것을 확인했다. 기존 실행 10개, 계정 변경 5개와 사건 906개가
남아 있으며 관리자 모델 호출은 0회다. 공용 데몬 PID를 유지하면서 감시만
`codexm-daemon-watchdog`로 넘겼다. 설치 전 DB 백업은 Git 밖의 비공개 상태 폴더에 있다.

추가 점검에서 tmux 종료 뒤 writer lock이 잠깐 남아 계정 전환을 막는 경우를
보완했다. 공식 0.162.1 worker 전환·완료/중지 목표 열람 시험을 세 번 통과했고,
최종 실제 PTY의 초기 입력 줄 검사는 1.06초였다. Terminal 앱의 창 생성 시간은
이 측정에 포함되지 않는다. 첫 셸 확장에서 제공했던 외부 보관 기능은 같은
어댑터의 독립 `codex-history` 명령으로 유지한다. 공식 Codex에는 확장을 추가하지 않는다.

Discord 알림 source·CLI·캡슐 지시문을 포함한 회귀 검사와 기존 history safety
검사를 통과했다. 로컬 패키지의 macOS ARM64 파일은 설치 파일과 SHA-256이 같고,
나머지 세 플랫폼은 컴파일·압축 파일 검사만 했다. 배포 게시와 다른 장비 적용은 하지 않았다.
