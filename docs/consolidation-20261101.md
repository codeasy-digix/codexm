> Superseded intermediate record: later on 2026-11-01 the user removed history-sync
> and all `.home` product build/install integration. Current policy is in
> [deployment](deployment.md) and [history-sync retirement](history-sync.md).

# 2026-11-01 제품 소스·설치 통합 검증

제품 구현 기준은 `c1de7d9`이다. 계정·작업 관리와 선택적 history relay가 한 저장소와
한 Go 바이너리에 들어간다. 설치·검증은 현재 Mac에만 적용했고 두 원격 장비는
경로·Git 상태 확인만 했다. `.home` 전체 bootstrap, 원격 설치·업데이트, 공개 배포는 없다.

## 적용

- 단일 개발 원본: `~/WebstormProjects/codexm`. 예전 `~/Codex/codexm`은 이 원본으로
  연결하는 경로 호환 링크다. 별도 clone과 8개 연결 worktree, 구형 account/tap 프로젝트는
  private 마이그레이션 보관 위치로 이동했고 Git 연결도 복구했다. 미병합 계획 커밋
  `46b4ced`는 제품의 `archive/pre-consolidation-account-policy-20261013` 브랜치에도 보존했다.
- 기존 standalone relay의 코드·테스트를 `internal/historysync`와 `internal/historysynccli`로
  이동했다. 기본 명령은 `codexm history-sync`; 기존 OS 서비스/SSH peer에 필요한
  `codex-history-sync`는 같은 바이너리를 가리키는 선택적 링크다.
- `.home`의 계정/Python archive/sync 구현·테스트·`.utils` 링크·`.codex_rc`와 중복 제품
  설명서를 정리했다. 선택적 메타데이터 진단 원본과 고유한 과거 검증 문서는 제품에 보관했다.
- `.home`은 제품 revision 핀·얇은 설치 호출·장비 설정·기존 시스템 서비스 선언만 갖는다.
  `homectl components`와 native build가 옛 제품 실행 파일을 복구/복제하던 경로를 제거했다.
  과거 home-tools artifact는 새 기능 복구본이 아니며 새 bootstrap은 현재 source build를 쓴다.
- 로컬 구형 Homebrew 패키지 3개 버전과 전용 tap을 제거했다. 계정별 인증 폴더,
  대화 DB, relay pending/원장과 이전 Python 아카이브 데이터는 그대로 남긴다.
- 95개 구형 실행/설정 경로와 11개 checkout/worktree 경로를 백업 보관했다.
  현재 Go CLI에는 Python이나 `.home` 런타임 의존성이 없다. 선택적 웹에는 Node가 필요하다.

설치한 실행 파일 SHA-256:
`eaa8dfc0b66ce6bd9696459d633e4b8d9f41800178ce0422f9b8fe6aca6d6fb0`.
기존 웹 번들 digest `c17c6033830c3facaf201106fb2aac831ae56c4cb38158b27e7dc5402b7b80d6`를
같이 설치했다. 실행 중인 supervisor PID 719, watchdog PID 721, relay PID 505는
유지했다. 새 CLI 설치와 이미 실행 중인 서비스의 실행 파일 버전은 구분한다.

## 실제 수행한 검증

| 검증 | 결과 |
|---|---|
| 제품 `go test -race -p 4 ./...`, `go vet ./...` | 통과 |
| 환경 관리자 전체 race/vet | 통과 |
| 공식 CLI 0.166.0 격리 account/shared-settings integration 2건 | 통과; 실제 사용자 로그인 전환·모델 호출 없음 |
| installer 기존 바이너리 충돌·실패 시 링크 복구 | 통과 |
| history 설정 우선순위·잘못된 새 링크·중복 서비스 차단·JSON 상태 | 통과 |
| 새 서비스 정의 macOS/Linux 렌더링 | 통과; 실제 새 서비스는 설치/시작하지 않음 |
| zsh 구형 함수만 제거·사용자 함수·공식 CLI 인수/종료 코드 유지 | 통과 |
| 환경 restore에서 구형 Codex 도구 재생성 차단 | 통과 |
| 네 플랫폼 archive 빌드·SHA-256·단일 실행 파일 구성·공식 Ruby 구문 | 통과; Linux/Intel의 실장비 실행은 아님 |
| 격리 HOME, 잘못된 manager config, `.home` 없는 실제 패키지 실행 | help/relay SSH 오류 응답/compatibility/service JSON 통과 |
| 로컬 `homectl plan` | 추가 변경 0건 |
| 로컬 `homectl doctor` | 191/191 정상 |
| 공식 `codex --version` | 0.166.0 유지; PATH의 구형 account helper 없음 |

공통 Codex 설정 바이트와 기본 인증 symlink 대상은 변경되지 않았다. 활성 기본 계정의
인증 파일 내용은 실행 중 변화가 관측되어 전체 토큰 바이트 불변을 주장하지 않는다.
이전 토큰으로 되돌리지 않았다. 다른 세션의 SSH·에디터 미커밋 변경은 바이트 비교로
보존했고, 동시에 들어온 터미널 색상 설정 커밋도 병합해 유지했다.

history-sync의 `threads.attached_thread_parent_id` 지원 오류는 그대로다. 적용 직후
읽기 전용 상태 조회는 pending 616, upload/download/install 0, 같은 schema 오류를
보고했다. 이번 작업은 실제 동기화를 복구하거나 pending을 강제 적용하지 않는다.

## 백업과 복구

로컬 private 영수증/백업: `~/.local/state/codexm/migrations/20261101-consolidation/`.
`result.json`, `entrypoints-before.json`, `retired-paths.json`, `retired-checkouts.json`,
Git bundles, 과거 설치 파일과 Homebrew keg 복사본을 보관한다. 인증 원문은 새 문서나
Git에 복사하지 않았다. `.home` 링크 적용 영수증은 `20261031T162305.685944000Z`이며
정상적인 plan 이후 XDG 링크 2개만 적용했다.

실행 파일을 되돌릴 때는 `previous-install.json`의 기존 codexm 버전 링크와 웹 번들을
사용한다. 그 버전은 새 history-sync subcommand를 모를 수 있으므로 relay 호환 링크도
함께 이전 standalone 파일로 복구해야 한다. 소스·링크 복구와 서비스 재시작, DB 복구는
서로 다른 작업이다. 이 백업을 이유로 실행 중인 인증·대화 DB를 과거 사본으로 덮어쓰지 않는다.

향후 변경·배치 방법은 [배포 관리](deployment.md), relay 명령과 기존/신규 서비스의
구분은 [history-sync](history-sync.md)를 따른다.
