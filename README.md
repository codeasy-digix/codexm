# codexm

로컬 Codex 채팅을 찾아 계정을 고르고 **공식 Codex CLI로 이어가는** 작은 도구입니다.
채팅 실행·모델 호출·권한 승인·목표 진행은 공식 CLI가 담당합니다. codexm은 별도
작업 관리자나 자동 계정 교체 서비스를 시작하지 않습니다. 공식 `codex` 명령과 업데이트는
수정하지 않습니다.

## 기본 흐름

```sh
codexm                              # 채팅 목록 → 번호 선택 → 계정 선택 → 공식 CLI
codexm list                         # 모든 로컬 프로젝트의 채팅 목록
codexm list --archived --search 검색어
codexm list --json
codexm resume THREAD_ID --account ACCOUNT_NAME
```

목록에서는 `/검색어`, `n`/`p` 페이지, `a` 보관 목록 포함, `r` 새로고침, `q` 종료를
사용합니다. 이전 계정은 과거 관리 기록과 codexm의 마지막 실행 요청에서 확인한
정보만 표시합니다. 다른 프로그램에서 실행한 계정은 알 수 없을 수 있습니다.
저장된 과거 계정을 다음 실행에 자동으로 고정하지 않고 다시 선택합니다.

선택 후에는 계정별 `CODEX_HOME`과 같은 머신의 공용 대화 저장소를 준비하고,
공식 CLI의 보관/복원 명령을 거쳐 `codex --no-daemon --no-alt-screen resume ID`로
프로세스를 교체합니다. 이후 입출력·종료 코드·작업 처리는 공식 CLI 그대로입니다.
이미 보관된 채팅에는 `unarchive`만 실행합니다. 일반 채팅에는
`archive → unarchive`를 실행합니다. 실패하면 resume을 시작하지 않습니다.

**아직 다른 CLI/GUI가 소유한 채팅은 공식 명령 자체가 거부할 수 있습니다.**
이 경우 원래 화면에서 해당 채팅을 보관하거나 CLI를 종료한 뒤 다시 선택하세요.
보관한 채팅은 목록의 `a` 또는 웹의 ‘보관된 채팅 포함’으로 찾습니다.
잠금 파일 삭제·DB 직접 수정·외부 프로세스 강제 종료·새 ID fork는 하지 않습니다.
보관 해제 단계가 실패하면 채팅은 보관 상태일 수 있으며 원래 ID로 다시 복원합니다.

## 계정

```sh
source <(command codexm shell-init zsh)
codexm account                          # 현재 터미널과 기본 로그인 정보
codexm account --list
codexm account ACCOUNT_NAME             # 현재 터미널의 계정
codexm account ACCOUNT_NAME --login --browser
codexm account ACCOUNT_NAME --login --device
codexm account ACCOUNT_NAME --set-default # 새 터미널·다음 GUI 실행의 기본 인증
codexm account ACCOUNT_NAME --remove     # 목록 등록만 해제, 인증·대화 보존
```

이 기능은 유지합니다. 같은 머신에서 대화·공통 설정을 공유하고 로그인만 구분합니다.
계정 간 모델 캐시·GUI 상태나 서버 설정을 복사하지 않습니다. 실행 중인 GUI/CLI의
인증을 강제로 바꾸지 않으며 기본 인증을 바꾼 GUI는 재실행해야 합니다.
등록 해제는 과거 작업 기록을 변경하거나 다른 계정으로 작업을 자동 재배정하지 않습니다.

## 웹

```sh
codexm web
codexm web status
codexm web stop
codexm web logs
```

웹은 채팅/계정 두 화면으로 구성합니다. 채팅과 계정을 선택하면
`codexm-cli-<id>`라는 tmux 세션에 공식 CLI를 엽니다. 웹에는 CLI가 출력한 기록과
현재 화면을 표시하고, 입력은 그 터미널로 전달합니다. 별도 app-server worker나
자체 답변 렌더링·목표 관리 엔진을 만들지 않습니다. 기본 키·슬래시 명령을 전달할 수
있으며 고급 터미널 조작은 화면에 표시한 `tmux attach -t ...`로 접속합니다.

웹을 닫거나 다시 열어도 CLI는 유지됩니다. 같은 채팅/계정으로 다시 열면 기존
터미널을 연결합니다. CLI 종료는 `/exit` 또는 명시적인 ‘터미널 종료’를 사용합니다.
한 채팅을 여러 CLI/GUI에서 동시에 작업시키지 마세요. 전송 응답이 끊긴 경우 입력을
자동 재전송하지 않고, 터미널에서 결과를 확인하도록 표시합니다.

직접 tmux 터미널을 여는 명령도 제공합니다.

```sh
codexm resume THREAD_ID --account NAME --terminal
codexm terminal codexm-cli-SESSION_ID
```

웹/별도 터미널 실행에는 tmux가 필요합니다. 현재 터미널에서 바로 재개하는 CLI에는
tmux가 필요 없습니다. Meteor는 loopback에서 실행합니다. 외부 접근은 별도 웹 로그인과
HTTPS 프록시를 명시적으로 설정한 경우에만 사용합니다.

```sh
codexm web user --username YOUR_USERNAME  # 비밀번호를 숨겨서 입력; Codex 로그인과 별개
codexm web --public-origin https://YOUR_HOST --no-open
```

HTTPS 프록시 구성·재시작·중지 방법은 [외부 접속 안내](docs/public-web.md)를 따릅니다.

## 설치와 범위

공식 Codex CLI를 먼저 설치하고 `codex --version`을 확인합니다.

```sh
cd /path/to/codexm
./install.sh --build
# 웹을 함께 설치할 경우 Node 24로 ./build-web.sh 후:
./install.sh --build --web /absolute/path/to/codexm/dist/web/bundle
codexm doctor
```

소스·빌드·설치·업데이트는 이 제품 저장소에서만 관리합니다. `~/.home`은 이미 설치한
명령의 등록과 개인 셸 로더만 관리합니다. CLI 실행에는 Python/Node가 필요 없고,
선택적인 Meteor 웹에는 Node 24가 필요합니다. 이번 적용은 로컬 Mac으로 한정하며
Homebrew/Release/npm 배포와 다른 장비의 codexm 업데이트는 보류합니다.

관리자 모델, 자동 worker/계정 교체, 스케줄링, 목표/예산 관리, watchdog, 자동 알림,
이전 관리 플러그인과 history-sync는 새 공개 실행 경로에서 제외했습니다.
기존 관리 DB는 이전 계정 표시용으로만 읽습니다. 인증·대화·기존 목표/예산은 삭제하거나
변환하지 않습니다. 이전 관리 구현/검증 문서는 과거 자료이며 현재 사용법이 아닙니다.

[실행 설계와 검증](docs/native-cli-core.md) · [설치 소유권](docs/deployment.md) ·
[웹 사용 안내](docs/local-web.md) · [동기화 제거](docs/history-sync.md)
