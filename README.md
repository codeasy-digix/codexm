# CodexM

로컬 Codex 채팅을 찾아 계정을 고르고 **공식 Codex CLI로 이어가는** 작은 도구입니다.
채팅 실행·모델 호출·권한 승인·목표 진행은 공식 CLI가 담당합니다. codexm은 별도
작업 관리자나 자동 계정 교체 서비스를 시작하지 않습니다. 공식 `codex` 명령과 업데이트는
수정하지 않습니다.

macOS 메뉴 앱도 같은 계정·인증·기본 계정을 사용합니다. 메뉴에서 사용량 조회,
계정 추가/재로그인/등록 해제, 기본 지정과 터미널 TUI 열기를 할 수 있습니다.
기존 Codex Usage 앱의 언어·표시 이름·순서·설정은 일회성 가져오기로 보존합니다.
[메뉴 앱과 마이그레이션](apps/macos/README.md) · [공통 계정 구조](docs/desktop-integration.md)

지원: **support@digix.kr** · 소스: https://github.com/codeasy-digix/codexm

## 기본 흐름

```sh
codexm                              # Rust TUI → 채팅 선택 → Enter → 공식 CLI
codexm list                         # 모든 로컬 프로젝트의 채팅 목록
codexm list --archived --search 검색어
codexm list --json
codexm resume THREAD_ID --account ACCOUNT_NAME
codexm resume THREAD_ID --account ACCOUNT_NAME --continue
codexm handoff THREAD_ID --account NEXT_ACCOUNT --if-low 5
codexm handoff-status THREAD_ID
codexm usage --all
```

| 키 | 동작 |
|---|---|
| ↑ / ↓, PageUp / PageDown | 채팅 선택 |
| Enter | 직전 등록 계정으로 CLI 재개 또는 기존 터미널 재연결. 기록이 없으면 `default` |
| → | 오른쪽 상세 패널에서 채팅 정보 확인·이번 실행 계정 선택 |
| 상세 패널에서 → 한 번 더 | 오른쪽 pane의 채팅 관리: 보관·보관 해제 |
| 채팅 관리에서 ↑↓ / Enter / ← | 메뉴 선택 / 확인 화면 / 계정 선택으로 돌아가기 |
| 상세 패널의 Enter / ← | 계정 선택을 적용하고 목록으로 / 적용 없이 목록으로 |
| F2 | 전역 설정·전체 계정 관리. Mac 키보드 설정에 따라 Fn+F2 |
| / | 제목·프로젝트·ID·이전 계정 검색. 검색 중 Enter는 검색만 완료 |
| A / R / Q | 보관 목록 포함 전환 / 새로고침 / 종료 |

보관·보관 해제는 선택한 채팅 ID를 표시하는 확인 화면을 거칩니다. 기본값은 취소입니다.
처리 후 목록을 갱신하며, 보관된 대화는 `A`로 다시 찾을 수 있습니다. 공식 Codex의
보관/복원 명령을 사용하고, 다른 GUI/CLI가 소유해 처리할 수 없으면 오류를 표시합니다.
그 창을 자동 종료하거나 대화 DB를 직접 수정하지 않습니다.

이전 계정은 과거 관리 기록과 codexm의 마지막 실행 요청에서 확인한 정보입니다.
다른 프로그램에서 실행한 계정은 알 수 없을 수 있습니다. 상세 패널의 계정 선택은
이번 TUI 실행에만 적용하고, 실제 재개한 계정은 다음 실행의 이전 계정으로 남깁니다.
자동 새로고침은 선택한 채팅과 계정을 유지합니다. 자동 실행·자동 계정 교체는 하지 않습니다.

실행 중인 **codexm 소유 CLI의 명시적인 계정 교체**에는 `handoff`를 사용합니다.
기존 CLI 종료와 잠금 해제 후 같은 ID를 새 계정으로 재개하며, 별도 요약·fork 없이
계속 진행하는 프롬프트를 한 번 전달합니다. `--if-low 5`는 현재 계정의 5h 또는
주간 잔여량이 5% 이하일 때만 요청합니다. 대상 계정은 두 한도가 모두 5% 초과여야
합니다. 상주 감시기가 아니므로 작업 구간마다 요청해야 합니다.
[명령·실행 범위·에이전트 지침](docs/account-handoff.md)을 참고하세요.

같은 대화·계정의 codexm CLI가 tmux에 이미 있으면 보관/복원 없이 그 터미널로
재연결합니다. 저장된 기록뿐 아니라 실제 pane·Codex 프로세스·해당 대화의 writer 파일을
열고 있는 PID까지 확인합니다. 다른 계정을 선택하면 기존 계정을 조용히 재사용하지 않고
안내합니다. 기존 CLI를 `/exit`로 닫은 뒤 새 계정으로 재개하세요. `default` 터미널에
재연결할 때도 실행 중 인증이 유지되며, 이후 바꾼 기본 인증으로 교체되는 것은 아닙니다.
한 CLI가 여러 대화를 열어둔 경우에는 현재 화면을 확정할 수 없어 명시적인 연결 명령을 안내합니다.

재사용할 터미널이 없으면 계정별 `CODEX_HOME`과 같은 머신의 공용 대화 저장소를 준비하고,
`codex --no-daemon --no-alt-screen resume ID`로
프로세스를 교체합니다. 이후 입출력·종료 코드·작업 처리는 공식 CLI 그대로입니다.
잠금이 없는 일반 채팅은 보관/복원을 생략합니다. 이미 보관된 채팅에는 `unarchive`만 실행합니다.
공식 공용 데몬이 선택한 채팅을 소유한 경우에는 그 데몬의 `thread/archive`로 진행 중인
작업을 중단하고, 잠금 해제를 확인한 뒤 복원·재개합니다. 데몬을 새로 시작하거나
기존 데몬의 로그인 정보를 바꾸지 않습니다. 새 CLI에는 선택한 계정을 적용합니다.

macOS에서 검증된 Codex GUI가 잠금을 갖고 있으면 **매 실행마다 터미널에서 확인**합니다.
`y`를 선택하면 GUI 전체에 종료 요청(SIGTERM)을 보내고, 선택한 대화가 CLI에서 잠금을
받은 뒤 GUI를 다시 엽니다. **다른 GUI 대화의 실행 중인 작업도 중단되며 자동 재개되지
않습니다.** 기본값은 취소이며 승인 저장·자동 승인 옵션은 없습니다.
입력을 받을 수 없는 웹/분리 실행에서는 GUI를 종료하지 않습니다. 다른 독립 CLI는 직접
종료하거나 원래 소유자에서 보관해야 합니다. 자세한 절차는 [GUI 인계와 tmux](docs/gui-handoff.md)입니다.

잠금 파일 삭제·DB 직접 수정·SIGKILL·새 ID fork는 하지 않습니다. 불명확한 보관 RPC 실패를
GUI 종료로 우회하지 않습니다. 보관 해제 실패 시에는 원래 ID로 복원합니다.
GUI 공용 데몬 모드나 계정·모델 설정은 변경하지 않습니다.

## 계정

TUI에서는 **F2 → 계정 관리**로 들어갑니다. `A` 추가, `L` 재로그인, `D` 기본 지정,
`X`/Delete 등록 해제, `U` 계정 사용량 조회를 사용합니다. 인증은 기기 코드/브라우저를
선택합니다. 기본 지정과 등록 해제는 확인 화면을 거치며, 조회 중에도 화면을 이동할 수
있습니다. 로그인 취소·종료는 소유한 임시 인증 프로세스만 정리합니다.

```sh
source <(command codexm shell-init zsh)
codexm account                          # 현재 터미널과 기본 로그인 정보
codexm account --list
codexm account ACCOUNT_NAME             # 현재 터미널의 계정
codexm account ACCOUNT_NAME --login --browser
codexm account ACCOUNT_NAME --login --device
codexm account ACCOUNT_NAME --set-default # 새 터미널·다음 GUI 실행의 기본 인증
codexm account ACCOUNT_NAME --remove     # 목록 등록만 해제, 인증·대화 보존
```

이 기능은 유지합니다. 같은 머신에서 대화·공통 설정을 공유하고 로그인만 구분합니다.
계정 간 모델 캐시·GUI 상태나 서버 설정을 복사하지 않습니다. 실행 중인 GUI/CLI의
인증을 강제로 바꾸지 않으며 기본 인증을 바꾼 GUI는 재실행해야 합니다.
등록 해제는 과거 작업 기록을 변경하거나 다른 계정으로 작업을 자동 재배정하지 않습니다.

## 선택적인 이전 웹 경로

현재 기본 인터페이스는 TUI이며 웹 서버·Node·tmux 없이 실행합니다. 웹 소스와 명시적
`web` 명령은 호환 목적으로 남겨두지만 기본 설치에는 웹 번들을 넣지 않습니다.
기존 웹에서 시작한 CLI는 웹을 중지해도 계속 실행됩니다.

```sh
codexm web
codexm web status
codexm web stop
codexm web logs
```

웹은 채팅/계정 두 화면으로 구성합니다. 채팅과 계정을 선택하면
`codexm-cli-<id>`라는 tmux 세션에 공식 CLI를 엽니다. 웹에는 CLI가 출력한 기록과
현재 화면을 표시하고, 입력은 그 터미널로 전달합니다. 별도 app-server worker나
자체 답변 렌더링·목표 관리 엔진을 만들지 않습니다. 기본 키·슬래시 명령을 전달할 수
있으며 고급 터미널 조작은 화면에 표시한 `tmux -L codex attach -t ...`로 접속합니다.

계정 화면에서 추가·재로그인(기기 코드/브라우저)·기본 지정·등록 해제를 할 수 있습니다.
등록 해제는 인증 파일과 대화, 실행 중인 작업을 보존합니다. 로그인은 임시 인증 전용
프로세스에서 확인한 뒤 저장합니다. 웹 로그아웃은 해당 브라우저의 진행 중 로그인을
취소하며, 이미 실행 중인 채팅 CLI는 유지합니다. 원격 브라우저에서는 기기 코드를 사용합니다.

웹을 닫거나 다시 열어도 CLI는 유지됩니다. 같은 채팅/계정으로 다시 열면 기존
터미널을 연결합니다. CLI 종료는 `/exit` 또는 명시적인 ‘터미널 종료’를 사용합니다.
한 채팅을 여러 CLI/GUI에서 동시에 작업시키지 마세요. 전송 응답이 끊긴 경우 입력을
자동 재전송하지 않고, 터미널에서 결과를 확인하도록 표시합니다.

직접 tmux 터미널을 여는 명령도 제공합니다.

```sh
codexm resume THREAD_ID --account NAME --terminal
codexm terminal codexm-cli-SESSION_ID
```

웹/별도 터미널 실행에는 tmux가 필요합니다. 현재 터미널에서 바로 재개하는 CLI에는
tmux가 필요 없습니다. GUI 재시작 복구와 기존 tmux 연결에는 tmux가 필요합니다. Meteor는 loopback에서 실행합니다. 외부 접근은 별도 웹 로그인과
HTTPS 프록시를 명시적으로 설정한 경우에만 사용합니다.

기존 tmux CLI의 자동 재연결 검사에는 `lsof`가 필요합니다(macOS 기본 제공).
없는 환경에서는 자동 판정 대신 `codexm terminal SESSION`으로 명시적으로 연결합니다.

```sh
codexm web user --username YOUR_USERNAME  # 비밀번호를 숨겨서 입력; Codex 로그인과 별개
codexm web --public-origin https://YOUR_HOST --no-open
```

HTTPS 프록시 구성·재시작·중지 방법은 [외부 접속 안내](docs/public-web.md)를 따릅니다.

## 설치와 범위

공식 Codex CLI를 먼저 설치하고 `codex --version`을 확인합니다.

```sh
brew install codeasy-digix/tap/codexm
codexm
# macOS 메뉴 앱
open "$(brew --prefix codexm)/CodexM.app"

# Homebrew 대신 소스에서 설치할 때
cd /path/to/codexm
./install.sh --build
codexm doctor
```

소스·빌드·설치·업데이트는 이 제품 저장소에서만 관리합니다. `~/.home`은 이미 설치한
명령의 등록과 개인 셸 로더만 관리합니다. 소스 빌드에는 Go와 Rust가 필요하고,
설치 후 TUI/CLI에는 Python·Node·컴파일러가 필요 없습니다. 선택적인 이전 Meteor
웹에는 별도 번들과 Node 24가 필요합니다. Homebrew는 공개된 검증 소스로 CLI/TUI를
빌드하고 macOS에서는 메뉴 앱도 함께 설치합니다. 앱은 로컬 ad-hoc 서명이며 Apple
공증된 Cask는 아닙니다. [배포 방식](docs/distribution.md)을 참고하세요.
설치·업데이트는 명령을 실행한 장비에만 적용하며 다른 장비를 자동 변경하지 않습니다.

관리자 모델, 자동 worker/계정 교체, 스케줄링, 목표/예산 관리, watchdog, 자동 알림,
이전 관리 플러그인과 history-sync는 새 공개 실행 경로에서 제외했습니다.
기존 관리 DB는 이전 계정 표시용으로만 읽습니다. 인증·대화·기존 목표/예산은 삭제하거나
변환하지 않습니다. 이전 관리 구현/검증 문서는 과거 자료이며 현재 사용법이 아닙니다.

[실행 설계와 검증](docs/native-cli-core.md) · [설치 소유권](docs/deployment.md) ·
[웹 사용 안내](docs/local-web.md) · [동기화 제거](docs/history-sync.md)

네이티브 모듈의 신규 개발은 [Rust 개발 기준](docs/rust.md)을 따릅니다. 기존 Python
메타데이터 진단 도구는 `crates/state-audit`로 대체했습니다. TUI는 `crates/terminal`의
Rust이며 계정·목록 조회·공식 CLI 인계는 기존 Go 코드를 재사용합니다. 웹은 TypeScript를
유지합니다. Go 전체를 Rust로 전환했다거나 전체 실행이 빨라졌다고 간주하지 않습니다.
[TUI 구조와 로컬 검증](docs/terminal-ui.md)을 참고하세요.
