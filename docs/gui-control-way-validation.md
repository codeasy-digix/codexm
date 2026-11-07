# GUI 소유 실행에 명령 전달하기 — 실험 결과

검증일: 2026-11-07. 브랜치: `gui-control-way`, 기준: `502c62c`.
이 문서는 최초 타당성 시험 결과다. 이후 독립 CLI의 공식 deep-link + queue 경로를
구현한 [별도 GUI 제어 실험](gui-control-experiment.md)을 추가했다. 설치된 CodexM,
기본 CLI 재개 경로, GUI 종료 정책, 계정 선택 동작은 변경하지 않는다.

## 결론

**GUI가 소유하는 채팅은 GUI를 종료하거나 writer lock을 해제하지 않고 추가 지시를
받을 수 있다. 새 채팅 생성과 소유자가 없는 기존 채팅의 GUI 재개도 가능하다.**

다만 앱 내부의 공식 제어 도구로 가능한 것과, 독립 실행 파일 `codexm`에서 같은
경로를 호출할 수 있는 것은 별도 문제다. 이번 실제 GUI 시험은 공식 `codex-app-tools`
플러그인의 `create_thread` / `send_message_to_thread`를 사용했다. 일반 CLI에서
이 앱 도구를 직접 호출할 수 있는 공개 로컬 endpoint는 확인하지 못했다.

다른 CLI가 이미 writer를 소유하면 GUI의 명령 전달도 충돌한다. 이 방식이 모든
실행 소유권 문제를 없애는 것은 아니다.

## 실제 GUI 시험

macOS arm64의 ChatGPT 데스크톱 내 Codex GUI, 번들 CLI `0.167.0-alpha.3.2`에서
사용자가 승인한 임시 채팅 하나로 수행했다. 기존 실제 작업에는 시험 명령을 보내지 않았다.

| 조건 | 수행한 시험 | 관측 결과 |
| --- | --- | --- |
| 새 채팅 | 앱의 `create_thread`로 표식 응답 요청 | GUI 소유 서버가 새 ID를 만들고 응답 완료 |
| GUI에 로드된 idle 채팅 | `send_message_to_thread` | 같은 ID에 새 턴으로 응답 완료 |
| GUI 실행 중 | active 상태를 확인하고 후속 표식 전달 | 동일 턴에 후속 입력이 들어가 최종 답변에 반영 |
| 로드되지 않은 기존 채팅 | 임시 채팅만 공식 보관·복원 후 writer 부재 확인, 추가 지시 전송 | GUI가 같은 ID를 다시 로드하고 첫 응답의 표식까지 기억함 |
| 외부 CLI 소유 | 같은 임시 채팅을 직접 CLI로 열고 GUI에서 추가 지시 전송 | `already has an active writer`로 거절, CLI가 계속 소유 |
| CLI 종료 후 기존 채팅 | 시험에서 직접 만든 CLI만 종료, writer 해제 확인 후 새로운 표식 전달 | GUI가 같은 ID와 이전 문맥으로 응답 완료 |

GUI main PID와 GUI app-server의 PID·시작 시각은 시험 내내 같았다. GUI를 재시작하거나
실제 작업 프로세스를 종료하지 않았다. CLI 소유권 시험의 종료 대상은 이 시험에서 만든
임시 채팅의 전용 CLI뿐이었다. native lock, SQLite, rollout을 직접 수정하지 않았다.
시험 완료 후 임시 채팅은 보관했고, 결과 확인을 위해 삭제하지 않았다.

앱 도구의 메시지는 `Sent by Codex from task`에 해당하는 위임 입력으로 기록되었다.
일반 사용자의 composer 입력과 같은 표시 형식이라고 가정하면 안 된다. 호출 성공만
확인한 것이 아니라 실제 턴의 `completed` 상태와 최종 표식까지 확인했다.

## 격리한 공식 런타임 시험

`internal/nativecli/gui_control_experiment_test.go`는 기존 Go RPC 시험 도구를 재사용한다.
새 제품 모듈이나 상주 제어 서비스를 추가하지 않는다. 임시 HOME·별도 저장소·로컬 가짜
모델 서버를 사용하며 운영 인증정보를 복사하거나 실제 모델 API를 호출하지 않는다.
standalone `0.167.0`과 GUI 번들 `0.167.0-alpha.3.2`에서 다음을 확인했다.

- GUI와 같은 stdio app-server가 writer를 가진 동안, **같은 저장소를 사용하는 별도
  시험용 Unix app-server**를 통해 공식 `codex queue --remote unix://...`로 입력을 등록했다.
  GUI형 소유 서버가 이를 소비했고 PID와 writer는 유지됐다. 서로 다른 저장소나 다른
  장비로 전달되는 시험이 아니다.
- busy 상태의 큐 입력은 현재 턴이 끝난 뒤 각각 다음 턴으로 실행됐다. 로드된 idle
  채팅의 큐 입력도 실행됐다. 이웃 채팅의 완료 이력은 같았다.
- 같은 텍스트를 두 번 명시적으로 제출하면 두 턴이 실행됐다. 텍스트가 같다는 이유로
  중복 요청이 제거되지는 않는다.
- **로드되지 않은 채팅은 큐 등록만 성공하고 실행되지 않았다.** 원래 시험용 소유
  서버가 공식 `thread/resume`으로 다시 로드한 뒤에야 큐 입력을 처리했다.
- 소유 서버에 `turn/steer`를 보내면 같은 turn ID를 유지했다. 오래된 `expectedTurnId`,
  idle 턴, 해당 채팅을 소유하지 않는 다른 서버로 보낸 steer는 거절됐다.
- 소유 서버가 Unix transport를 제공하는 경우에는 별도 클라이언트의 steer도 동작했다.
  stdio만 쓰는 GUI에 외부 CLI가 같은 방식으로 접속할 수 있다는 증거는 아니다.

현재 `0.167.0`에서는 `codex --no-daemon queue`가 거절됐다. shared server를 발견해야
한다는 오류가 반환된다. 시험에서는 기존 운영 daemon을 시작하거나 설정을 바꾸지 않고,
임시 서버에 `--remote`를 명시했다. 실제 GUI의 기본 stdio 서버를 Unix listener로
재설정하지 않았다.

## GUI 중심 통합 시의 경계

권장 제어 흐름은 다음과 같다.

1. 새 채팅이면 앱의 공식 생성 경로로 GUI에서 시작한다.
2. 기존 채팅에 writer가 없으면 앱의 공식 재개 경로로 로드한 뒤 지시한다.
3. GUI 소유 채팅이면 종료·archive 없이 해당 GUI 실행에 전달한다. 현재 작업 수정은
   steer, 작업 완료 후 추가 실행은 queue로 구분한다.
4. 다른 CLI 소유라면 자동 GUI 전환으로 취급하지 않는다. 기존 CLI에 연결하거나,
   별도로 허용된 인계로 원래 writer를 해제해야 한다.

**이 최초 시험에서는 독립 CodexM의 GUI 제어 API를 구현하지 않았다.**
앱 도구는 이 앱 세션에서 제공되며, 일반 프로그램용 connection 정보를 노출하지 않는다.
`codex app` 명령은 앱/작업 디렉터리 열기 기능이고, 이 버전의 도움말에는 특정 GUI
채팅의 create/start/steer를 수행하는 하위 명령이 없다. 앱이 제공하는 지원된 연결
방법이 있어야 독립 CLI·메뉴 앱을 같은 직접 제어 경로에 연결할 수 있다.
비공개 앱 IPC 추측, 앱 번들 패치, 키 입력으로 전송 버튼을 강제 누르는 방식은 도입하지 않았다.

후속 구현은 이 내부 도구 연결 대신 공식 앱 링크와 native queue를 조합한다.
기존 채팅의 명령 전달 범위와 새 초안의 제한은 [후속 실험 문서](gui-control-experiment.md)를 따른다.

`queue --resume`은 로컬 daemon 또는 지정한 remote server에서 재개를 시도한다.
**GUI에서 재개한다는 의미가 아니다.** 또 resume가 실패해도 queue 명령은 성공 종료할 수
있으므로 GUI 통합의 성공 판정으로 사용할 수 없다.

## 부작용과 아직 검증하지 않은 것

- 전달은 기존 GUI 실행의 인증을 사용한다. 시험에 사용한 앱 도구에는 계정 선택 인자가
  없다. CodexM의 채팅별 계정 선택을 이 경로로 그대로 옮길 수 있다고 결론 내리지 않는다.
- 모델·effort·권한·cwd override를 추가하지 않았다. 공식 `turn/start`의 일부 override는
  이후 턴의 기본값으로 남을 수 있으므로 단순 메시지 전달에 섞지 않는 것이 적절하다.
- steer는 현재 실행 방향을 바꾼다. 이미 수행된 작업을 취소하거나 되돌리는 기능이 아니다.
  독립적인 후속 작업은 queue와 구분해야 한다.
- 전송 ACK와 작업 완료는 다르다. 결과가 불명확할 때 자동 재전송하면 중복 실행할 수 있다.
  상태를 `미전송 / 거절 / 접수 / 처리 확인 / 결과 불명`으로 구분하고 원래 요청을 보존해야 한다.
- 실제 GUI에서 승인 대기·첨부파일·작성 중인 composer 초안·동시 사용자 입력·앱 종료 중
  전달·다른 계정/원격 호스트 전환은 검증하지 않았다. 모든 상황에서 부작용이 없다고
  주장하지 않는다. 격리된 이웃 채팅 보존 시험과 운영 GUI 전체 무영향 보장은 다르다.

## 재현

시험은 명시적으로 활성화해야 하며, 기본 Go test에서는 건너뛴다. writer PID 검증에
`lsof`가 필요하다. tmux default 서버의
`codexm` 작업 공간에서 이름을 붙인 시험 창으로 실행한다.

```sh
CODEXM_GUI_CONTROL_TEST=1 \
CODEXM_NATIVE_RUNTIME=/absolute/path/to/official/codex \
go test -race ./internal/nativecli -run '^TestGUIControl' -count=1 -v
```

실제 GUI 시험의 대화 식별자·프로세스 관측값·원시 앱 응답은 비공개 로컬 validation
자료로 보존하며 공개 저장소에는 포함하지 않는다. 실제 GUI 시험은 사용자 승인 아래
별도의 임시 채팅에서만 반복해야 한다.

공식 근거:

- [Codex CLI reference: queue](https://learn.chatgpt.com/docs/cli/reference#codex-queue)
- [App-server: lifecycle and turn/steer](https://learn.chatgpt.com/docs/app-server)
- [Prompting: steering and queuing](https://learn.chatgpt.com/docs/prompting)

## 2026-11-08: 계정별 공식 daemon

macOS arm64에서 공식 packaged CLI `0.167.0`과 태그가 적용된 별도 codexm 빌드로
검증했다. 운영 로그인 대신 synthetic ChatGPT 계정 두 개와 loopback 계정/routing·
모델 응답 서버를 사용했다. 모델 제공자/프롬프트/프로젝트는 격리 저장소에만 설정했다.

| 항목 | 관측 결과 |
| --- | --- |
| 계정별 `daemon start` | 서로 다른 home, Unix socket, PID, 현재 user/workspace 확인 |
| 실행 중인 daemon 다시 등록 | 기존 PID와 시작 시각 유지; start/restart 재호출 없음 |
| 같은 ID A → B 인계 | A의 첫 턴 이력을 유지하고 B의 다른 인증으로 두 번째 턴 완료; fork 없음 |
| 다른 채팅 동시 실행 | A의 이웃 채팅이 active 상태와 원래 writer를 유지 |
| 활성 턴 인계 | 기본 요청은 거절; `--interrupt` 요청은 선택한 턴만 중단하고 B → A 인계 |
| 같은 request ID 재호출 | 기존 상태/시각 반환; archive나 continuation 재전송 없음 |
| unknown 요청 | 새 ID로 우회한 재시도도 거절; 영수증을 유지 |
| 자식 채팅/대기 입력 | preflight 검사에서 거절하는 기본 테스트 통과 |
| 실제 native CLI의 PTY 제거 | frontend PID 종료 뒤에도 동일 daemon/turn ID가 active를 유지하고 완료 |
| native CLI에 SIGTERM | 검증한 frontend PID만 종료; daemon과 모델 턴 유지 |
| 컨트롤러 독립성 | seed 구독을 닫고 시험; 모델 완료 동안 관측용 연결도 닫은 뒤 재연결하여 확인 |
| GUI 방식의 `/rpc` Unix WebSocket | initialize 성공 |
| 공식 `app-server proxy --sock` | stdio의 raw WebSocket 터널로 같은 daemon의 채팅 조회; proxy EOF 종료 확인 |
| 실제 운영 GUI | main/app-server의 PID와 시작 시각 유지; 기본 daemon 소켓 생성 없음 |

PTY 시험은 tmux 뷰어에서 detach하는 데 그치지 않고, 시험용 native frontend가
실제로 사라지는 것까지 확인한다. 단순 연결 유지, 새 턴의 자동 재실행이나 tmux
프로세스 생존을 daemon의 작업 유지라고 잘못 판단하지 않도록 turn ID도 비교한다.

기본/실험 build tag 각각의 전체 Go race 테스트와 vet가 통과했다. 기본 빌드에서
`gui`와 `daemon` 진입점이 활성화되지 않는 테스트가 있다. Rust/Swift/웹 코드는
이 변경에서 수정하지 않았다. 공식 runtime은 수집된 시험용 임시 디렉터리에서만
시작·정리하며 운영 바이너리와 Homebrew는 교체하지 않는다.

연속 PTY 시험에서 idle unload가 loaded 목록 제거와 writer 해제를 순차 처리하는
경계를 확인했다. attach에 한해 검증된 같은 daemon의 해제를 짧게 관찰하도록
보완한 뒤 전체 daemon 시험을 함께 통과했다. archive에는 계속 loaded 증명이 필요하다.

실제 GUI의 서버 연결 변경은 시험하지 않았다. 현재 GUI는 config override가 있는
별도 stdio 서버이며, 관찰한 앱의 local-daemon 분기 조건에 맞지 않는다. protocol
호환성 성공을 GUI 전환 성공으로 표시하지 않는다. GUI 계정별 서버 선택과 실제
OpenAI 인증/과금·quota, 새 daemon 경로의 Discord 답장 전달은 이번 격리 시험의
검증 결과에 포함되지 않는다. 세부 사용법과 제한은 [daemon-experiment.md](daemon-experiment.md).
