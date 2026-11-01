# GUI 인계와 Codex 전용 tmux

## 매 실행마다 확인하는 GUI 재시작

`codexm`의 TUI에서 Enter 또는 `codexm resume ID --account NAME`을 사용한다.
잠금 없는 대화는 바로 공식 CLI로 넘기며, 도달 가능한 공식 공용 데몬의 대화는
그 소유자에게 선택한 대화만 보관하도록 요청한다.

별도 macOS GUI가 소유하면 다음 절차를 사용한다.

1. 대화의 writer 파일을 연 PID가 하나인지, 현재 사용자 소유인지, 실제 GUI의 자식인지
   확인한다. GUI는 `com.openai.codex`와 OpenAI 서명으로 검증한다.
2. 채팅 ID와 GUI PID를 보여주고 **GUI의 다른 작업도 중단되며 자동 재개되지 않음**을
   알린 뒤 `[y/N]`으로 확인한다. 매번 새 확인을 받는다. 거절·EOF·비대화형 입력은 취소한다.
3. `codexm-gui-reopen-*` 복구 세션을 `tmux -L codex`에 먼저 만들고 준비 응답을 확인한다.
   하나의 GUI 인계만 허용하며, 확인 중 소유자가 바뀌면 종료하지 않는다.
4. 검증한 GUI main PID에 SIGTERM을 보낸다. SIGKILL이나 다른 CLI/공용 데몬 종료는 없다.
   잠금 해제를 기다린 뒤 필요한 공식 unarchive를 수행하고 원래 ID를 resume한다.
5. 공식 CLI가 잠금을 획득하면 GUI를 다시 연다. CLI 시작 실패·요청 프로세스 소멸·75초
   복구 기한 만료에도 종료된 GUI를 다시 연다. GUI가 종료 요청에 응하지 않으면 인계를
   중단하고 계속 실행 중인 GUI는 그대로 둔다. 복구 세션은 종료 시 사라진다.

GUI가 실행하던 다른 대화는 자동으로 이어지지 않는다. 이 영향을 원하지 않으면 `N`을
선택하고 GUI에서 해당 채팅만 보관한 뒤 재시도한다. GUI 공용 데몬 모드를 강제로 켜거나
비공개 IPC/앱 번들을 변경하지 않는다. GUI 종료 실패·불명확한 RPC 결과를 성공으로 간주하지 않는다.

`--terminal`과 호환 웹의 분리 실행은 대화형 승인을 대신하지 않는다. 필요한 경우 위의
foreground resume 명령에서 인계한다. Linux와 독립 CLI writer는 소유자가 직접 해제한다.

복구 영수증은 `~/.local/state/codexm/native-cli/gui-restarts/`의 0600 파일이다.
GUI 자동 복구 자체가 실패하면 `result.json`의 이유를 확인하고 기록된 앱을
`open -a /Applications/ChatGPT.app`처럼 직접 연다. 앱 이름/경로는 설치에 따라 다르다.
복구 도우미는 잠깐만 실행하며 daemon이나 자동 승인 설정을 남기지 않는다.

## 전용 tmux 서버

새 codexm 터미널과 Codex 에이전트의 장시간 테스트/작업은 `codex` 소켓을 쓴다.
일반 tmux 별칭이나 전역 서버 설정은 바꾸지 않는다.

```sh
tmux -L codex ls
tmux -L codex new-session -s codex-project
tmux -L codex attach -t SESSION
tmux -L codex kill-session -t SESSION
codexm resume THREAD_ID --account NAME --terminal
codexm terminal codexm-cli-0123456789abcdef
```

세션 유지가 필요하면 위 전용 세션 안에서 `codexm`을 실행한다. 기본 foreground 실행은
계속 공식 CLI로 직접 exec하며 관리 서비스를 추가하지 않는다. GUI 복구용 임시 세션과
검증 세션은 작업이 끝나면 정리한다. 사용자 대화가 실행 중인 세션을 일괄 종료하지 않는다.

기존 실행 중인 default 서버의 codexm 세션은 그대로 유지한다. 이전 영수증에는 소켓
필드가 없으므로 `-L default`로 조회·입력·연결한다. 새 영수증은 `tmux_server: codex`를
기록한다. 다른 서버의 같은 pane 번호와 혼동하지 않으며, 종료 후 다시 열면 새 서버를 쓴다.
서버가 다르면 `switch-client` 대신 attach로 연결하며 다른 클라이언트를 끊지 않는다.
기존 agent-discord 등 이미 운영 중인 서비스도 이번 작업에서 임의 재시작하지 않는다.
