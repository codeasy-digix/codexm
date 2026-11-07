# 독립 CLI에서 GUI 소유 채팅 제어

`gui-control-way` 브랜치의 macOS 실험이다. 기존 `codexm` TUI, 계정 선택,
CLI 인계, 설치본과 Homebrew 패키지는 그대로 유지한다. 새 제어기는 Rust
`crates/gui-control`에 있고, Go 진입점은 `gui_experiment` build tag로만 활성화된다.
새 서버 서비스나 GUI 패치, Accessibility 키 입력, 비공개 앱 IPC를 사용하지 않는다.

## 확인한 연결 방식

1. 서명 검증한 공식 앱에 `codex://threads/THREAD_ID`를 LaunchServices로 연다.
   앱이 없으면 자동 설치하지 않으며, 설치되어 있고 실행 중이 아니면 시작을 요청한다.
2. native writer 파일을 **열거나 변경하지 않고** `lsof`와 프로세스 정보로 관찰한다.
   현재 사용자의 GUI와 번들 runtime 사이의 부모 관계, PID와 실행 시각을 확인한다.
   외부 CLI가 소유하면 실패하며 그 프로세스를 종료하지 않는다.
3. 같은 GUI 번들 runtime을 짧게 `app-server --listen stdio:// --no-daemon`으로
   실행한다. GUI와 같은 native home을 사용하지만 이 보조 연결에서는
   `thread/resume`, `turn/start`, archive/unarchive를 호출하지 않는다.
4. 공식 `thread/queue/add`로 입력을 한 번 등록한다. GUI가 이미 소유한 채팅에서
   이를 소비한다. 별도의 모델·인증·effort·권한 override는 보내지 않는다.
5. `clientUserMessageId`로 등록 요청과 실제 `userMessage.clientId`를 대조한다.
   보조 연결은 종료하고 GUI의 실행은 유지한다.

이는 GUI의 stdio에 직접 접속하는 방식이 아니라 **공식 native queue를 통한 전달**이다.
해당 채팅을 GUI가 로드했는지 확인하지 않고 큐에만 넣으면 실행되지 않을 수 있다.
`queue --resume`로 보조 서버에 실행을 맡기지도 않는다. `--no-daemon queue`와 달리
지원되는 official app-server RPC로 큐만 등록한다. 호환성을 확인한 runtime은
`0.167.0` / `0.167.0-alpha.3.2`이며 다른 버전은 쓰기 전에 거절한다.

앱 내부 도구의 native pipe에는 자체 peer 검증과 호출 컨텍스트가 있다.
그 내부 프로토콜이나 호출자 정보를 흉내 내는 경로는 제품에 도입하지 않는다.

## 빌드와 사용

일반 설치/패키지 명령을 실행하지 않는다. 빌드는 default tmux 서버의 `codexm`
작업 공간에서 독립 창으로 실행한다.

```sh
tmux -L default new-window -d -t codexm: -n gui-build \
  'cd /absolute/path/to/codexm && sh scripts/build-gui-experiment.sh'
```

스크립트가 출력하는 `target/gui-experiment.XXXXXX/codexm` 절대 경로를 사용한다.
옆에 있는 `codexm-gui`와 같은 빌드 디렉터리에서 실행해야 한다. 같은 소스의
`codexm-tui`도 포함하고 세 실행 파일의 SHA-256을 `SHA256SUMS`에 기록한다.
실행 중인 파일을 덮어쓰지 않고 매번 새 빌드 디렉터리를 만든다.

```sh
/path/to/experiment/codexm gui status
/path/to/experiment/codexm gui list --limit 30
/path/to/experiment/codexm gui list --archived --limit 30
/path/to/experiment/codexm gui open THREAD_ID
printf '%s' '남은 검증을 이어서 진행해줘.' | \
  /path/to/experiment/codexm gui send THREAD_ID --message-stdin --wait 15
/path/to/experiment/codexm gui status REQUEST_ID
/path/to/experiment/codexm gui new --cwd /absolute/project/path
printf '%s' '이 프로젝트 구조를 설명해줘.' | \
  /path/to/experiment/codexm gui new --cwd /absolute/project/path --message-stdin
```

- `open`: GUI 실행/탐색 요청 후 실제 GUI writer 확인. 메시지를 보내지 않는다.
- `send`: 기존 채팅에 텍스트를 **큐**로 전달한다. 진행 중인 턴은 끝까지 실행하고
  이어서 입력을 처리한다. 즉시 방향을 바꾸는 steer와는 다르다.
- `new`: 공식 링크로 새 채팅의 **초안**을 연다. 사용자가 GUI에서 Send를 눌러야 한다.
  자동 전송을 가장하거나 보이지 않는 빈 대화를 DB에 만들어 놓지 않는다.
- `status REQUEST_ID`: 이미 보낸 요청을 읽기 전용으로 다시 확인한다.
- `list`: native 저장 목록의 페이지를 조회한다. 다음 페이지에는 `--cursor`를 사용한다.
  `useStateDbOnly`를 지정하여 rollout 검색·메타데이터 복구를 요청하지 않는다.
  목록의 정보는 저장된 데이터이며 실시간 실행 여부를 의미하지 않는다.

`--app /absolute/path/App.app`으로 설치 위치를 지정할 수 있다.
macOS 외에는 실험 명령이 지원되지 않으며 기존 macOS/Linux CLI는 영향받지 않는다.
실험 빌드에서도 인자 없이 실행하면 기존 TUI가 열리며, GUI 경로는 위의 `gui` 명령으로 선택한다.

## 접수와 완료를 구분하기

출력은 JSON 한 개다. `accepted`는 큐 접수, `started`는 해당 입력의 턴이 기록됨,
`completed`는 완료 기록을 확인했음을 뜻한다. `failed`는 명시적인 native 오류다.
`not_sent`는 검증 단계에서 전달하지 않은 경우, `unknown`은 전달 결과가 불명확한
경우다. `owner_changed`는 원래 GUI 소유자가 없어지거나 바뀐 경우,
`owner_unverified`는 소유자 조회가 실패하여 판단할 수 없는 경우다.

실제 시험에서 **별도 app-server의 읽기 결과는 진행 중인 GUI 턴을 잠시
`interrupted`로 표시했다.** GUI는 정상 응답을 완료했다. 따라서 이 값만으로 중단을
확정하거나 재전송하지 않는다. 요청 ID가 보이면 `started`로 표시하고 명시적인
완료/오류를 기다린다. 실제 사용자 중단도 이 경로만으로 즉시 확정할 수는 없다.

요청 영수증은 `$XDG_STATE_HOME/codexm/gui-control/requests/` 또는
`~/.local/state/codexm/gui-control/requests/`에 0600으로 저장한다. 프롬프트 대신
해시·요청 ID·대화 ID·검증한 프로세스·관측 상태를 저장한다. 사용자가
`--request-id UUID`를 재사용하면 기존 결과만 반환하며 다시 보내지 않는다.
다른 내용에 같은 ID를 쓰면 거절한다. `status`도 재전송하지 않는다.

## 범위와 부작용

- GUI가 현재 사용하는 계정으로 실행한다. CLI에서 고른 `CODEX_HOME`이나
  `--account`를 GUI에 주입하지 않는다. 계정 변경 기능은 기존 경로를 유지한다.
- 앱 링크는 앱을 활성화하고 보이는 채팅을 바꾼다. 새 초안은 사용자에게 보인다.
  초안용 prompt 링크의 텍스트는 OS URL 전달 인자에 들어간다. `send`의 텍스트는
  stdin/stdio로만 전달되고 영수증에는 저장되지 않는다.
- native queue는 내구성 있는 채팅별 큐이며 GUI PID에 고정된 메시지함이 아니다.
  검증 직후 GUI 종료/다른 CLI 시작이 경쟁하면 나중 소유자가 이를 처리할 수 있다.
  원래 소유자 유지 여부를 다시 관찰하지만 원자적인 GUI 전용 전달 보장은 아니다.
  그런 경우 큐 삭제·인계·재시도는 자동으로 하지 않는다.
- 공식 API를 통하더라도 native queue/이력이 갱신된다. 직접 SQLite/rollout/lock을
  수정하지 않는다는 뜻이지 디스크에 아무 변화도 없다는 뜻은 아니다.
- 보관된 채팅은 GUI에서 먼저 복원해야 한다. 다른 CLI의 실행도 종료하지 않는다.
- 새 채팅 **자동 생성·첫 전송**, 즉시 steer, GUI 내부 계정 전환은 이 실험의
  지원 범위에 들지 않는다. 공개 링크는 초안만 지원하며 이를 우회하는 UI 자동화나
  내부 app-tools 호출을 추가하지 않았다.
- GUI가 꺼져 있는 경우의 실제 종료/재기동 시험은 진행 중인 GUI 작업을 보존하기
  위해 하지 않는다. LaunchServices 시작 경로를 사용하지만 실제 검증은 실행 중인
  GUI와 로드되지 않은 테스트 채팅을 대상으로 한다.
- Computer Use는 이 앱 자체의 화면 접근을 거절하므로 새 초안의 시각적 검증은
  수행하지 않았다. 이 제한을 AppleScript·비공개 IPC 등으로 우회하지 않는다.

## 검증

`cargo test --locked -p codexm-gui`는 소유자 교체·외부 CLI 거절·불명확한 ACK의
재전송 금지·요청 ID 대조·passive interrupted 상태·영수증 권한·입력 검증을 확인한다.
기존 isolated official-runtime 시험은 `docs/gui-control-way-validation.md`를 참고한다.
실제 GUI 시험은 사용자가 허용한 임시 채팅에서만 수행하고 실제 ID/원시 로그는
Git 밖의 비공개 validation 디렉터리에 둔다.

2026-11-07 macOS arm64, GUI 번들 `0.167.0-alpha.3.2`에서 독립 tmux CLI로 확인했다.

| 시험 | 결과 |
| --- | --- |
| GUI idle 채팅에 `send` | 동일 ID의 새 userMessage 및 완료 응답 확인 |
| busy 채팅에 후속 `send` | 원래 턴 완료 시각 이후에 후속 턴 시작, 동시 실행 없음 |
| 동일 `--request-id` 재호출 | 기존 영수증 반환, 같은 turn ID 유지, 재전송 없음 |
| 미로드 채팅에 `send` | 공식 링크로 GUI writer 확보 후 같은 ID에서 실행 |
| 접수 직후 호출 CLI에 SIGTERM | 호출 CLI와 소유한 보조 프로세스 종료, GUI는 응답 완료 |
| 별도 CLI가 writer를 소유 | `not_sent`, 큐 ID 없음, 원래 CLI 유지 |
| 보관된 채팅 | `not_sent`, 자동 복원·입력 없음 |

전체 시험에서 GUI main/app-server PID와 시작 시각은 동일했다. 외부 CLI 거절 시험의
CLI만 시험 후 검증된 PID로 정리했다. 임시 채팅은 완료 후 다시 보관했다.
새 초안의 실제 화면, GUI cold start, 앱 종료와 큐 등록의 동시 경쟁, 승인 대기나
첨부파일 처리는 이 결과에 포함하지 않는다.

Rust workspace fmt/check/test/clippy, Go 전체 race/vet 및 실험 build-tag 검사,
두 공식 runtime 버전의 격리 시험을 통과했다. `TestGUIControlDirectQueueRPCNeverLoadsTheRelay`는
queue RPC 후에도 보조 서버의 loaded 목록이 비어 있고 원래 writer가 유지되며
native 이력에 client ID가 남는지 확인한다.

공식 문서:
- [앱 링크와 새 채팅 초안](https://learn.chatgpt.com/docs/reference/commands#deep-links)
- [공식 queue와 resume 동작](https://learn.chatgpt.com/docs/cli/reference#codex-queue)
- [App-server 프로토콜](https://learn.chatgpt.com/docs/app-server)
