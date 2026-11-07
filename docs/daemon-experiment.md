# 계정별 공식 daemon 연결 실험

`gui-control-way` 브랜치의 `gui_experiment` 빌드에만 들어간다. 일반 `codexm`,
TUI, 계정 명령, native `codex`, 설치본과 Homebrew는 바꾸지 않는다. 독자적인 모델
실행기나 상시 계정 회전 서비스를 만들지 않고, 공식 daemon의 Unix WebSocket
API와 공식 CLI frontend를 연결한다. 운영 GUI도 재시작하거나 재설정하지 않는다.

## 역할과 인증

```text
codexm 실험 명령 ── 계정·프로세스 검증 / 선택한 채팅 인계
                  ├─ 공식 daemon A ── 계정 A의 여러 채팅
                  └─ 공식 daemon B ── 계정 B의 여러 채팅
공식 CLI frontend ── --remote unix://선택한-daemon-소켓
```

서버 로그인은 해당 daemon 전체에 적용된다. 계정별 `CODEX_HOME`에 이미 저장된
로그인을 사용하며, 공유하는 것은 같은 장비의 기존 대화 저장소다. 자격 증명을
복사하거나 기본 로그인을 바꾸지 않는다. `default`를 daemon 계정으로 지정하는
것은 거절한다. 다른 머신으로 동기화하는 기능도 없다.

연결 전에 공식 `daemon version`, 예상 계정 경로, 소켓 UID, 실제 프로세스의
실행 파일·시작 시각·인자, `account/read`의 현재 이메일과 workspace를 검증한다.
계정 파일의 사용자/workspace 식별 해시와 프로세스 정보를 등록 영수증에 고정한다.
재로그인이나 서버 재시작으로 달라지면 자동으로 따라가지 않고 재검증을 요구한다.
토큰, 비밀번호, 프롬프트는 영수증에 저장하지 않는다.

이번 연결과 인계 코드는 기존 Go 계정·프로세스·RPC 검증을 재사용하는 확장이다.
같은 검증을 다른 언어로 중복 구현하거나 새 FFI/상주 bridge를 추가하지 않았다.
GUI 전달 모듈과 기본 TUI의 Rust 구현에는 변화가 없다.

## 빌드와 명령

[실험 빌드 스크립트](../scripts/build-gui-experiment.sh)를 default tmux 서버의
`codexm` 작업 공간 안에서 실행한다. 출력된 새 디렉터리의 절대 경로를 사용하며
일반 설치 명령을 실행하지 않는다. 현재 쓰기 프로토콜은 검증한 공식 CLI
**0.167.0**에 제한한다. 설치된 공식 runtime을 자동으로 교체하지 않는다.

```sh
CX=/absolute/path/to/target/gui-experiment.XXXXXX/codexm

"$CX" daemon start --account ACCOUNT_A
"$CX" daemon start --account ACCOUNT_B
"$CX" daemon status --account ACCOUNT_A

"$CX" daemon new --account ACCOUNT_A --cwd /absolute/project
"$CX" daemon resume THREAD_ID --account ACCOUNT_A
"$CX" daemon resume THREAD_ID --account ACCOUNT_A --terminal

"$CX" daemon switch THREAD_ID --from ACCOUNT_A --account ACCOUNT_B
"$CX" daemon resume THREAD_ID --account ACCOUNT_B

# 진행 중인 선택 턴만 중단한 뒤 같은 ID를 인계
"$CX" daemon switch THREAD_ID --from ACCOUNT_A --account ACCOUNT_B --interrupt

# 기존 요청 관찰: 재전송이나 자동 복구를 하지 않음
"$CX" daemon request REQUEST_ID
```

- `start`: 선택한 계정에 공식 daemon이 없으면 명시적으로 시작한다. 이미 실행
  중이면 시작 명령을 다시 보내지 않고 기존 서버를 검증·등록한다. restart,
  update, login, bootstrap은 호출하지 않는다. 기존 소켓 상태가 불확실하면 거절한다.
- `status`: 등록된 서버의 현재 인증·프로세스와 로드된 채팅 ID를 조회한다.
  서버를 시작하거나 채팅을 로드하지 않는다.
- `new/resume`: 검증한 서버에 공식 CLI를 `--remote`로 연결한다. frontend 창은
  기존 정책대로 default/codexm에 만들고, 터미널별 독립 뷰어로 연결한다.
  `--terminal`은 창만 남기고 돌아온다. 새 채팅 ID는 공식 CLI가 생성한다.
  추가 프롬프트나 자동 continuation은 보내지 않는다. native 기본 계정이나
  다른 GUI/CLI가 이미 소유하는 채팅은 강제로 가져오지 않는다.
  자기 daemon이 idle 채팅을 unload하는 경계에서는 writer 해제를 최대 3초간
  관찰한다. 이 대기는 읽기만 하며 archive나 다른 프로세스 종료를 하지 않는다.
- `switch`: 양쪽 daemon이 이미 실행·등록되어 있어야 한다. 서로 다른 실제
  로그인인지 확인하고, 원래 서버가 해당 ID의 단독 writer이며 loaded 목록에도
  있는지 확인한다. 원래 서버의 공식 archive → writer 해제 확인 → 대상 서버의
  unarchive/resume 순서로 인계한다. fork나 ID 변경은 없다. 완료 뒤에는 선택한
  계정으로 `daemon resume`하여 입력한다.
- 활성 턴은 기본적으로 거절한다. `--interrupt`를 명시하면 그 턴 ID에만 공식
  interrupt를 보내고 idle 상태를 확인한다. 다른 채팅/서버/프로세스를 종료하지 않는다.

`switch` 전에 선택한 채팅의 다른 뷰어에서 입력을 멈추고 닫는다. 소유권 검사와
공식 RPC 전체를 하나의 원자적 트랜잭션으로 묶는 API는 없다. 다른 클라이언트가
동시에 다시 실행하거나 로그인을 바꾸면 보수적으로 중단한다. 특히 GUI의 stdio
writer를 이 경로로 해제할 수 있다고 가정하지 않는다.

## 불확실한 인계와 부작용

요청 UUID를 stderr에 먼저 출력한다. `--request-id UUID`로 직접 지정할 수도 있다.
영수증 상태는 prepared, interrupting, archiving, restoring, resuming, completed,
unknown이다. `completed`는 소유권 인계 완료이며 모델 작업 완료를 뜻하지 않는다.

같은 요청 ID를 다시 사용하면 저장된 결과만 반환한다. 다른 인자로 같은 ID를
사용하면 거절한다. 중간 결과가 불확실하면 새 요청 ID로도 해당 채팅을 재시도하지
못하게 막는다. `request`와 양쪽 `status` 및 native 상태를 확인해야 하며, 자동
rollback·재전송·영수증 초기화 명령은 제공하지 않는다. 불확실한 상태를 단순히
실패로 지우거나 다음 호출에서 충돌 0으로 숨기지 않는다.

원래 archive RPC는 하위 채팅에 영향을 줄 수 있어, 보관 상태를 포함한 spawned
descendant가 있으면 인계를 거절한다. 큐에 입력이 남아 있는 채팅도 거절한다.
공식 archive/unarchive 자체는 대화의 보관 상태를 잠시 바꾸고 native 메타데이터를
갱신할 수 있다. 직접 SQLite/rollout/lock을 수정하지 않으며, UI의 pin/정렬까지
완전히 동일하게 유지한다고 보장하지 않는다.

영수증은 설정한 `storage.state_dir/daemon-experiment/`의 servers, requests,
chats, terminals 아래에 0600으로 저장한다. 상태 조회는 모델 요청을 만들지 않는다.
재부팅 시 자동 시작은 설정하지 않았다. 터미널 종료 후 유지와 재부팅 복구는
별개이며, 새 stop/restart 자동화도 이번 범위에 넣지 않았다.

## GUI 연결 결과와 한계

공식 daemon은 CLI와 다른 클라이언트가 같은 서버에 연결하는 프로토콜을 제공한다.
설치된 macOS 앱의 local-daemon 경로는 Unix 소켓의 `/rpc` WebSocket을 사용한다.
그 경로의 initialize와 공식 `app-server proxy --sock SOCKET`를 통한 같은 채팅
조회가 격리 시험에서 성공했다. proxy는 stdio에 **raw WebSocket 전송을 터널링**한다.
JSON-lines stdio app-server처럼 직접 호출하면 연결되지 않는다.

그러나 현재 앱은 독립 stdio app-server를 사용한다. 관찰한 앱의 daemon 연결
조건에는 `CODEX_APP_SERVER_USE_LOCAL_DAEMON=1`, 로컬 host, 호환 버전 외에도
config override와 CLI 경로 override가 없어야 한다는 제약이 있다. 현재 GUI 실행에는
config override가 있어서 환경변수 하나로 이 분기로 바뀐다고 볼 수 없다.

따라서 **실제 GUI가 선택 계정 daemon에 붙도록 전환한 것은 아니다.** 진행 중인
GUI를 종료하거나 설정을 제거하지 않았고, 앱의 내부 IPC·UI 자동화로 우회하지 않았다.
GUI가 원래부터 같은 daemon에 연결되어 있으면 그 서버의 계정을 사용한다는 구조적
가능성과, 기존 GUI를 무중단으로 계정별 서버에 재배치하는 기능은 구분해야 한다.
현재 GUI의 채팅별 로그인 변경이나 자동 서버 전환도 구현하지 않았다.

공식 참고: [App Server](https://learn.chatgpt.com/docs/app-server),
[Remote connections](https://learn.chatgpt.com/docs/remote-connections),
[워크스페이스 인증·routing 구현](https://github.com/openai/codex/blob/main/codex-rs/app-server/src/request_processors/account_processor/workspace_routing.rs).
로컬 확인 버전의 세부 동작을 우선하며, upstream main은 이후 변경될 수 있다.

## Discord는 별도 제품

Discord 알림과 답장 라우팅은 **agent-bridge**가 담당한다. 별도 소스 프로젝트,
설치 바이너리, MCP server와 receiver/router/executor 및 durable routing 상태를 갖는다.
codexm의 compact CLI는 Discord receiver나 예전 notification worker를 시작하지 않는다.
같은 default/codexm tmux 작업 공간에 관련 연결 창이 있어도 제품이 통합된 것은 아니다.

codexm은 계정·목록·공식 실행 연결을 관리하고, agent-bridge는 알림과 답장이 갈
실제 채팅/실행 경로를 관리한다. 두 제품이 MCP와 native 실행 정보로 연동하는
현재 경계를 유지한다. 이번 daemon 실험에서 Discord 라우팅 코드를 바꾸거나,
새 daemon 경로의 Discord 답장 전달을 end-to-end 검증했다고 주장하지 않는다.

## 재현 시험

`internal/nativecli/daemon_*experiment_test.go`에 기본 검증과 opt-in native 시험이 있다.
후자는 임시 HOME, synthetic 계정, loopback 인증/routing·모델 응답을 사용한다.
실제 로그인 파일이나 사용자 대화는 읽거나 복사하지 않는다. 계정별 공식 패키지와
daemon을 임시 디렉터리에 만들고 테스트 종료 후 해당 서버/소유 helper만 정리한다.

```sh
CODEXM_GUI_CONTROL_TEST=1 \
CODEXM_NATIVE_RUNTIME=/absolute/path/to/official/0.167.0/bin/codex \
CODEXM_GUI_EXPERIMENT_BINARY=/absolute/path/to/experiment/codexm \
go test -race -tags gui_experiment ./internal/nativecli ./internal/cli \
  -run '^TestDaemon' -count=1 -timeout=4m -v
```

운영 계정의 실제 OpenAI 모델 요청이나 quota 소비를 시험한 결과와, 로컬 모형
서버로 인증 경계·native 소유권·프로세스 수명을 검증한 결과를 혼동하지 않는다.
구체적인 관측 결과는 [검증 기록](gui-control-way-validation.md)에 남긴다.
