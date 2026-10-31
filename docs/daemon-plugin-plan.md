# codexm 데몬·로컬 ChatGPT 플러그인 전환 계획

> 과거 계획 기록입니다. 아래 `.home`의 revision pin·제품 설치 호출 방식은
> 2026-11-01 제거했습니다. 현재 소유권은 [설치 관리](deployment.md)를 따릅니다.

작성: 2026-10-12, Asia/Seoul. 상태: 구현 전 기준 문서. 사용자가 계획 저장 후 구현까지 승인했다.
이 문서는 공개 배포 계획이 아니라 개인 로컬 설치와 검증을 위한 작업 계획이다.
단계별 실제 결과와 검증되지 않은 사항은 별도 실행 기록에 남긴다.

## 1. 목표와 완료 조건

1. codexm 관리 서비스를 macOS의 사용자 launchd 서비스로 실행한다. Ubuntu용은
   systemd 사용자 서비스 정의와 동작을 지원하되 이번 실장비 적용은 로컬 Mac만 한다.
2. 관리 서비스와 새 작업 엔진의 정상 운영에서 tmux가 필수 의존성이 되지 않게 한다.
3. 계정 우선순위, 한도 소진 후 동일 대화 재개, fallback 계정에서 10분마다 복귀 확인,
   수동 중지와 목표 예산 존중, 관리자 모델의 명시적 호출 원칙을 유지한다.
   계정 관리가 제품의 핵심이다. 선택한 세션만 계정을 바꾸는 메뉴/CLI/API를 제공하고,
   중복 요청·전환 도중 재시작·한도 관측 실패에도 계정 정책과 실행 의도를 보존한다.
4. ChatGPT 플러그인 안에 작업 목록, 대화·추가 지시, 설정 편집, 계정·사용량·로그를 제공한다.
5. 독립 웹사이트, 브라우저용 제품 화면, 공개 수신 포트와 공용 배포는 만들지 않는다.
6. CLI·서비스·플러그인을 기존 `codeasy-digix/codexm` 한 저장소에서 버전 관리한다.
   `.home`은 해당 제품의 고정 revision 설치·호스트 통합을 담당한다.
7. 개인 로컬 marketplace 설치·업데이트·제거·진단 절차를 제공하고 다른 플러그인을 보존한다.
8. 문서·소스·검증 결과를 커밋하고 기존 비공개 원격 저장소에 푸시한다.

완료의 증거는 코드 존재만으로 대체하지 않는다. 실제 OS 서비스의 PID·상태, 격리한
프로세스 복구 시험, MCP 협상과 도구 호출, 실제 또는 격리된 GUI 조작 결과,
로컬 설치 receipt와 Git 원격 revision을 각각 확인한다. ChatGPT 앱 안에서 확인하지
못한 화면은 모의 호스트 시험 통과와 구별한다.

## 2. 현재 기준선

- 제품: Go `github.com/codeasy-digix/codexm`, 0.2.1, 시작 commit
  `a225b84c40fe7e325a0d563394aed4c7528e1c91`.
- `.home`: 시작 commit `37b7315aa8ae64be35e6057dd91bf0f3e4c99fc2`.
  다른 작업의 manifest·SSH·에디터·markdown 도구 변경이 있으므로 별도 worktree에서 작업한다.
- supervisor는 Unix socket HTTP RPC 서버이고 시작 함수가 tmux 세션을 만든다.
  종료 정리와 작업 생존 판단도 tmux 식별자에 결합되어 있다.
- 작업 데이터·이벤트·실행 회차·계정 변경은 codexm DB에 저장한다.
  전체 대화 목록은 Codex SQLite를 읽기 전용으로 직접 조회한다.
- `/settings`는 현재 조회만 가능하다. 입력 포커스 상태의 위·아래 키도 목록 이동에 사용한다.
- 공식 `codex`는 별도 원본 명령이다. 제품 실패로 공식 실행이 막혀서는 안 된다.
- 공식 공용 Codex daemon, codexm supervisor, 개별 작업 app-server는 서로 다른 프로세스다.
  새 서비스 이름과 도움말에서도 이 구분을 유지한다.

주요 코드: `internal/supervisor/{server,actions,reconcile,api}.go`,
`internal/runtime/{launch,rpc,engine}.go`, `internal/config`, `internal/store`,
`internal/catalog`, `internal/terminal`, `internal/tui`, `install.sh`.

## 3. 제품과 Git 소유권

기존 저장소를 monorepo로 발전시킨다. CLI와 plugin을 별도 Git 저장소로 분리하거나
submodule로 묶지 않는다. 프로토콜·서버·UI를 한 commit에서 수정하고 검증할 수 있어야 한다.
현재 Go 모듈 경로와 CLI 명령은 유지하고 불필요한 전체 파일 이동은 피한다.

```text
codexm/
  cmd/codexm/                  # 단일 CLI 진입점
  internal/supervisor/         # 작업·계정·복구 정책
  internal/service/            # launchd/systemd 사용자 서비스 수명주기
  internal/runtime/            # 공식 Codex 실행과 소유권 확인
  internal/plugin/             # 로컬 설치와 MCP/UI 연결
  plugin/                     # manifest, MCP 설정, skill, UI 소스/자산
  docs/                       # 계획, 아키텍처, 사용법, 실측 검증 기록
  install.sh, build.sh         # 같은 revision의 실행 파일·플러그인 묶음
```

서비스와 플러그인은 하나의 제품 버전을 사용한다. 관리 API/저장 구조/MCP 계약의
버전은 따로 표시하여 호환 가능 여부를 판단한다. 설정에 credential이나 실제 대화,
로그, DB, socket, 인증 캐시, 앱 내부 데이터를 커밋하지 않는다.
공개 release/tag/npm/tap 또는 marketplace 제출은 이번 작업 범위에 없다.

## 4. .home과 설치 경로

- 제품 소스·서비스 정의 생성·plugin lifecycle은 codexm 저장소가 소유한다.
- `.home/config/home/codexm-revision.txt`는 검증한 제품 commit을 가리킨다.
- `.home/tools/codexm/`은 단일 제품 installer를 호출한다. 구현을 복사하지 않는다.
- `manifests/home.json`에 명령, 원본, macOS/Linux 범위, 의존성, 설정·상태 경로,
  서비스와 플러그인 진단 명령을 등록한다.
- 실행 파일은 기존 `~/.local/bin/codexm`과 불변 version 디렉터리를 사용한다.
- 설정은 `~/.config/codexm/config.toml`, 상태는 `~/.local/state/codexm/`,
  엔진과 설치한 플러그인 자산은 `~/.local/share/codexm/` 아래에 둔다.
- 사용자 launchd plist는 `~/Library/LaunchAgents/`, Linux unit은
  `~/.config/systemd/user/`에 둔다. 실제 절대 경로를 안전하게 인코딩한다.
- 개인 marketplace는 공식 형식을 사용하되 기존 항목을 병합·보존한다.
  제품 설치 receipt에는 codexm이 소유한 항목과 자산만 기록한다.
- symlink 교체는 원자적으로 한다. 사용자 수정 파일은 백업하고 실패하면 이전 버전으로 복구한다.
- 공식 Codex 설치가 없으면 기존처럼 먼저 설치하라는 안내를 제공한다.

## 5. 서비스 설계와 명령 구분

제안 명령:

```text
codexm service install|start|stop|restart|status|logs|uninstall
codexm plugin install|status|doctor|uninstall
codexm plugin mcp
```

기존 `codexm daemon ...`은 공식 공용 Codex daemon용 명령임을 명확히 한다.
새 manager service와 같은 의미로 사용하지 않는다.

- macOS는 로그인 사용자 LaunchAgent: 사용자 인증·파일 권한을 그대로 사용한다.
  root 서비스나 로그인 전 실행을 보장하는 설계로 확대하지 않는다.
- OS는 codexm의 시작·비정상 종료 복구를 담당한다. codexm은 자기 작업만 관리한다.
- 정상 서비스 모드에서는 UI가 모두 닫혀도 supervisor가 종료되지 않는다.
  모델을 호출하지 않는 유휴 감시이며, 완료한 worker를 불필요하게 유지하지 않는다.
- 재시작 간격/실패 원인/상태를 노출하고 반복 실패에 backoff를 적용한다.
- 명시적 stop/uninstall은 재실행 의도를 해제한다. 강제 종료와 사용자의 중지를 구별한다.
- 서비스 제어는 일반 사용자의 권한으로 수행한다. 전역 `killall codex`를 사용하지 않는다.
- foreground 실행은 개발·격리 시험용으로 제공한다. 임시 장기 시험은 이름 있는 tmux에서
  실행하고 시험 종료 시 정리한다. 실제 제품 서비스는 사용자 요청에 따라 OS가 관리한다.

## 6. 작업 프로세스 관리

tmux 자체를 작업 정체성으로 삼지 않는다. 기존 job ID, thread ID, run ID는 유지하고
프로세스 운반 방식만 교체한다.

- 새 작업은 독립 실행 프로세스로 공식 app-server를 시작한다.
- run ID, 계정 nickname, socket, engine revision, PID와 소유권 증거를 private receipt에 저장한다.
- PID 하나만으로 생존/소유권을 판단하지 않는다. 재사용된 PID나 다른 세션을 종료하지 않는다.
- 종료는 해당 작업의 interrupt/응답 확인 → 자신이 소유한 프로세스 정리 → writer lock 해제
  확인 → 새 실행의 준비 확인 순서로 수행한다.
- 서비스가 종료되거나 재시작됐을 때 기존 worker에 재접속할 수 있는 경로를 갖춘다.
  전달 여부가 불명확한 지시는 자동으로 중복 전송하지 않는다.
- 자식 프로세스/프로세스 그룹/실행 파일 소유권을 시험한다. DB에 남은 stale PID나
  tmux 이름 때문에 새 작업이나 unrelated 프로세스를 죽이지 않게 한다.

기존 작업은 가능한 한 그대로 인계한다. 특히 이번 구현을 수행하는 활성 대화의 engine을
중단하여 자기 작업을 끊지 않는다. 기존 tmux worker를 즉시 교체할 수 없으면 기존 실행은
legacy transport로 추적하고 자연 종료·명시적 재개·계정 전환 시 새 backend로 이동한다.
이 과도 상태는 로그/진단에서 명시하며 영구적인 tmux 신규 생성으로 우회하지 않는다.

## 7. 동시 실행·장애·상태 보존

- 상태 디렉터리별로 하나의 supervisor만 쓰기 권한을 가진다. 시작 시 migration/recovery도
  소유권 잠금을 확보한 뒤 수행한다.
- 살아 있는 다른 native writer가 있는 대화는 `external_writer`로 표시하고 강탈하지 않는다.
- native DB는 읽기 전용이다. codexm 전용 테이블을 native SQLite에 생성하지 않는다.
- native 대화·계정 인증·목표 텍스트·예산·누적 사용량을 유지한다.
- quota, 일시적 rate limit, goal budget, 사용자 pause, 인증 실패를 각각 다르게 처리한다.
- fallback 허용은 세 우선 계정이 모두 소진된 경우이다. 확인 실패/unknown을 소진으로
  취급하지 않는다. account-backup 사용 중 600초마다 상위 계정 복귀 가능성을 확인한다.
- 서비스/GUI 재연결은 마지막 입력을 임의로 다시 보내는 이유가 되지 않는다.
- 설정과 명령 변경에는 revision/idempotency 또는 이에 준하는 중복 방지를 적용한다.

## 8. API와 이벤트 전달

관리 코어의 권한은 로컬 사용자 Unix socket에 둔다. 플러그인은 제한된 MCP 도구로 연결한다.
브라우저가 native app-server에 직접 붙거나 account token을 받는 구조는 사용하지 않는다.

필요 기능: 상태/health, 전체 대화 검색·페이지 조회, 관리 작업 목록, 새 작업 생성,
기존 대화 등록, 대화 내용과 진행 상태, 추가 입력/steer, pause/resume/stop,
승인·질문 응답, 계정 관측, 전체·작업별 로그, 설정 조회·검증·저장.

이벤트 전달은 모델을 호출하지 않는다. 도구/브리지 지원에 맞춰 cursor 기반 증분 조회나
stream을 사용하며, 화면이 닫힌 동안에도 상태를 DB에 보관한다. live delta는 화면 성능을
위해 묶고, 최종 상태와 작업 명령 결과는 저장·재조회한다. 재접속 시 sequence와 snapshot을
대조한다. native stream 자체가 replay를 보장한다고 가정하지 않는다.

작업 목록 응답에 모든 대화 본문·목표·인증을 섞지 않는다. 사용자가 선택한 대화의 내용을
필요한 때 읽는다. MCP의 UI 전용 metadata도 자격 증명 보관 장소로 사용하지 않는다.

## 9. 공식 플러그인과 호환성 경계

공식 MCP Apps의 resource/bridge를 기본으로 삼는다. ChatGPT 전용 기능은 별도 얇은
adapter로 묶고 capability를 확인한 후 사용한다.

- 도구 UI: `_meta.ui.resourceUri`, `text/html;profile=mcp-app`.
- ChatGPT sidebar/thread entrypoint: 공식 `openai/ui` metadata.
- 버튼/폼은 MCP 도구를 직접 호출한다. 새 manager 모델 turn을 발생시키지 않는다.
- plugin-specific 설정 화면과 전체 관리 화면을 연결한다.
- 같은 앱 안의 일반 ChatGPT 계정과 codexm manager/worker 계정은 별개다.
- 공식 composer에 메시지를 보내는 기능과 codexm worker에 지시를 보내는 기능을 혼동하지 않는다.
- ChatGPT 번들, 비공개 IPC, DOM 주입, 인증 파일 변조에 의존하지 않는다.
- 메뉴가 아직 제공되지 않는 host에는 지원 여부를 명확히 표시한다.

공식 문서는 로컬 marketplace 설치를 지원한다. 다만 로컬 bundled MCP 도구 지원과
그 도구의 sidebar UI 렌더링이 실제 설치된 ChatGPT에서 함께 동작하는지는 직접 검증한다.
플러그인을 로컬에서 설치할 수 있다는 사실을 모든 GUI surface 지원의 증거로 삼지 않는다.
로컬 GUI 연결에 등록형 HTTPS/터널이 요구되는 경우 이를 숨기지 않고 실제 제약으로 기록한다.
사용자 요청 없는 공개 서버, 터널 계정 생성, API 결제 설정으로 문제를 우회하지 않는다.

## 10. GUI 정보 구조

```text
┌ 작업/대화 목록 ─────┬ 선택한 작업의 대화·실행 내용 ──────┬ 상세/작업 도구 ┐
│ 검색                │ 제목 · 경로 · 계정 · 상태          │ 계정/모델      │
│ 실행 중 / 승인 대기 │ 메시지 / 도구 실행 / 변경 파일     │ 목표/예산      │
│ 프로젝트/계정 필터  │ 승인·질문 응답                     │ 실행 회차      │
│ 전체 / 관리 중      │ 추가 지시 입력                     │ 작업별 로그    │
├ 설정 · 계정 · 로그 ─┴───────────────────────────────────┴───────────────┤
│ 서비스 연결 상태 · 로컬 호스트 · 마지막 관측 시간                        │
└────────────────────────────────────────────────────────────────────────┘
```

좌측 목록 폭은 조절하거나 좁은 화면에서 접을 수 있게 한다. 선택과 입력 초안을 보존한다.
검색 입력과 대화 입력은 별개이며, 현재 메시지가 어느 작업에 전달될지 제목/계정을 표시한다.
목록 행과 작업 버튼의 click propagation을 구분하여 잘못된 세션으로 전송하지 않게 한다.
관찰 중인 native 대화와 codexm이 실행을 소유한 작업을 구분한다.

## 11. GUI에서 제공할 조작

- 새 대화/작업: 작업 폴더, 제목, 계정, 종류(chat/task/goal), 선택적 모델·권한을 선택한다.
- 기존 대화: 검색·선택·미리보기 후 계정을 선택하여 관리/재개한다.
- 대화: 메시지와 실행 결과, 진행 중 표시, 추가 지시/steer, 중지·재개를 제공한다.
- 승인/질문: 원래 request ID와 실행 회차를 확인하고 stale 응답을 보내지 않는다.
- 모델·계정 선택은 실제 사용 가능 값만 표시한다. 기본값 변경이 기존 실행을 바꾸지 않게 한다.
- 목표가 없는 대화에 goal을 자동 생성하지 않는다. goal 완료 후 추가 요청은 일반 대화로 처리한다.
- 계정 카드: 로그인 상태, 한도 관측값, 갱신 시각, reset 시각, 재조회 동작을 제공한다.
- 로그: 전체/선택 작업/계정/기간 필터, 계정 변경 이유, 실행 시간, 사용량 관측 범위를 표시한다.
- 사용량 비율은 계정 전체 관측값이며 특정 세션이 소비한 정확한 비율로 표시하지 않는다.
- 상태가 오래되었거나 연결이 끊겼으면 빈 목록이나 사용량 0으로 오해시키지 않는다.
- 확장 이득: 즐겨찾기/최근 작업, 프로젝트 그룹, 검색 결과 수, copy ID/path,
  키보드 접근, 상태 배지, 계정 전환 타임라인을 우선 검토한다.
- 일괄 파괴 동작과 새 외부 알림 서비스는 이번 범위에 추가하지 않는다.

## 12. 설정 편집

설정은 raw JSON 편집 대신 명시적인 필드·설명·검증 오류·저장 결과를 제공한다.

| 영역 | 예시 | 적용 시점 |
|---|---|---|
| 계정 | manager, 기본 worker, 우선순위, fallback | 새 선택/다음 전환 |
| 복구 | fallback 재확인 주기, 상태 대조 주기 | 실행 중 안전하게 갱신 또는 재시작 필요 표시 |
| 실행 | 동시 작업 수, 작업 경로 충돌 정책 | 새 작업/스케줄링 |
| 로그 | 사용량 관측 주기 | 다음 관측부터 |
| UI | 필터/선택/초안/테마 | UI 전용 |

읽은 revision을 함께 저장하여 다른 클라이언트의 변경을 덮어쓰지 않는다.
검증은 서버에서 수행하고 쓰기는 원자적으로 한다. 허용 목록에 없는 runtime/auth 경로 변경은
일반 GUI 설정 저장 API로 받지 않는다. 인증 파일 내용은 UI에 반환하지 않는다.
저장 후 읽기 검증과 적용 범위를 보여준다. 기본값 복원은 적용 전 변경 내역을 표시한다.

## 13. 입력과 접근성

- 입력창 위·아래 키는 커서/명령 히스토리에 사용하고 목록 이동을 가로채지 않는다.
- 목록에 focus가 있을 때 위·아래로 선택한다. slash palette가 열렸을 때만 메뉴 탐색한다.
- 이전 입력 검색/선택을 제공하고 미전송 초안을 보존한다.
- 한국어 IME composition 중 Enter로 전송하지 않는다. Shift+Enter는 줄바꿈이다.
- 포커스 표시, label, aria 상태, 키보드만으로 설정 저장/취소가 가능해야 한다.
- 좁은 패널/전체 화면, 밝은/어두운 테마를 지원한다. 진행 중 갱신으로 스크롤을 강탈하지 않는다.

## 14. 보안과 개인 로컬 운영

- localhost라는 이유만으로 인증·소유권 확인을 생략하지 않는다. 주 제어 채널은 사용자 전용 socket이다.
- 코드/파일 경로/메시지는 shell 문자열로 이어붙이지 않는다. OS 서비스 정의도 XML/unit escaping을 한다.
- plugin 도구는 범위를 제한하고 임의 shell 명령 실행 도구로 만들지 않는다.
- credential, webhook, 원본 환경 변수, 실제 goal 본문이 진단 출력·테스트 artifact·Git에 새지 않게 한다.
- HTML 렌더링은 대화 내용의 script/HTML을 실행하지 않도록 한다.
- plugin install/uninstall은 다른 marketplace와 MCP 설정을 삭제하지 않는다.
- 상태 디렉터리는 0700, credential/receipt/민감 로그는 0600을 기본으로 한다.

## 15. 업데이트 정책

ChatGPT가 갱신되어도 앱 원본에 패치할 필요가 없는 공식 확장만 사용한다.
호환 불가 상태는 진단에서 설명하고 작업 서비스는 계속 동작한다.
Codex engine은 현재처럼 호환성 확인 후 고정한다. 실행 중 engine을 자동 교체하지 않는다.
프로토콜 변경은 adapter에서 흡수하고 미지원 필드는 fail-safe로 처리한다.
로컬 plugin은 불변 버전 자산 + 원자적 활성 경로 + 이전 버전 receipt로 설치한다.
service/plugin/API 버전 불일치를 진단한다. 업데이트 실패를 사용자 대화 손상으로 연결하지 않는다.

## 16. 순차 작업과 통과 기준

### P0 — 계획과 기준선

- 이 계획을 먼저 커밋·푸시한다.
- 실제 운영 파일은 read-only 기준선으로 확인하고 비밀 없는 fingerprint만 기록한다.
- 독립 worktree에서 코드 작업을 진행한다.

### P1 — 서비스·worker 수명주기

- OS 서비스 정의/설치·상태·명시적 stop/복구 구현.
- tmux와 독립된 신규 worker, 소유권 검증, 종료/재접속 구현.
- 상태 잠금 이전의 recovery 쓰기를 제거한다.
- 격리 상태 디렉터리에서 강제 종료 복구·stop 유지·중복 supervisor 방지를 검증한다.
- 이 단계의 runtime 검증이 끝나기 전에 실제 plugin을 연결하지 않는다.

### P2 — 관리 API와 설정·이벤트

- GUI에 필요한 최소 데이터 응답, 대화·입력·승인 연결, 설정 revision 저장을 구현한다.
- 재접속·중복 입력·stale 승인·settings 충돌과 invalid 필드를 시험한다.
- 독립 웹 UI 서버 없이 MCP에서 기존 코어 API를 호출한다.

### P3 — 로컬 plugin·GUI

- manifest, 로컬 marketplace, 설치/제거 receipt, MCP handshake/tools/resources를 구현한다.
- 공식 sidebar/thread UI를 연결하고 검색·목록·입력·설정·계정·전체 로그 화면을 구현한다.
- 브리지 기능 검출, 연결 실패·재시도·명확한 미지원 상태를 제공한다.
- 자동화된 UI 조작 시험과 실제 ChatGPT 내 설치/노출/도구·화면 확인을 분리 기록한다.

### P4 — 로컬 제품 전환과 .home 등록

- native Codex와 기존 데이터 보존을 확인한 뒤 새 실행 파일을 설치한다.
- 기존 supervisor의 실제 PID·실행 경로·현재 dispatch 상태를 확인해 중복 없는 인계를 수행한다.
- 활성 worker를 보존하고 새 service가 동일 run에 재접속하는지 확인한다.
- `.home` pin/registry/진단 문서를 변경한다. 다른 작업의 manifest 수정은 보존하며 병합한다.
- 로컬 plugin을 설치하고 설치·업데이트·제거 방법과 서비스 제어 명령을 문서화한다.

### P5 — 최종 검증과 정리

- `go test -race ./...`, `go vet ./...`, 관련 공식 native 통합 시험을 실행한다.
- 서비스 복구/명시적 stop, plugin 계약·GUI, native DB read-only, 실제 설치 경로를 확인한다.
- 임시 worker·tmux·테스트 서비스·테스트 인증 사본만 정리한다.
- 제품과 `.home` 변경을 검토·커밋·푸시하고, 실제 완료/제약/남은 수동 조작을 기록한다.
- 다른 장비 적용과 공개 배포는 이 로컬 전환을 검증한 뒤 별도 단계로 취급한다.

## 17. 검증 행렬

| 시나리오 | 기대 결과 |
|---|---|
| ChatGPT/플러그인 화면 닫기 | worker와 관리 서비스 계속 동작 |
| supervisor 강제 종료 | OS가 복구, 기존 실행 확인 후 재접속, 입력 중복 없음 |
| worker 강제 종료 | 자기 작업만 복구, 다른 대화 보존 |
| service stop | 자동 재시작하지 않음, 재시작은 명시적 start |
| 정상 계정 → quota 초과 | 우선순위 계정 선택, 동일 thread·goal 유지 |
| 세 우선 계정 한도 소진 | account-backup 사용, 600초 단위 복귀 확인 |
| 계정 확인 실패 | unknown 표시, 임의 exhaustion/fallback 없음 |
| 목표 예산 소진·사용자 pause | 자동 우회·불필요한 재개 없음 |
| 다른 native writer 사용 중 | 실행 소유권 강탈 없음 |
| GUI 버튼 연속 클릭·재전송 | 작업 명령 중복 방지 |
| 두 설정 편집 창 | revision 충돌 감지, 사용자 변경 보존 |
| 이벤트 연결 중단·재연결 | snapshot/이력 복원, 잃은 상태를 0으로 표시하지 않음 |
| 한국어 조합·여러 줄 입력 | 오발송 없음, 초안·히스토리 보존 |
| plugin 업데이트/제거 | 다른 plugin과 대화/인증 유지 |
| Codex/ChatGPT 업데이트 | 명시적 호환 진단, 기존 동작 engine 보존 |

## 18. 되돌리기

설치 전 실행 파일 링크, config, marketplace, 서비스 정의와 native 인증 fingerprint를
비공개 상태 경로에 기록한다. DB 백업은 일관된 SQLite backup 방식으로 하며 WAL 파일만
따로 복사하여 정상 백업으로 취급하지 않는다.

새 서비스 전환 실패 시 새 서비스의 재시작 의도를 해제하고 소유권 잠금을 확인한 뒤 이전
실행 파일/설정으로 돌아간다. 기존 worker가 살아 있으면 그대로 재접속한다. plugin 제거는
관리한 항목만 제거한다. 대화 원본이나 인증을 rollback 대상으로 덮어쓰지 않는다.

## 19. 공식 근거와 검증 경계

다음 공식 문서는 2026-10-12 확인했다. 현재 host/계정에서의 실제 노출은 별도 검증 사항이다.

- [Plugin Extensions](https://developers.openai.com/plugins/build/extensions): sidebar, thread panel,
  plugin settings와 공식 metadata.
- [Package your plugin](https://developers.openai.com/plugins/build/plugins): 로컬/개인 marketplace와
  manifest, bundled MCP, 업데이트/설치 경로.
- [MCP UI](https://developers.openai.com/plugins/build/chatgpt-ui): portable bridge와 UI resource,
  직접 도구 호출, host-specific 기능 검출.
- [Connect and test](https://developers.openai.com/plugins/deploy/connect-chatgpt): MCP 연결·metadata 갱신·검증.
- [Codex App Server](https://learn.chatgpt.com/docs/app-server): 대화·turn·승인·이벤트 API와 버전별 schema.

## 20. 작업 기록 규칙

계획 변경에는 이유와 현재 근거를 남긴다. 구현했다고 추정하여 체크를 완료하지 않는다.
검증 기록에는 실행한 명령, 범위, 결과, 실제 host 여부, private artifact 위치, 정리 결과를 적는다.
미지원 host 기능이 발견되면 독립 웹사이트로 범위를 바꾸지 않는다. 실제 가능한 공식
plugin surface를 사용하고 지원 범위와 막힌 부분을 명확히 보고한다.
