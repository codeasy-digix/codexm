# Rust 개발과 단계별 전환

신규 네이티브 기능·파일 처리·진단 코드는 Rust를 우선한다. 루트 Cargo workspace와
`crates/` 아래 라이브러리를 공유하고 `Cargo.lock`을 커밋한다. 현재 도구 체인은
`rust-toolchain.toml`에 고정하며 개발 PC의 셸 설정을 제품 설치기가 바꾸지 않는다.
의존성 설치·도구 체인 갱신은 개발 작업이고, 만들어진 실행 파일에는 Rust/Python 런타임이
필요 없다. Cargo 패키지는 `publish = false`이며 배포 작업은 계속 보류한다.

| 영역 | 현재 선택과 이유 |
|---|---|
| 파일 메타데이터 진단 | `crates/state-audit` Rust 라이브러리와 CLI. 남아 있던 Python 도구를 대체 |
| 웹 UI·웹 서버 | Meteor + React TypeScript 유지. 브라우저/웹 생태계에 맞는 경계 |
| 계정·공식 CLI 인계·설치 | 기존 Go 유지. 검증된 인증·프로세스 동작을 한 번에 재작성하지 않음 |
| 셸 설치·셸 환경 로더 | 운영체제 접점에 필요한 짧은 sh/zsh 유지 |
| 퇴역한 worker/manager/sync | 전환 대상 아님. 새 Rust 코드로 되살리지 않음 |

현재 Rust 진단 코드는 수동 개발 도구다. 기존 `build.sh`, `install.sh --build`의 Go
제품 설치와 분리해, 필요 없는 Rust 도구를 모든 실행에 끼워 넣지 않는다. 진단은 아래처럼
저장소에서 빌드한 단일 실행 파일로 사용한다. `.home`은 빌드나 소스를 관리하지 않는다.

```sh
cargo build --release --locked -p codexm-state-audit
./target/release/codexm-state-audit --help
./target/release/codexm-state-audit snapshot before
# 사용자가 확인하려는 작업 이후:
./target/release/codexm-state-audit snapshot after
./target/release/codexm-state-audit diff
```

계정·대화 내용은 열지 않는다. symlink의 대상 경로는 기록하되 디렉터리 링크 안으로
들어가지 않는다. 파일의 mode·inode·size·시간을 수집하며 기본 결과 위치는
`$XDG_STATE_HOME/codexm/state-audit/<home의 SHA-256>/`이다. XDG 변수가 없으면
`~/.local/state`를 쓴다. 디렉터리는 0700, 결과는 0600이고 기존 파일을 덮어쓰지 않는다.
진단 결과에도 파일명과 경로가 있으므로 Git에 넣지 않는다. 이 도구는 sync·DB 복구를
실행하지 않으며 계정 변경 시 자동 실행하지 않는다.

Python의 schema 1 스냅샷은 `diff OLD NEW`로 계속 읽을 수 있다. 기존 결과 위치를
그대로 쓸 때는 `--output /path/to/auth-switch-audit`를 명시한다. 새 결과는 schema 2로,
UTC 시각과 정수 나노초를 사용한다. schema 1과 비교할 때는 Python/macOS의 부동소수점
birthtime 정밀도 차이만 제외하고 그 사실을 결과에 표시한다. 서로 다른 home의 스냅샷은
실수로 비교하지 않는다. 읽을 수 없는 경로·중복 경로·지원하지 않는 schema는 실패로 처리한다.

다음 전환은 경계가 분리된 기능부터 진행한다. Go 모듈을 Rust로 옮길 때에는 기존 동작
계약, 오류/취소, 소유 프로세스 정리, 설치·업데이트 경로를 함께 검증한 후 기존 구현을
제거한다. 언어 변경을 위해 매 요청마다 프로세스를 추가하거나 FFI와 이중 구현을 계속
유지하지 않는다. 성능 개선은 동일 입력·같은 장비에서 측정하고, 측정하지 않은 웹/CLI
전체의 속도 향상을 주장하지 않는다.

```sh
cargo fmt --all --check
cargo test --workspace --locked
cargo clippy --workspace --all-targets --locked -- -D warnings
```

CI에는 macOS/Linux의 Rust 검사도 포함한다. 다른 실제 장비의 codexm 설치/업데이트와
Homebrew·릴리스·레지스트리 배포는 별도 승인 전까지 진행하지 않는다.

실제 수행한 범위와 성능 측정은 [로컬 검증 기록](accounts-rust-validation-20261101.md)에
남긴다. 구조는 [Cargo workspace 문서](https://doc.rust-lang.org/cargo/reference/workspaces.html)와
[Unix 파일 메타데이터 API](https://doc.rust-lang.org/std/os/unix/fs/trait.MetadataExt.html)를 따른다.
