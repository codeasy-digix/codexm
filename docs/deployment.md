# 소스·설치·장비 설정 관리

`codeasy-digix/codexm`이 제품 구현·테스트·문서·설치기의 단일 원본이다. 개발 체크아웃은
`/Users/ChangeMe/WebstormProjects/codexm`이다. 다른 장비에서는 원하는 위치에 clone한다.
`.home`, `codex-accounts` 저장소나 개인 alias 없이도 이 저장소만으로 빌드·설치한다.

| 구분 | 원본 / 역할 |
|---|---|
| 계정·작업·웹·플러그인 | 이 저장소의 `internal/`, `web/`, `plugin/` |
| 대화 relay | `internal/historysync/` 엔진, `internal/historysynccli/` CLI |
| 셸 통합 | `internal/shellinit/codexm.zsh`; `codexm shell-init zsh`로 출력 |
| 설치·배포 준비 | `install.sh`, `build.sh`, `cmd/package/` |
| 서비스 생성·관리 | `internal/service/`; 사용자 launchd / systemd |
| 선택적 수동 진단 | `tools/diagnostics/codex_state_audit.py`; Python이 필요한 개발자 도구로 기본 제품에 미포함 |
| 개인 장비 배치 | `~/.home`의 revision 핀·얇은 설치 호출·셸 로더·장비별 SSH/relay 설정·기존 시스템 서비스 선언 |

`.home`에는 제품 구현, 복제한 Go 실행 파일, 계정 helper나 별도 최신 사용 설명서를
두지 않는다. 개인 환경 관리자는 제품 설치기를 호출하고 이 문서를 연결한다.
공식 `codex` 실행 파일, 업데이트와 앱 번들은 OpenAI가 소유한다.

## 실행 경로와 데이터

| 경로 | 관리 주체 / 처리 |
|---|---|
| `~/.local/bin/codexm` | 제품 설치기가 만드는 링크 |
| `~/.local/share/codexm/bin/codexm` | 선택한 immutable 버전으로 원자적 전환 |
| `~/.local/share/codexm/versions/…` | Go 실행 파일·선택적 웹 번들. 실행 중인 이전 버전 보존 |
| `~/.config/codexm/config.toml` | 사용자별 계정 정책·관리 설정; Git 밖 |
| `~/.config/codexm/history-sync.json` | 선택적 relay 설정; `.home` 사용자는 `hosts/<profile>/history-sync.json` 파일 링크 |
| `~/.config/codexm/shell.zsh` | `.home` 사용자의 얇은 로더; 함수 구현은 제품에만 존재 |
| `~/.local/state/codexm/` | 관리 DB·로그·서비스 영수증·마이그레이션 백업 |
| `~/.codex/` | 공식 Codex 대화·공통 설정·기본 인증 연결. 이동·삭제하지 않음 |
| `~/.codex-accounts/<name>/` | 기존 계정별 실제 인증 데이터. 이름은 유지하되 옛 제품 의존성은 아님 |
| `~/.codex-history-sync/v2/` | 기존 relay 저장소·pending·충돌 원장·로그. 이번 통합에서 이동하지 않음 |
| `~/.codex-sync/` | 퇴역한 Python 아카이브의 기존 데이터. 자동 가져오기·삭제하지 않음 |

XDG 변수는 지원되는 설정·설치·상태 경로에 적용한다. 인증·대화·로그·캐시·바이너리는
소스 Git에 넣지 않는다. 여러 계정은 머신의 공통 설정을 쓰며, 인증 전환으로 다른
계정의 캐시·GUI 설정이나 계정 서버의 환경설정을 복사하지 않는다.

## 독립 설치와 업데이트

```sh
codex --version
./install.sh --build
source <(command codexm shell-init zsh)
codexm --version
codexm history-sync --help
```

빌드에는 Go가 필요하고, 빌드된 CLI에는 Python/Node/`.home`이 필요 없다. 공식 Codex와
SSH는 사용하는 기능의 외부 실행 파일이다. 선택적 Meteor 웹에는 Node가 필요하다.
셸 로더는 `.zshrc`에 위 `source` 한 줄로 등록할 수 있다. `codex`는 감싸지 않는다.

웹을 함께 설치한 장비에서 개발 빌드를 교체할 때는 검증한 웹 번들을
`--web /absolute/bundle`로 함께 지정한다. 설치는 심볼릭 링크만 바꾸며 기존 서비스·
worker·GUI를 강제 재시작하지 않는다. 서비스 실행 버전은 설치 버전과 별도로 확인한다.
관리 서비스 업데이트는 작업 상태를 확인한 뒤 기존 `codexm service upgrade` 경로를 사용한다.

`.home`은 `config/home/codexm-revision.txt`에 검증한 Git commit을 고정하고
`tools/codexm/{source,build,install,install-web}.sh`로 이 저장소의 도구를 호출한다.
제품 코드를 `.home/artifacts/`에 복제하거나 옛 계정 도구를 복구하지 않는다.
새 장비의 `.home` 복구는 현재 환경 관리자 소스로 `bootstrap.sh --build`를 사용한다.
과거 `home-tools-v1.0.5`의 해시는 역사 기록으로만 남아 있으며, 새 환경 전용 복구
명령을 모르는 구형 관리자는 적용을 거부한다. 이 변경은 새 Release를 게시하지 않는다.

## 기존 설치에서 제거한 것과 남긴 호환성

구형 `codex-accounts` 실행 파일·설치본, Python `codex_account.py`/`codex_sync.py`/
`codex_history.py`, `.utils` 연결, `.codex_rc`, `config/codex-accounts` 로더는 퇴역한다.
이전 소스는 Git 이력과 명시적 마이그레이션 백업으로 보존한다. Python 아카이브 서비스는
현재 Go relay와 다른 프로토콜이며 자동 변환하지 않는다. 기존 자료는 그대로 둔다.

기존 OS 시스템 서비스와 SSH peer는 `codex-history-sync`라는 실행 파일명을 사용한다.
이 이름만 필요한 장비는 기존 파일을 백업한 다음 다음 옵션으로 연결한다.

```sh
./install.sh --build --history-sync-compat  # 웹 사용자는 --web도 지정
```

이는 `~/.local/bin/codex-history-sync → codexm` 링크 하나를 추가한다. 같은 Go 바이너리가
`argv[0]`을 보고 `history-sync` 하위 명령으로 전달한다. 별도 account 패키지나 wrapper가
필요하지 않다. 기존 일반 파일은 덮어쓰지 않고 거부하며 실패한 설치는 이전 링크로 돌아간다.
새 설치·Brew/npm 준비물에는 이 호환 이름을 자동 설치하지 않는다.

기존 system launchd/systemd 서비스 선언은 `.home`의 운영 설정으로 유지할 수 있다.
제품의 새 사용자 서비스와 중복 기동하지 않으며, 시스템 서비스가 남아 있으면 새 서비스
설치·시작을 거부한다. 실행 중인 기존 프로세스는 이전 바이너리로 계속 실행될 수 있다.
소스 통합은 sync 성공이나 새 서비스로의 전환을 의미하지 않는다.

## 변경 규칙

제품 기능·CLI·데이터 보호·설치 방법은 여기서 구현하고 검증한다. `.home`에는 검증한
commit과 장비별 선언만 반영한다. 공용 지침은 [AGENTS.md](../AGENTS.md)를 따른다.
계정·대화 원본은 정리 대상에서 제외하고, 소스·엔트리포인트 정리 전에 백업한다.
현재 신규 설치·검증 범위는 로컬 Mac이며 원격 두 장비와 공개 배포는 별도 적용이다.
