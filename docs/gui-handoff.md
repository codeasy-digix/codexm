# GUI·외부 exec 인계와 tmux 작업 공간

## 매 실행마다 확인하는 GUI 재시작

`codexm`의 TUI에서 Enter 또는 `codexm resume ID --account NAME`을 사용한다.
잠금 없는 대화는 바로 공식 CLI로 넘기며, 도달 가능한 공식 공용 데몬의 대화는
그 소유자에게 선택한 대화만 보관하도록 요청한다.

별도 macOS GUI가 소유하면 다음 절차를 사용한다.

1. 대화의 writer 파일을 연 PID가 하나인지, 현재 사용자 소유인지, 실제 GUI의 자식인지
   확인한다. GUI는 `com.openai.codex`와 OpenAI 서명으로 검증한다.
2. 채팅 ID와 GUI PID를 보여주고 **GUI의 다른 작업도 중단되며 자동 재개되지 않음**을
   알린 뒤 `[y/N]`으로 확인한다. 매번 새 확인을 받는다. 거절·EOF·비대화형 입력은 취소한다.
3. `codexm-gui-reopen-*` 복구 창을 default/codexm에 먼저 만들고 준비 응답을 확인한다.
   하나의 GUI 인계만 허용하며, 확인 중 소유자가 바뀌면 종료하지 않는다.
4. 검증한 GUI main PID에 SIGTERM을 보낸다. SIGKILL이나 다른 CLI/공용 데몬 종료는 없다.
   잠금 해제를 기다린 뒤 필요한 공식 unarchive를 수행하고 원래 ID를 resume한다.
5. 공식 CLI가 잠금을 획득하면 GUI를 다시 연다. CLI 시작 실패·요청 프로세스 소멸·75초
   복구 기한 만료에도 종료된 GUI를 다시 연다. GUI가 종료 요청에 응하지 않으면 인계를
   중단하고 계속 실행 중인 GUI는 그대로 둔다. 복구 창은 종료 시 사라진다.

GUI가 실행하던 다른 대화는 자동으로 이어지지 않는다. 이 영향을 원하지 않으면 `N`을
선택하고 GUI에서 해당 채팅만 보관한 뒤 재시도한다. GUI 공용 데몬 모드를 강제로 켜거나
비공개 IPC/앱 번들을 변경하지 않는다. GUI 종료 실패·불명확한 RPC 결과를 성공으로 간주하지 않는다.

`--terminal`과 호환 웹의 분리 실행은 대화형 승인을 대신하지 않는다. 필요한 경우 위의
foreground resume 명령에서 인계한다. GUI 재시작은 macOS에서만 지원한다.

복구 영수증은 `~/.local/state/codexm/native-cli/gui-restarts/`의 0600 파일이다.
GUI 자동 복구 자체가 실패하면 `result.json`의 이유를 확인하고 기록된 앱을
`open -a /Applications/ChatGPT.app`처럼 직접 연다. 앱 이름/경로는 설치에 따라 다르다.
복구 도우미는 잠깐만 실행하며 daemon이나 자동 승인 설정을 남기지 않는다.

## 외부 `codex exec` 인계

TUI에서 채팅·계정을 선택하거나 아래처럼 실행한다. macOS와 Linux의 foreground 터미널을
지원하며, 별도의 강제 종료 옵션이나 상주 서비스는 없다.

```sh
codexm resume THREAD_ID --account ACCOUNT
```

GUI 목록에 보이는 대화라도 실행 중인 writer는 별도 `codex exec`일 수 있다. 생성 기록의
`source=exec`만으로 종료하지 않는다. 현재 사용자의 정확한 native 실행 파일과 OS argv의
실제 `exec`/`e` 명령, PID 시작 식별자, 선택한 채팅 하나만 소유하는지 확인한다. 프롬프트나
설정 값에 들어 있는 `exec` 문자열은 명령으로 취급하지 않는다. 명령 인자·환경·프롬프트는
진단 출력이나 영구 기록에 저장하지 않는다.

확인 화면에는 채팅 ID, exec PID, 선택 계정과 현재 작업 중단의 영향을 표시한다. 기본값은
`N`이며 매 실행 다시 확인한다. 거절하거나 `yes | ...`처럼 입력을 넘기면 기존 작업을 유지한다.
승인 후 소유자와 대상 계정·프로젝트를 다시 검증하고 해당 PID에 SIGTERM을 한 번만 보낸다.
프로세스 종료와 writer 해제를 모두 확인한 뒤 같은 ID를 공식 CLI로 연다. 기본 resume는
새 지시를 자동 전송하지 않으며, 이미 수행한 외부 작업을 되돌리거나 재실행하지 않는다.

다른 CLI, 부모 실행기, 공용 app-server 및 daemon은 종료 대상이 아니다. 호출자가 해당 exec의
자식이면 자신의 종료를 승인할 수 없다. 실행기가 자동 재시작하여 writer를 다시 잡으면 새
CLI를 시작하지 않고 중단한다. 종료 거부·불확실한 결과에는 SIGKILL이나 재시도가 없다.
설치된 runtime과 다른 실행 파일, 해석할 수 없는 argv, 다중 writer는 소유자의 환경에서
먼저 해제해야 한다. 계정 자동 인계의 종료 권한을 외부 exec에 확대하는 기능은 아니다.

새 CLI를 준비하는 동안 다른 writer가 나타나도 자동 archive 또는 추가 종료로 넘겨받지
않는다. native lock/DB/rollout을 직접 바꾸지 않으며 채팅 ID, 기존 문맥·목표·예산을 유지한다.

## 단일 tmux 작업 공간

새 CLI·GUI 복구 helper·계정 인계 helper는 default 서버의 `codexm` 세션 안에 작업별 창으로 만든다.

```sh
tmux attach -t codexm
tmux -L default list-windows -t codexm
tmux -L default list-panes -s -t codexm
tmux -L default capture-pane -p -t codexm:WINDOW -S -30
```

기존 `codex` 서버와 default의 옛 영수증은 계속 읽는다. 다른 서버의 PTY는 실행 중에
이동할 수 없으므로 `legacy-*` 연결 창으로 접근한다. 다른 클라이언트를 떼어내거나
원본 CLI를 재시작하지 않는다. 새 작업부터 일반 창을 사용하고 이전 작업이 종료된 후
원본 창을 개별 정리한다. `codexm` 세션 전체를 종료하지 않는다.

GUI가 소유한 대화는 CLI 터미널 화면이 아니므로 이 경로로 연결하지 않는다.
터미널 밖에서 실행 중인 일반 CLI의 PTY 역시 강제로 가져오지 않는다.
GUI 종료 확인·기본 취소·다른 작업 중단 안내는 위 규칙을 그대로 유지한다.
