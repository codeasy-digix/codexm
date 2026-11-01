# 계정 관리 복원과 Rust 진단 도구 검증

현재 Mac 로컬에만 계정 UI/CLI 변경을 설치했다. 웹의 계정 선택은 직전 등록 계정을
사용하고, 이전 기록이 없거나 등록 해제되면 native default로 돌아간다. 사용자가 고른
값은 주기적 목록 갱신으로 바뀌지 않는다. CLI 번호 메뉴의 Enter도 같은 규칙이다.

웹에 계정 추가·재로그인·브라우저/기기 코드 인증·기본 지정·등록 해제를 복원했다.
이전 계정의 인증·모델/effort 설정·대화·실행 중 작업을 계정 간 복사하지 않는다.
등록 해제는 파일 삭제가 아니며, 인증 확인 후 같은 닉네임을 다시 등록할 수 있다.

검증은 임시 계정·SQLite·가짜 공식 인증/터미널 프로세스를 분리한 fixture에서 수행했다.
실제 설치 웹에서는 HTTPS 로그인, 목록 조회, 두 종류의 계정 선택, 관리 버튼,
모바일 폭, 로그아웃만 확인했다. 실제 계정을 추가/제거하거나 기본 인증을 변경하지 않았다.

| 검증 | 결과 |
|---|---|
| `go test -race ./...`, `go vet ./...` | 통과 |
| TypeScript typecheck, Node 테스트 7개, Meteor production build | 통과 |
| 브라우저 계정 관리·취소/로그아웃 정리, 인계 실패/명시적 재시도 | 2개 통과 |
| 모바일/PC 목록, 공식 CLI fixture 입력·재연결·종료·계정 선택 | 4개 통과 |
| 설치된 공식 CLI 0.166.0의 격리된 로그인 미리보기/취소 | 통과. 실제 인증 완료·모델 호출 없음 |
| 로컬 HTTPS 웹 | 마지막 등록 계정/기본 계정 선택, 4개 계정 관리 표시, 로그아웃 후 401 확인 |
| 설치 전후 보존 | 인증·설정 14개 내용/링크 동일, 기존 실행 중 CLI 2개 pane/PID 유지 |
| Rust fmt, locked workspace 테스트, clippy `-D warnings`, release build | 통과 |
| Rust `cargo check --all-targets --target x86_64-unknown-linux-gnu` | 로컬 크로스 컴파일 검사 통과. 실제 Linux 실행 테스트는 아님 |
| 실제 구 Python schema 1 ↔ 새 Rust schema 2 결과 비교 | 같은 fixture에서 추가·삭제·변경 0 |

처음 전체 브라우저 실행의 CLI 테스트 한 건은 복사한 fixture의 symlink가 예전
fixture 폴더를 가리켜 실패했다. 소유 경로 검사로 거부된 것이며, 테스트 링크를 해당
fixture 내부로 수정한 뒤 재검증했다. 제품의 외부 링크 거부 규칙은 완화하지 않았다.

`Pane is dead (status 1)`은 현재 GUI가 소유한 채팅의 공식 archive 거부가 원인이었다.
웹은 인계 실패 원인을 터미널 위에 표시하며 오류 상세/재시도 버튼을 제공한다. 원래
GUI/CLI의 writer를 강제 제거하지 않는다. 사용자는 원래 GUI에서 보관하거나 해당
CLI 세션을 종료한 뒤 다시 시도해야 한다. 실행 중인 GUI 채팅은 이 검증에서 건드리지 않았다.

Rust 전환 대상은 유일하게 남아 있던 Python 메타데이터 진단 도구다. `crates/state-audit`
라이브러리/CLI로 대체하고 Python 소스를 제거했다. 일반 제품 실행은 Go, 웹은 TypeScript를
유지한다. 퇴역 worker/sync는 전환하거나 다시 활성화하지 않았다.

로컬 APFS의 합성 파일 5,021개에서 전체 스냅샷/Markdown 생성 시간을 측정했다.
각 도구 1회 warm-up 후 순서를 번갈아 5회 실행한 중앙값은 Python 267.3ms,
Rust release 65.6ms였다(약 4.1배). 이는 진단 명령에 대한 결과이며 서로 다른 출력
위치 정책(Python: 기존 audit 폴더, Rust: 별도 상태 폴더)을 사용했다. 웹·resume·실제
대형 대화 저장소 전체에 대한 성능 개선으로 일반화하지 않는다.

검증 웹 `codexm-account-test`와 테스트 CLI는 종료했다. 실제 웹 `codexm-web`과
HTTPS 프록시 `codexm-https`, 기존 사용자 CLI는 유지한다. Homebrew/릴리스/태그/원격
장비 설치는 수행하지 않았다. 원본 증거와 출력은 Git 밖의
`~/.local/state/codexm/validation/account-ui-20261101/`, `rust-audit-20261101/`에 보관한다.
