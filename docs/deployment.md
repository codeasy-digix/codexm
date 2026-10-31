# 제품 설치와 개인 환경의 경계

`codeasy-digix/codexm`이 제품 소스·테스트·문서·빌드·설치·업데이트의 유일한 원본이다.
현재 개발 디렉터리는 `~/WebstormProjects/codexm`이다. `.home` 없이 빌드·실행한다.
`~/.home`은 이미 설치한 도구를 개인 셸에서 사용하기 위한 목록·설정만 관리한다.

| 항목 | 담당 |
|---|---|
| 채팅 목록·계정·공식 CLI 인계·웹 | codexm 소스의 `internal/cli/compact.go`, `internal/nativecli/`, `web/` |
| 빌드·설치·업데이트 | codexm 소스의 `build.sh`, `build-web.sh`, `install.sh` |
| CLI 셸 함수 | 제품의 `internal/shellinit/`; `codexm shell-init zsh`로 출력 |
| 이미 설치된 명령과 의존성 목록 | `.home/manifests/home.json`의 외부 도구 등록 |
| 개인 zsh 로드 설정 | `.home/config/codexm/shell.zsh`; 제품 함수를 로드하는 짧은 설정 |
| 실행 생명주기 | 공식 CLI 직접 실행; 웹과 별도 CLI는 이름 있는 tmux 세션 |
| 장비 간 history-sync | 제거. 코드·서비스·활성 환경 등록 없음 |

`.home`에는 codexm source clone, revision pin, build/install/update 호출, 제품
실행 파일의 별도 빌드/복사본을 두지 않는다. `homectl apply`, `components`, bootstrap은
codexm을 설치하거나 갱신하지 않는다. 제품을 먼저 독립 설치한 뒤 셸 설정을 적용한다.
개인 환경 등록이 없어도 제품은 동일하게 동작한다.

## 독립 로컬 설치

```sh
cd /path/to/codexm
codex --version
./install.sh --build
source <(command codexm shell-init zsh)
codexm --version
```

소스 빌드에는 Go가 필요하다. 빌드한 CLI 실행에는 Python/Node/`.home`이 필요 없다.
공식 Codex는 별도 선행 도구다. 선택적 Meteor 웹의 빌드·실행에는 해당 Node가 필요하다.
웹을 사용 중인 로컬 설치를 갱신할 때는 검증한 번들을 `--web /absolute/bundle`로 함께
지정한다. 제품 설치기는 공식 `codex` 파일·업데이트·GUI 번들을 변경하지 않는다.

| 경로 | 내용 |
|---|---|
| `~/.local/bin/codexm` | 제품 설치기가 만드는 실행 링크 |
| `~/.local/share/codexm/versions/…` | immutable Go 실행 파일·선택적 웹 번들 |
| `~/.local/share/codexm/bin/codexm` | 선택된 설치 버전 링크 |
| `~/.config/codexm/config.toml` | 로컬 경로·계정 등록 설정, Git 밖; 이전 실행 정책은 읽지 않음 |
| `~/.config/codexm/shell.zsh` | 개인 환경에서 사용하는 선택적 셸 로더 |
| `~/.local/state/codexm/` | native-cli 실행 영수증·웹 상태·과거 관리 DB·private 백업 |
| `~/.codex/` | 공식 Codex 대화·공통 설정·기본 인증 연결 |
| `~/.codex-accounts/<name>/` | 계정별 실제 인증; 역사적인 경로 이름을 유지 |
| `~/.codex-history-sync/`, `~/.codex-sync/` | 제거한 기능의 기존 보관 자료; 자동 적용하지 않음 |

인증·대화·캐시·바이너리는 소스 Git에 넣지 않는다. 계정 선택은 같은 장비의 인증만
선택하고 다른 계정의 GUI/서버 설정을 복사하지 않는다. 실행 중 worker와 GUI를
설치 과정에서 강제 재시작하지 않는다. 서비스 실행 버전은 새 CLI 설치와 별도로 확인한다.

## 과거 설치 정리

구형 account 실행 파일·Python archive/sync helper·`.codex_rc`는 퇴역했다. 실제 인증
폴더와 대화 DB는 정리 대상이 아니다. 예전 `~/Codex/codexm`은 현재 개발 원본에 대한
경로 호환 링크로 두어 저장된 작업 디렉터리가 사라지지 않게 했다.

`.home/tools/codexm/`, `config/home/codexm-revision.txt`, bootstrap의 제품 호출은
제거했다. `.home`은 source 다운로드 캐시나 별도 제품 배포 주체가 아니다.
유일한 설치 절차는 제품 저장소의 설치기이며, 향후 배포도 이 저장소에서 준비한다.
**현재 Homebrew 등 배포 작업은 보류**한다. 이번 작업에는 소스 정리와 로컬 설치만
포함하며 다른 장비에 codexm을 설치/업데이트하지 않는다. 기존 history-sync 서비스의
중지·자동 시작 해제는 제품 신규 설치와 구분하여 세 장비에서 수행한다.

과거 통합 중간 단계는 [검증 기록](consolidation-20261101.md)에 남겼다. 그 문서의
relay 통합·`.home` pin/build 설명은 현재 정책이 아니다. 제거한 기능의 데이터 경계는
[history-sync 제거](history-sync.md)를 따른다.

소스 분리·로컬 설치·서비스 제거의 실제 확인 결과는
[2026-11-01 검증 기록](retirement-20261101.md)에 남긴다.

현재 기능은 [공식 CLI 인계 구조](native-cli-core.md)다. 이전 supervisor/watchdog와
관리 플러그인은 현재 설치의 구성 요소가 아니며 새 CLI에서 생성하거나 시작하지 않는다.
과거 작업 DB와 설정 백업은 유지한다. 서비스는 실행 중인 작업이 없는 것을 확인한 뒤
현재 로컬에서만 해제하고, 공식 Codex와 GUI는 재시작하지 않는다.
