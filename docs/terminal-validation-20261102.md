# Rust TUI 로컬 적용 검증

2026-11-01~02 KST, 로컬 Mac arm64만 적용했다. 공개 버전은 발행하지 않고 기존
0.6.2 표시를 유지했다. 다른 장비 설치, Homebrew, release/tag/npm 배포는 하지 않았다.

## 완료한 검사

| 검사 | 결과 |
|---|---|
| `cargo fmt --all --check` | 통과 |
| `cargo test --workspace --locked` | 통과: TUI 8개, 메타데이터 진단 4개 |
| `cargo clippy --workspace --all-targets --locked -- -D warnings` | 통과 |
| `cargo check --workspace --all-targets --locked --target x86_64-unknown-linux-gnu` | 통과. Linux 실행/설치 검증은 아님 |
| `go test -race ./...`, `go vet ./...` | 통과 |
| 실제 tmux PTY 통합 검사 | 합성 HOME/DB/계정으로 통과 |
| 소스 설치 트랜잭션 검사 | Rust만 변경 시 새 설치, 버전 불일치 거부, 이전 링크·파일 보존 통과 |
| 실제 로컬 설치 TUI | 목록·검색·상세·F2·계정 목록·정상 종료 통과 |

PTY 검사는 방향키·Enter·F2, 보관 표시 설정, 계정 기본 지정/등록 해제 확인 화면,
기기 코드 계정 추가 완료, 브라우저 로그인 취소, 직전 beta 계정 자동 선택을 확인했다.
Enter 후 공식 CLI 대역에서 한글 후속 입력을 받았고 TUI 이전 terminal modes가
복구됐으며 소유 bridge PID가 종료된 것을 확인했다. 합성 대화 DB·모델/effort 설정은
바이트 단위로 같고, 등록 해제한 계정의 인증은 유지되며 임시 인증 홈은 남지 않았다.
실제 공식 Codex에 모델 요청을 보내거나 실제 사용자 채팅을 보관/재개한 검사는 아니다.
공식 CLI의 writer 소유권 한계는 기존 [격리 검증](native-cli-core.md)을 따른다.

검증 중 발견한 두 제품 문제를 수정했다. 백그라운드 조회가 키 입력을 막던 동작은
조회 중 탐색을 허용하고 명시 변경 요청을 그 조회 다음에 한 번만 실행하도록 바꿨다.
반복 빌드에서 실행한 Mach-O 파일을 그대로 덮어쓰면 macOS가 새 프로세스를 SIGKILL할
수 있어 Rust 결과도 새 inode로 교체한다. 이후 같은 출력 경로 재빌드/실행과 PTY가 통과했다.
테스트 도구의 tmux pane 대상과 respawn 후 TTY 재조회도 수정했다.

## 실제 로컬 적용

`./install.sh --build`로 웹 번들 없이 설치했다. 설치 디렉터리 ID:

```text
v0.6.2-source-c4ee8c6d024c3285b8ea2d5c6b177470d332d4856a4ca0974f4fd312d5012b30
```

`~/.local/bin/codexm`은 immutable 설치의 Go 진입점을 가리키고 같은 디렉터리에
Rust `codexm-tui`가 있다. 이전 웹 포함 설치는 롤백용으로 보존했다. `doctor`가 찾은
공식 CLI는 Homebrew의 0.166.0이며 실제 `codex` 실행 파일/업데이트 경로는 변경하지 않았다.

확인 당시 catalog는 640개, 보관 89개였다. 실제 TUI에서 목록·상세·계정 관리·현재
대화 ID 검색만 하고 재개/인증/기본 변경은 하지 않았다. 시작 전후 인증·공통 설정·제품
설정 12개 파일의 해시와 symlink 대상이 동일했다. 비교 대상 중 원래 없던 파일 1개도
생성되지 않았다. 실제 대화 DB는 다른 작업이 쓰는 중이므로 전체 파일 불변이라고 주장하지 않는다.

전용 웹은 `codexm web stop`으로 중지했다. Caddy 설정이 해당 웹 한 곳만 프록시하는
것을 확인한 뒤 `codexm-https` tmux에 Ctrl+C를 전달해 종료했다. 4317/443 리스너와
두 서비스 tmux가 사라졌고 `web status`는 `running:false`였다. 로그인·인증서 설정은
지우지 않았다. 기존 공식 Codex CLI tmux 2개와 agent-discord의 세션/프로세스 PID는
유지됐으며 모든 이번 TUI 테스트 세션은 정리했다.

private 증거: `~/.local/state/codexm/validation/native-tui-20261101/`의
`live-before.json`, `live-after.json`, `live-tui-result.json`, 화면 캡처 텍스트.
인증 파일 내용이나 대화 제목을 Git 문서에 넣지 않는다.

웹 TypeScript는 변경하지 않았다. 웹 전체 회귀 검사를 이번 TUI 변경으로 재실행했다고
주장하지 않는다. Go 인증/목록/공식 CLI 인계는 유지하며 Rust로 전체 전환되거나 전체
제품 성능이 향상됐다는 측정 결과도 없다. 새 기본 UI를 Rust로 구현하고 웹 실행 의존성을
제거한 단계다.
