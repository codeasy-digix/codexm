# 소스 소유권 정리와 history-sync 제거 검증

2026-11-01 로컬 Mac 기준 기록이다. 제품 변경은 `2c2c1bc`, 개인 환경 변경은
`account-main/.home`의 `c72d8d5`에서 반영했다. 이전
[중간 통합 기록](consolidation-20261101.md)의 relay 통합·환경 저장소 pin/build 방식은
이 기록과 [설치 관리](deployment.md)로 대체한다.

## 적용한 경계

- 제품 소스·빌드·설치·업데이트·운영 문서는 `~/WebstormProjects/codexm`이 소유한다.
- `.home`에는 이미 설치된 명령의 등록과 개인 셸 로더만 남겼다. 제품 소스 다운로드,
  revision pin, 빌드·설치·업데이트 helper와 bootstrap 연결을 삭제했다.
- 로컬 CLI는 제품의 `./install.sh --build --web ...`로 설치했고 기존 웹 번들을
  보존했다. 다른 장비에 codexm을 설치하거나 업데이트하지 않았다.
- history-sync 엔진·프로토콜·서비스 생성·설치 옵션과 `.home` 활성 등록을 제거했다.
  구형 하위 명령과 실행 이름은 설정·DB를 열기 전에 명시적으로 실패한다.
- 과거 소스와 배포 초안은 private 복구 자료로 이동했다. `.home`이 내려받았던
  소스 사본 15개는 미커밋 변경과 사용 중인 작업 디렉터리가 없음을 확인했다.

## 실제 검증

| 검사 | 결과 |
|---|---|
| 제품 `go test -race -p 4 ./...`, `go vet ./...` | 통과 |
| 환경 관리 `go test -race ./internal/manage`, `go vet ./internal/manage` | 통과 |
| `python3 -B -m unittest discover -s tests -p 'test_codex*.py'` | 4개 테스트 통과 |
| bootstrap 일반/빌드 × plan/apply의 4개 경로 | 제품 helper·실행 파일 호출 없음 |
| 설치된 CLI의 구형 sync 명령·이름, 잘못된 설정을 둔 격리 HOME | 거부, native/config/state 파일 생성 없음 |
| `homectl plan`, `homectl doctor --json` | 변경 필요 0건, 186개 검사 중 실패 0건 |
| 새 로그인 zsh의 실행 명령 | `codex`: 공식 command, `codexm`: 제품 shell function |
| 공식 Codex | `codex-cli 0.166.0`, 실행 파일 미변경 |
| 공통 `config.toml`, 기본 `auth.json` 연결 대상 | 변경 없음 |
| 기존 SSH·에디터 미커밋 변경 | 백업과 동일, 미커밋 상태 유지 |

CLI 표기 버전은 `0.6.2`이며 새 소스 빌드는 바이너리 해시별 설치 경로로 구분한다.
기존 manager/watchdog·GUI·작업 프로세스는 강제 재시작하지 않았다. 인증 토큰은
실행 중인 공식 프로그램이 갱신할 수 있으므로 과거 토큰 바이트를 복원하지 않았다.
대화·archive DB의 내용을 변경하거나 동기화 데이터를 가져오지 않았다.

## 세 장비의 동기화 종료 상태

| 장비 | 확인한 상태 |
|---|---|
| Ubuntu `server` | 서비스 중지·정의 삭제·mask 완료, PID 0, inactive/dead |
| 로컬 Mac | relay 프로세스 없음, 공개 실행 파일 없음, config disabled; root LaunchDaemon 제거 대기 |
| `mac-secondary` | relay 프로세스 없음, 공개 실행 파일 없음, config disabled; root LaunchDaemon 제거 대기 |

두 Mac에서는 기존 root job이 실행 파일을 찾지 못해 `spawn scheduled` / exit 78로
남아 있었다. 이는 OS 서비스 제거 완료를 의미하지 않는다. 비대화형 sudo가 관리자
인증을 요구하므로 아래 두 명령의 사용자 실행을 요청했다. 스크립트는 해당 job만
bootout/disable하고 해당 plist만 삭제하며 대화 DB에 접근하지 않는다.

```sh
sudo ~/.local/state/codexm/migrations/20261101-remove-history-sync/remove-system-service.sh
ssh -t mac-secondary 'sudo ~/.local/state/codexm/migrations/20261101-remove-history-sync/remove-system-service.sh'
```

실행 후 각 Mac에서 `launchctl print system/kr.digix.codex-history-sync`가 job을
찾지 못하고, `/Library/LaunchDaemons/kr.digix.codex-history-sync.plist`가 없으며,
`launchctl print-disabled system`에 해당 label이 disabled로 표시되는지 확인한다.
이 확인 전에는 전체 서비스 제거를 완료했다고 보고하지 않는다.

개별 장비의 config와 실행 경로는 해제했지만 원격 `.home` 전체 적용이나 codexm
업데이트는 수행하지 않았다. 기존 동기화·인증·대화 저장소는 복구용으로 보존한다.

복구 영수증과 관리자 스크립트는 각 장비의
`~/.local/state/codexm/migrations/20261101-remove-history-sync/`에 있다.
이전 전체 정리 백업은 로컬 `20261101-consolidation/`에 남겼다. Git에 포함하지 않는다.

Homebrew 등 배포는 사용자 보류 지시 이후 작업하지 않았다. 보류 전 실행한 구형
로컬 패키지·tap 제거는 이전 기록에 남겼으며, 새 release/tag/npm/tap 게시나 원격
codexm 설치는 하지 않았다.
