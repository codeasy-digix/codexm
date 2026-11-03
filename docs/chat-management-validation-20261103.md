# 오른쪽 pane의 채팅 관리 검증

2026-11-03. 로컬 Mac에만 빌드·설치했다. 사용법은 [TUI 문서](terminal-ui.md)를 따른다.

## 동작

- 목록 → 첫 번째 Right: 기존 계정 선택. 두 번째 Right: 같은 오른쪽 pane의 채팅 관리.
- 보관하기/보관 해제를 선택하면 같은 pane에서 취소가 기본인 확인 화면을 표시한다.
- 목록 갱신·필터 변경 중에도 관리/확인 대상 ID를 유지한다. 원래 채팅이 목록에서
  사라졌다면 다른 행에 적용하지 않고 다시 선택하도록 한다.
- 보관/복원 성공 후 실제 상태를 확인하고 목록을 갱신한다. 실패 시 메뉴와 오류를
  유지하며 불명확한 요청을 재전송하지 않는다. `A`로 보관 목록을 포함해 복원할 수 있다.
- Rust가 화면/키 처리를 담당하고 기존 stdio bridge로 공식 native 보관 명령을 호출한다.
  native catalog와 DB는 직접 수정하지 않는다. 네이티브 mutation은 같은 ID의 재개
  준비와 제품 내부 gate를 공유한다. 이 gate는 Codex writer lock과 별개다.
- 접근 가능한 공식 공용 데몬이 대상 채팅을 소유한 경우 owner archive RPC를 사용한다.
  별도 GUI/CLI writer에 접근할 수 없으면 원래 창에서 처리하도록 오류를 표시한다.
  이 메뉴에서는 GUI/CLI 종료, 로그인 변경이나 대화 재개를 실행하지 않는다.

## 실제 실행한 검증

| 검사 | 결과 |
|---|---|
| `cargo fmt --all --check` | 통과 |
| `cargo test --workspace --locked` | 통과; 계정 선택 보존, 두 번째 Right, 확인 취소, 대상 고정, 보관/복원 후 목록, 작은 화면 포함 |
| `cargo clippy --workspace --all-targets --locked -- -D warnings` | 통과 |
| `go test -race ./...`, `go vet ./...` | 통과 |
| 합성 native 명령과 로컬 DB의 보관·복원·중복 요청·사라진 프로젝트·다른 writer | 통과; 다른 ID 보존, 실패/잠금 시 상태 유지 |
| 실제 tmux PTY `TestChatManagementThroughTUI` | 두 번 Right, 기본 취소, 보관, A 표시, 복원, native 실패 표시 통과 |
| 공식 CLI 0.166.0 `TestOfficialUnarchiveAndResumePreparation` | 격리 HOME·mock model에서 기존 인계와 새 관리 API의 보관/복원 통과; 다른 채팅 writer 유지 |
| 기존 `TestNativeTUIThroughOwnedTerminal` | 계정 관리·Enter 인계·한글 입력·PTY/SIGTERM 정리 통과 |
| 선택적 웹 계약의 typecheck와 단위 테스트 | 통과; 공유 bridge 메서드 두 개와 계약 일치 |
| `install.sh --from-build` | Go/Rust 쌍의 새 immutable 로컬 설치 통과 |
| 설치 전후 native auth/config와 codexm config 해시 | 동일 |

테스트의 직접 SQLite 변경은 테스트 전용 대역과 합성 DB 준비에만 있다. 실제 사용자의
대화는 검증을 위해 보관하거나 복원하지 않았다. 사용자 데이터 화면은 읽기 전용으로
새 메뉴를 확인한다. 기존 GUI·공식 CLI 작업은 중단하지 않는다.

`--no-daemon`은 공유 app-server daemon을 사용하지 않는 실행 경로다. 같은 로컬
대화 저장소의 동일 채팅 ID는 이 모드에서도 writer lock을 공유한다. 옵션 하나로 GUI가
소유한 채팅을 중복 실행할 수 있게 되는 것은 아니며 기존 소유권 검사/인계가 필요하다.

로그·화면·이전/새 설치 경로·해시는 로컬
`~/.local/state/codexm/validation/chat-management-20261103/`에 보관한다.
실제 인증·대화 원문은 Git에 넣지 않는다. 원격 장비와 Homebrew/release는 변경하지 않았다.
