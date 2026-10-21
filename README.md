# codexm

공식 Codex의 계정과 하위 작업을 관리하는 독립 프로그램입니다. 모델 호출 없이 계정 한도·작업 상태를 감시하고, 같은 대화를 다른 계정으로 이어갑니다. Meteor 로컬 웹, TUI와 로컬 ChatGPT 플러그인이 하나의 관리 서비스에 연결됩니다. `codex` 명령, 로그인, 업데이트는 공식 CLI가 그대로 소유합니다.

## 실행

공식 Codex CLI를 먼저 설치해야 합니다. `codex --version`으로 확인하세요.
설치기는 Codex가 없거나 실행되지 않으면 설치 안내를 표시하고 종료합니다.
설치기는 공식 Codex 실행 파일·로그인·업데이트를 변경하지 않습니다. 명시적인 `account NAME --set-default`만 GUI/native 공용 기본 로그인을 변경합니다.

```sh
./install.sh --build               # Go가 설치된 로컬 개발 환경
./install.sh                       # 정식 Release 이후 사용 (private 저장소: gh 로그인 필요)
source <(command codexm shell-init zsh) # zsh 시작 파일에 등록 (.home은 자동 로드)
codexm account account-work              # 현재 터미널의 native Codex 계정 변경
codexm account account-main --set-default # 새 터미널의 native 기본 계정 저장
codexm account account-secondary --remove        # 등록 삭제 (로그인 파일·대화 보존)
codexm setup --manager account-backup     # 관리 판단에 사용할 별도 계정 저장
codexm service install            # 사용자 OS 서비스 설치 및 시작
codexm plugin install             # 개인 로컬 marketplace 등록
codexm notify five-hour           # 모든 계정의 5시간 소진을 Discord로 알림
codexm                            # 작업 목록 TUI
```

기존 `~/.codex-accounts/<name>` 인증과 `~/.codex` 대화를 유지합니다.

| 명령 | 적용 범위 |
|---|---|
| `codexm account NAME` | 현재 터미널에서 이후 실행할 native `codex`의 계정 |
| `codexm account NAME --set-default` | 새 독립 터미널과 이후 시작할 GUI의 native 기본 로그인 |
| `codexm account NAME default` | `--set-default`의 이전 호환 표기 |
| codexm 설정의 우선순위·관리자·예비 계정 | codexm이 관리하는 작업의 계정 정책 |

현재 터미널의 선택은 `CODEX_HOME`으로 named 인증을 가리키고, 공용 config·대화 파일과
`CODEX_SQLITE_HOME`으로 같은 대화 DB를 사용합니다. 자식 셸은 부모 선택을 상속합니다.
`--set-default`는 공용 `~/.codex/auth.json`을 named 인증에 원자적으로 연결하고 이전
인증을 private 백업합니다. 이미 명시적으로 선택한 터미널의 환경은 유지합니다.
`codexm account`는 현재 터미널과 native 기본값을 구분해서 표시합니다.

두 native 선택 명령은 codexm 등록·우선순위·관리자·예비 계정과 기존 작업을 바꾸지
않습니다. 관리 화면에서 로그인 등록과 작업 정책을 설정하세요. codexm에는 별도 기본
작업 계정이 없으며, 새 작업은 우선순위에서 사용 가능한 첫 계정을 선택합니다.
예전 `default_account`는 무시하고 설치 중 백업 후 제거합니다.

부모 환경은 Go 자식만으로 바꿀 수 없으므로 얇은 **codexm** zsh 함수가 필요합니다.
공식 **codex** 실행 파일·인수·출력·종료 코드는 그대로 사용합니다. `.home` 설치에서는
새 터미널에 자동 로드되며, 이미 열린 터미널은 `source ~/.codex_rc` 한 번이면 됩니다.
일반 설치에서는 위 `source <(...)`를 `.zshrc`에 넣으세요. 셸 통합이 없으면 성공한
전환처럼 표시하지 않고 로드 방법을 안내합니다. 현재 지원하는 셸은 zsh입니다.

파일 인증만 지원하며 keyring이나 강제 workspace 정책과 충돌하면 기존 선택을 보존하고
실패합니다. native 홈별 캐시·기기 연결 상태는 공식 Codex가 관리합니다. 실행 중인
Codex/GUI/daemon의 인증은 메모리에 남을 수 있어 새 실행부터 선택을 적용하며 강제
재시작하지 않습니다. 기본값 변경은 GUI의 다음 실행에도 영향을 줍니다. daemon 조회는
계정 선택의 전제 조건이 아닙니다.

## 화면과 명령

TUI도 웹과 같은 Go 관리 기능을 사용합니다. 상단의 **작업·계정·사용량·기록·설정·관리자**
탭을 클릭하거나 `F1`–`F6`으로 이동합니다. 넓은 터미널에서는 대화 목록과 상세를
나란히 표시하고, 좁은 화면에서는 세로로 배치합니다. 권장 최소 크기는 40×20입니다.

- 마우스: 탭·대화·버튼·폼 클릭, 목록·본문 휠 스크롤. `Ctrl+G` 또는 `/mouse`로
  마우스 보고를 끄면 터미널의 기본 텍스트 선택·복사를 사용할 수 있습니다.
- 키보드: `F7` 새 작업, `F8` 검색·필터, `F9` 선택 작업의 모든 동작.
  입력창 `↑↓`/`Ctrl+P,N`은 입력 기록, `Alt+↑↓`는 대화 선택입니다.
- 폼: `Tab`/`Shift+Tab` 이동, `Enter` 편집·선택, `←→` 옵션 변경, `Ctrl+S` 저장,
  `Esc` 취소. 여러 줄 편집은 `Enter` 줄바꿈, `Ctrl+Enter` 적용입니다.
- 계정 탭: 최초 계정 추가·재로그인·취소, 단기/장기 잔여 한도, native 기본 로그인과
  등록 삭제를 관리합니다. 관리자·작업 우선순위·예비 계정은 설정 탭에서 바꿉니다.
- 설정 탭: 필드를 편집하고 저장합니다. 다른 화면의 변경과 충돌하면 입력을 보존하며,
  최신 설정 확인으로 바뀐 항목만 합쳐 다시 저장할 수 있습니다.
- 기록 탭: 전체/선택 작업의 실행 시간·한도 관측·계정 전환을 보고 계정·기간·사유로
  거릅니다. 한도 사용률은 계정 전체 관측값이며 작업별 정확한 소비율이 아닙니다.
- 새 작업 폼은 채팅·단발 작업·지속 목표·그룹, 계정·모델·추론·목표 예산을 지원합니다.
  `/send`, `/goal`, `/requests`, `/unmanage`로 추가 지시·목표·질문/승인·관리 해제를
  조작합니다. 지원하지 않는 요청 형식은 공식 Codex 터미널로 연결합니다.

기본 TUI와 `codexm session list`는 현재 장비의 **전체 Codex 대화**를 표시합니다.
CLI·데스크톱/에디터·하위 에이전트·보관된 대화를 같은 목록에서 찾습니다.
`--search TEXT`는 제목·ID·폴더·배정 계정 검색, `--managed`는 codexm 등록 작업만,
`--active-only`는 보관 대화 제외, `--json`은 명시적인 메타데이터 출력입니다.
목록 조회는 관리자 로그인이나 supervisor 실행 없이도 가능합니다.

Codex의 `~/.codex/state_*.sqlite`는 `mode=ro`와 `query_only`로 읽고 내용을 수정하지
않습니다. 살아 있는 WAL을 읽으므로 실행 중인 Codex의 새 대화도 표시됩니다.
DB 파일의 버전 번호와 실제 컬럼을 확인하며, 지원하지 않는 스키마에는 오류 안내를
표시합니다. 관리자 계정·작업·로그는 기존 `~/.local/state/codexm/state.sqlite`에
별도로 보관합니다. 공식 DB에 codexm 테이블을 추가하지 않습니다.

목록 조회는 대화를 실행하거나 가져오지 않습니다. 처음 `/open`할 때 작업 계정을
선택하며 같은 대화 ID를 등록·연결합니다. 기존 관리 작업은 저장한 계정을 유지합니다.
`codexm open THREAD_ID`도 지원합니다. 이미 다른 Codex가 쓰고 있는 대화의 소유권은
가져오지 않습니다. 관리 로그는 codexm 등록 이후의 관측부터 기록합니다.

- 입력창의 ↑↓는 입력 기록·커서 이동입니다. 목록은 Tab 또는 Alt+↑↓로 이동합니다. `/` 메뉴 안에서는 ↑↓로 명령을 선택합니다.
- 선택 작업의 `/account`로 계정을 고르거나 `/account account-secondary`를 입력합니다. CLI는 `codexm switch-account JOB_ID account-secondary`입니다. 수동 전환도 예비 계정 제한을 지킵니다.
- 목록 Enter 또는 `/open`: 같은 대화 ID를 해당 작업의 계정으로 새 터미널에 연결합니다. `/new`: 새 채팅, `/chats 검색어`: 전체 대화 검색입니다.
- 공식 Codex의 전체 대화 화면과 기본 명령은 새 터미널에서 사용합니다. TUI에서도
  `/send`로 추가 지시를 보내고 새 작업 폼에서 모델을 선택할 수 있습니다.
- `/status`, `/usage`: 전체 상태·사용량. `/info`, `/logs`: 선택한 작업. `/logs all`: 전체 로그.
- `/manager`로 판단을 요청할 때만 관리자 모델을 호출합니다. 평상시 감시·로그·한도 확인은 모델을 호출하지 않습니다.

세션을 열 때 최근 60초 안에 검증한 사용량을 재사용합니다. 소켓이 준비되면 0.2초 간격으로 화면을 연결합니다. 첫 화면 실행 전 남은 터미널 입력을 한 번 정리해 초기 CR/LF가 편집창으로 넘어가는 것을 방지합니다. 재연결에서는 입력을 정리하지 않습니다.

## 데몬과 복구

공식 `codex app-server daemon`은 OpenAI CLI의 공용 백그라운드 서버입니다. `codexm-supervisor`는 우리가 만든 작업 관리자이며 서로 다른 프로세스입니다. codexm은 계정별 전용 공식 app-server를 실행하므로 공용 데몬 상태에 의존하지 않습니다.

```sh
codexm daemon watch       # 5초 간격 확인, 사라졌을 때 공식 start 실행
codexm daemon status      # 감시·공식 데몬 상태
codexm daemon restart     # 공식 데몬 재시작
codexm daemon stop        # 자동 복구 일시 중지와 공식 데몬 중지
codexm daemon start       # 공식 데몬 시작과 설정된 자동 복구 재개
codexm daemon unwatch     # 복구 감시 서비스 중지, 공식 데몬 유지
```

작업 관리 서비스는 `org.codeasy.codexm.supervisor`, 공용 데몬 감시는 `org.codeasy.codexm.watchdog` 사용자 서비스입니다. macOS는 launchd, Linux는 systemd를 사용합니다. 새 worker는 독립 프로세스이고, 기존 tmux worker는 현재 실행을 유지한 채 다음 재시작·계정 전환부터 이동합니다. `killall codex`는 공식 서버들을 강제 종료하므로 실행 중인 작업에도 영향을 줍니다. 감시가 활성화돼 있으면 공용 서버를 복구하고, 관리 중인 작업은 supervisor가 작업 상태에 맞춰 복구합니다. 자동 복구를 켠 상태에서 의도적으로 중지하려면 `codexm daemon stop`을 사용합니다. 공식 CLI에서 직접 `daemon stop`을 실행하면 감시가 다시 시작할 수 있습니다.

수동 공식 명령은 `codex app-server daemon start`, `restart`, `version`입니다. codexm 없이도 사용할 수 있습니다.

## 로컬 플러그인

[설치·사용법](docs/local-plugin.md)에 따라 개인 marketplace에서 `codexm`을 설치합니다. 계정 추가·재로그인, 우선순위, 계정별 잔여 한도, 선택 세션의 계정 변경, 새 작업·기존 대화·추가 지시, 전체/세션별 기록과 설정 편집을 제공합니다. 별도 웹 서버나 공개 포트를 열지 않습니다. 일반 화면 조작은 MCP에서 로컬 관리 서비스로 바로 전달하며 관리자 모델을 호출하지 않습니다.

공식 플러그인 확장으로 **Plugins → codexm → codexm 관리 / 계정 관리** 버튼을
선언합니다. 같은 패널의 메뉴는 **작업·계정·사용 기록·설정**이며 버튼으로 조작합니다.
사이드바·대화 패널에도 **codexm 관리** 진입점을 선언하며 실제 노출은 호스트에 따릅니다.
로컬 Codex 입력창에서 `$codexm-manage` / `$codexm-accounts` 스킬을 사용합니다.
0.5.5는 GUI가 붙이는 `$codexm:codexm-manage` / `$codexm:codexm-accounts`와
Markdown 스킬 링크도 처리합니다. `@codexm manage` / `@codexm accounts` 텍스트도
지원하지만 `@` 목록의 파일 참조 선택과는 구분합니다.
최초 설치 후 **Settings → Hooks** 또는 공식 CLI의 `/hooks`에서 codexm의
`UserPromptSubmit` 훅을 검토·신뢰해야 합니다. 신뢰된 훅이 명령을 가로채 공식 플러그인
딥링크를 열고 모델 제출을 차단합니다. 훅이 꺼졌거나 신뢰되지 않은 환경에는 이 보장이 없습니다.
`/manage`를 새 native slash 명령으로 등록하는 방식은 아닙니다. 화면 열기는 대화 등록·작업 실행·계정 변경을 하지 않습니다.
`@codexm` 또는 `$codexm`만 전송하면 같은 훅이 현재 채팅을 모델 호출 없이 등록합니다.
명시적인 `@codexm register`도 같습니다. 플러그인을 선택만 하거나 관리 화면만 열면
등록하지 않습니다. 추가 지시가 붙은 문장은 일반 대화입니다. 원래 앱/CLI가 쓰기 권한을 보유하면 계정 인계는
해제 후 실행되며, 현재 GUI의 대화 하나만 투명하게 계정을 바꾸는 공식 API는 미지원입니다.
앱 파일을 수정하지 않으며 GUI 업데이트/종료와 독립적으로 관리 서비스가 유지됩니다.
`codexm plugin doctor`는 공식 엔진의 MCP/skill/hook 발견, 훅 신뢰 상태와 설치 캐시 버전을 검사합니다.
[최종 기능·검증 상태](docs/completion-status.md)에서 설치 확인과 남은 호스트 경계를 구분합니다.

## Meteor 로컬 웹

0.6.0의 선택형 웹은 왼쪽 대화 목록·중앙 채팅·하단 입력창, 계정·설정·사용 기록,
직접 명령과 작업 상세를 제공합니다. 관리자 설정 전 로그인도 가능하며 MongoDB는 필요하지 않습니다.

```sh
./build-web.sh                     # Go, Meteor 3.5.2, Node 24
./install.sh --build --web "$PWD/dist/web/bundle"
codexm web                         # loopback에서 실행하고 브라우저 열기
codexm web verify                  # 설치된 번들 검증
codexm web stop                    # 웹·브리지만 종료, 작업 유지
```

[사용·설치·복구 안내](docs/local-web.md)와 [실제 검증 기록](docs/web-validation-20261021.md)을 참고하세요.
개발 실행은 `codexm web --dev /absolute/project/web`, 세션은 `codexm-web-dev`입니다.

## 개발 설계

[Meteor 로컬 웹 개발 계획](docs/meteor-local-web-plan.md)은 착수 당시 기능 범위·연결 계약·구현 순서·검증 기준을 보존합니다. 개발 프로젝트 경로는 `~/WebstormProjects/codexm/`입니다.

## 업데이트와 저장 위치

검증한 공식 엔진을 `~/.local/share/codexm/engines`에 보관합니다. 공식 Codex가 업데이트돼도 기존 작업은 보관한 엔진을 유지합니다. `codexm runtime refresh`는 호환성을 검사한 뒤 새 엔진을 보관합니다.

공식 npm 설치에서는 실제 native optional package를 찾아 보관합니다. Node 런처를
그대로 복사하지 않으며, 계정 관리 제품이 공식 Codex의 npm/Brew 업데이트를 가로채지 않습니다.

codexm 실행 파일을 업데이트한 뒤 `codexm service upgrade`로 관리 서비스만 교체합니다. 기존 worker를 보존하며 실패 시 이전 서비스 정의로 돌아갑니다. 플러그인은 `codexm plugin install` 후 공식 플러그인 설치/새로고침을 다시 수행합니다.

설정은 `~/.config/codexm/config.toml`, 작업 DB와 로그는 `~/.local/state/codexm`입니다. 인증·대화·실행 상태는 Git에 보관하지 않습니다. `~/.home`은 독립 저장소의 고정 설치 버전과 로컬 셸 통합만 관리합니다.

```sh
go test -race ./...
go vet ./...
codexm doctor --json
```

현재는 배포 준비 단계입니다. Brew 공식·npm 패키지·네 플랫폼 압축 파일은 로컬에서
생성하며 게시하지 않습니다. [배포 구조](docs/distribution.md)를 참고하세요.
