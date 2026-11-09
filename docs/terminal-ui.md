# 터미널 중심 codexm

`codexm`은 Rust TUI를 연다. 채팅을 선택하고 Enter를 누르면 직전 등록 계정으로
공식 Codex CLI에 현재 터미널을 넘긴다. 직전 계정을 모르거나 등록 해제했다면
`default`를 사용한다. 별도 계정 질문을 거치지 않는다.

해당 대화의 일반 Codex CLI 또는 codexm CLI가 tmux에서 실행 중이면 새 archive/resume 대신
그 화면으로 연결한다. writer·프로세스·pane의 부모 관계를 확인한다. 선택 계정과 달라도
실행 중인 계정으로 연결하며 인증을 변경하지 않는다. 계정 변경은 명시적인 handoff다. 새 기본 인증 지정 후 예전 `default`
터미널에 연결하는 경우도 실행 중 인증을 유지한다. 새 인증은 그 CLI 종료 후 새 실행에 적용한다.

## 화면과 키

- 목록: ↑↓/PageUp/PageDown/Home/End 이동, `/` 검색, `A` 보관 포함, `R` 갱신, `Q` 종료.
  검색 중 Enter는 검색 종료만 한다. 한 번 더 Enter를 눌러야 재개한다.
- 목록·상세·채팅 관리에서는 `i`=↑, `j`=←, `k`=↓, `l`=→도 사용할 수 있다.
  검색·닉네임 입력에서는 그대로 문자로 입력하며, 계정 관리의 `L` 재로그인과
  확인 화면의 방향키·Tab 선택은 기존대로 동작한다.
- →: 오른쪽 상세 패널. ID·프로젝트·이전 계정·모델/effort를 확인하고 이번 실행 계정을
  선택한다. Enter는 선택을 적용하고 목록으로, ←/Esc는 적용하지 않고 목록으로 돌아간다.
  넓은 터미널은 두 패널, 좁은 터미널은 현재 패널 하나를 표시한다.
- 상세/계정 패널에서 → 한 번 더: 같은 오른쪽 pane의 **채팅 관리**. ↑↓로 보관하기 또는
  보관 해제를 고르고 Enter로 확인 화면을 연다. 확인의 기본값은 취소이며 ←→로 실행을
  고른 뒤 Enter로 요청한다. Esc는 취소, 관리 메뉴의 ←/Esc는 계정 패널로 돌아간다.
  계정 패널에서 고르던 값은 관리 메뉴 이동만으로 적용되지 않는다.
  보관/복원 후 목록으로 돌아와 갱신하며 보관된 대화는 `A`로 확인한다.
- F2: 전역 설정. 계정 관리 또는 기본 목록의 보관 포함 여부를 선택한다.
  Mac 기능 키 설정에 따라 Fn+F2가 필요하다.
- 계정 관리: `A` 추가, `L` 기기 코드/브라우저 재로그인, `D` 기본 지정,
  `X`/Delete 등록 해제, `U` 사용량 명시 조회. 기본 지정과 등록 해제는 확인 후 실행한다.
- 로그인: `O` 공식 인증 주소를 해당 장비의 브라우저로 열고 Esc로 취소한다.
  SSH에서는 기기 코드를 선택한다. 종료/취소 시 원래 인증을 삭제하지 않는다.

등록 해제는 목록 등록만 바꾼다. 기본 인증·로그인 파일·대화·과거 작업과 현재 CLI는
유지한다. 기본 지정은 명시적으로만 하고 실행 중 GUI는 재실행 후 반영한다.
모델·effort·권한은 codexm에서 덮어쓰지 않고 재개된 공식 CLI에서 변경한다.

채팅 관리는 `chats.archive`/`chats.unarchive` bridge 요청으로 공식 Codex의
`--no-daemon archive ID`/`--no-daemon unarchive ID`를 실행한다. 요청한 ID와 처리 후
실제 보관 상태를 확인한다. 목록이 바뀌어도 확인 중인 대상 ID는 바뀌지 않는다.
이미 같은 상태면 반복 실행하지 않으며 실패/응답 불명확한 요청도 자동 재시도하지 않는다.
프로젝트 폴더가 없어도 보관/복원이 가능하고, 대화 재개나 모델 호출은 하지 않는다.

공식 공용 데몬이 그 채팅을 소유하면 해당 소유자의 archive RPC를 사용한다.
접근할 수 없는 GUI/독립 CLI writer가 있으면 원래 창에서 보관하거나 종료하도록
오류를 표시한다. 이 메뉴에서는 GUI 재시작·CLI 종료·writer 잠금 삭제를 하지 않는다.
보관은 진행 중 작업을 중단하거나 Codex 관리 worktree를 정리할 수 있어 확인 화면에 안내한다.
`--no-daemon`도 같은 대화 저장소의 writer 잠금은 공유한다.

TUI 표시 설정은 제품 config 파일명에 `.tui.json`을 붙인 별도 0600 파일이다.
기본값은 `~/.config/codexm/config.toml.tui.json`이며 공식 `~/.codex/config.toml`과
분리된다. 계정별 GUI 상태나 서버 환경설정을 복사하지 않는다.

## 프로세스와 설치 경계

```text
codexm (Go 진입점)
  exec → codexm-tui (Rust, 같은 설치 디렉터리)
           └─ 소유한 codexm bridge --stdio 하나 (Go 계정/읽기 전용 목록/공식 보관·복원)
  Enter → 터미널 복구 → bridge/임시 인증 정리
        → exec codexm resume ID --account NAME
        → 검증된 실행 중 CLI가 있으면 해당 화면 연결
        → 없으면 필요 시 공식 unarchive → default/codexm의 새 창 → 공식 CLI
```

TUI는 Ratatui/Crossterm으로 작성했다. 검증된 Go 인증/목록/CLI 인계는 로컬 stdio
프로토콜로 재사용한다. HTTP/웹/관리자/모델 서버를 시작하지 않는다. 입력마다 프로세스를
띄우지 않으며 bridge는 순차 요청만 처리한다. 조회 중 화면 이동은 계속 가능하고,
사용자 변경 요청은 처리 중인 조회 다음에 한 번만 실행한다. 불명확한 변경은 재시도하지 않는다.
종료·panic·신호 처리에서 터미널을 복구하고 소유 프로세스를 정리한다. SIGKILL처럼
프로세스가 처리할 수 없는 강제 종료까지 복구를 보장하지는 않는다.

터미널 입력 읽기는 별도 스레드이며 모든 Crossterm poll/read를 그 스레드에서만 한다.
PTY가 닫혀 입력 reader가 기다리더라도 주 루프는 신호를 처리하고 bridge를 종료한다.

검증된 macOS GUI가 writer를 소유하면 TUI를 정상 복구한 뒤 터미널에서 매번 GUI 재시작을
확인한다. 다른 GUI 작업도 중단되고 자동 재개되지 않음을 알리며 기본값은 취소다.
공식 CLI가 잠금을 받은 뒤 GUI를 복구한다. 독립 CLI·비대화형 실행은 자동 종료하지 않는다.
[GUI 인계와 tmux 작업 공간](gui-handoff.md)에 실패 복구와 기존 세션 호환성을 설명한다.
잠금 파일 삭제·DB 수정·자동 fork를 하지 않는다.

`build.sh`는 Go와 Rust 실행 파일을 함께 만든다. 설치기는 두 파일의 버전과 내용을
검증하고 두 파일을 합친 해시로 immutable 설치 디렉터리를 구분한다. Rust만 변경해도
새 설치가 된다. 공개 명령은 `codexm` 하나이며 `codexm-tui`는 같은 디렉터리의 내부 구성이다.
이미 실행한 Mach-O 파일은 덮어쓰지 않고 새 inode로 교체하여 macOS 서명 캐시와 충돌하지 않는다.
소스 빌드에는 Go/Rust, 지속되는 CLI 실행에는 공식 Codex와 tmux가 필요하다. 재연결 소유자 확인에는 lsof도 필요하다. 다른 OS를 교차 빌드하려면 Rust target과 해당 플랫폼 linker가 필요하다.

웹 소스/명시적 명령은 호환용으로 남기고 기본 설치에는 웹 번들을 넣지 않는다.
기존 웹의 이름 있는 CLI 세션은 웹을 중지해도 그대로 사용할 수 있다.
`.home`은 빌드/설치에 관여하지 않는다. local/mac-secondary/mac-mobile 적용만 승인됐으며 공개 배포는 보류한다.

## 검증

Rust 단위 테스트는 선택/검색/기본 계정/명시 선택 보존/확인 화면/작은 화면/문자 정화/
설정 저장/소유 bridge 종료를 다룬다. Go의 `TestNativeTUIThroughOwnedTerminal`은 임시
HOME과 합성 DB·인증만 사용하여 실제 tmux PTY에서 탐색·계정 추가·기본 지정·등록 해제·
로그인 취소·공식 CLI 대역으로 Enter 인계·한글 후속 입력을 검증한다.
네이티브 CLI 소유권/보관 동작은 기존 격리 공식 CLI 테스트가 담당한다.

```sh
cargo fmt --all --check
cargo test --workspace --locked
cargo clippy --workspace --all-targets --locked -- -D warnings
go test -race ./...
go vet ./...
binary=$(./build.sh)
CODEXM_TUI_TEST_BINARY="$binary" go test ./internal/cli -run '^TestNativeTUIThroughOwnedTerminal$' -count=1 -v
```

이 테스트는 실제 대화를 보관/재개하지 않는다. 실제 설치의 목록·화면 이동은 별도
읽기 전용 검증을 하고 실행 중 CLI와 인증/공통 설정을 비교한다. Rust가 Go보다
전체적으로 빠르다는 성능 주장은 하지 않는다. GC 없는 TUI와 웹 서버 제거를 우선하고,
나머지 Go 모듈은 동작 계약과 성능 근거를 확보한 단위부터 전환한다.

실제 완료한 검증과 로컬 설치 결과는 [2026-11-02 검증 기록](terminal-validation-20261102.md)에 남긴다.
기존 CLI 재연결과 닫힌 PTY 처리는 [후속 수정 검증](reconnect-validation-20261102.md)을 따른다.
