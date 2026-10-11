# codexm

공식 Codex를 하위 작업으로 관리하는 독립 터미널 프로그램입니다. 채팅 화면이나 기본 명령을 복제하지 않습니다. `codex` 명령, 로그인, 업데이트는 공식 CLI가 그대로 소유합니다.

## 실행

```sh
./install.sh --build               # Go가 설치된 로컬 개발 환경
./install.sh                       # 정식 Release 이후 사용 (private 저장소: gh 로그인 필요)
codexm account account-work              # 기본 작업 계정 저장
codexm setup --manager account-backup     # 관리 판단에 사용할 별도 계정 저장
codexm                            # 작업 목록 TUI
```

기존 `~/.codex-accounts/<name>` 인증과 `~/.codex` 대화를 유지합니다. `codexm account`는 셸 환경 변수나 공식 Codex의 기본 로그인을 변경하지 않습니다. 선택한 기본 계정은 새 작업과 처음 가져오는 대화에 적용되며 기존 작업은 저장한 계정을 유지합니다. 다음 실행에서도 설정을 불러옵니다.

## 화면과 명령

- 입력창 포커스에서 ↑↓로 작업을 선택합니다. `/` 메뉴가 열려 있으면 ↑↓는 메뉴를 선택합니다. 작성 중인 입력은 보존합니다.
- 목록 Enter 또는 `/open`: 같은 대화 ID를 해당 작업의 계정으로 새 터미널에 연결합니다. `/new`: 새 채팅, `/chats`: 기존 대화 가져오기입니다.
- 채팅·추가 지시·모델 선택·Codex 기본 명령은 새 터미널의 공식 Codex에서 사용합니다.
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
codexm daemon unwatch     # 감시 tmux 정리, 공식 데몬 유지
```

감시 세션은 `codexm-daemon-watchdog`이며 `tmux attach -t codexm-daemon-watchdog`로 연결합니다. `killall codex`는 공식 서버들을 강제 종료하므로 실행 중인 작업에도 영향을 줍니다. 감시가 활성화돼 있으면 공용 서버를 복구하고, 관리 중인 작업은 supervisor가 작업 상태에 맞춰 복구합니다. 자동 복구를 켠 상태에서 의도적으로 중지하려면 `codexm daemon stop`을 사용합니다. 공식 CLI에서 직접 `daemon stop`을 실행하면 감시가 다시 시작할 수 있습니다.

수동 공식 명령은 `codex app-server daemon start`, `restart`, `version`입니다. codexm 없이도 사용할 수 있습니다.

## 업데이트와 저장 위치

검증한 공식 엔진을 `~/.local/share/codexm/engines`에 보관합니다. 공식 Codex가 업데이트돼도 기존 작업은 보관한 엔진을 유지합니다. `codexm runtime refresh`는 호환성을 검사한 뒤 새 엔진을 보관합니다.

공식 npm 설치에서는 실제 native optional package를 찾아 보관합니다. Node 런처를
그대로 복사하지 않으며, 계정 관리 제품이 공식 Codex의 npm/Brew 업데이트를 가로채지 않습니다.

설정은 `~/.config/codexm/config.toml`, 작업 DB와 로그는 `~/.local/state/codexm`입니다. 인증·대화·실행 상태는 Git에 보관하지 않습니다. `~/.home`은 독립 저장소의 고정 설치 버전과 로컬 셸 통합만 관리합니다.

```sh
go test -race ./...
go vet ./...
codexm doctor --json
```

현재는 배포 준비 단계입니다. Brew 공식·npm 패키지·네 플랫폼 압축 파일은 로컬에서
생성하며 게시하지 않습니다. [배포 구조](docs/distribution.md)를 참고하세요.
