# Meteor 로컬 웹

codexm 0.6.1은 Meteor 3.5.2·React·TypeScript 화면을 제공합니다. 왼쪽 대화 목록,
중앙 기록, 하단 입력창과 선택형 작업 상세로 구성하며 기본 HTML과 일반 CSS만 씁니다.
웹, 기존 CLI/TUI, 플러그인은 같은 Go 관리 서비스를 사용합니다. MongoDB는 필요하지 않습니다.

## 빌드와 실행

공식 Codex CLI, Go, Node 24, Meteor 3.5.2, tmux가 있는 대상 머신에서 빌드합니다.
Node의 기준 버전은 `web/.nvmrc`의 24.15.0입니다. 설치한 번들의 실행에는 Meteor·Go가
필요하지 않으며 Node 24·tmux·공식 Codex와 같은 버전의 codexm이 필요합니다.

다른 프로젝트가 다른 Node 기본 버전을 사용하는 머신에서는 Node 24를 별도로 설치하고
`~/.config/codexm/config.toml`의 `[runtime]`에
`web_node_binary = "/absolute/path/to/node24/bin/node"`를 지정합니다.
웹은 이 실행 파일을 사용하므로 셸·Homebrew·NVM의 기본 Node를 바꿀 필요가 없습니다.

```sh
./build-web.sh
./install.sh --build --web "$PWD/dist/web/bundle"
codexm web verify
codexm web
codexm web status
codexm web logs
codexm web stop
```

CLI만 설치하는 `./install.sh --build`도 유지됩니다. 설치기는 번들의 전체 체크섬,
제품 버전, 계약 버전, OS·CPU를 검사한 뒤 새 불변 경로에 복사합니다. 동일 버전의
다른 파일이나 일반 파일로 된 진입점은 덮어쓰지 않습니다. 설정 변환이 실패하면
진입점을 바꾸지 않고, 마지막 링크 교체가 실패하면 이전 실행 파일 링크를 복구합니다.
기존 버전 디렉터리는 삭제하지 않습니다. 공식 `codex` 설치와 인증은 설치 대상이 아닙니다.
복사 과정은 디렉터리·파일과 macOS 심볼릭 링크의 권한을 보존하므로 `umask 077`에서도
원래 번들의 봉인 체크섬이 유지됩니다.
웹을 업데이트할 때는 `codexm web stop` 후 설치하고 다시 실행합니다. 관리 서비스의
실행 파일 변경은 기존 `codexm service upgrade` 경로를 사용합니다.

기본 주소는 `http://127.0.0.1:4317`이며 다른 머신에 공개하지 않습니다. 다른 포트는
`codexm web --port 4327`로 지정합니다. 실행 시 생성하는 일회성 코드는 URL fragment로
전달되어 HTTP URL·서버 로그에 남지 않습니다. `--no-open`은 코드 대신 private 파일의
위치를 출력합니다. 같은 머신에서 `codexm web`을 다시 실행하면 두 번째 브라우저용 코드를
재발급합니다. 브라우저 세션은 8시간 유지됩니다.

SSH로 접속한 머신의 웹을 볼 때는 `ssh -L 4317:127.0.0.1:4317 remote-mac`로
포워딩한 뒤 같은 `http://127.0.0.1:4317`을 엽니다. 해당 머신에서 `codexm web --no-open`을
실행하고 출력된 private 파일의 코드를 직접 입력합니다. 다른 호스트 이름이나 Origin은 거절됩니다.

```sh
# 격리 config의 runtime/account/store 경로도 개발용으로 지정해야 합니다.
codexm --config /absolute/dev/config.toml web --dev "$PWD/web" --no-open
tmux attach -t codexm-web-dev
# 설치된 웹은 tmux attach -t codexm-web
```

웹 종료는 Meteor와 웹 브리지만 종료합니다. 별도 launchd/systemd supervisor와 worker는
계속 실행됩니다. 웹의 tmux 세션을 supervisor 서비스와 혼동하지 마세요. macOS의 SSH
Background 세션에는 `user/UID`, GUI 로그인에는 `gui/UID` 도메인을 사용하고 설치 영수증에
기록합니다. 재시작 시 기록된 도메인을 유지합니다.

## 사용 흐름

관리자 설정 전에도 계정 화면에서 로그인·재로그인·취소와 저장된 계정 조회가 가능합니다.
저장한 관리자 계정으로 관리 서비스를 시작한 뒤 새 작업을 만듭니다. 계정의 네 역할은
작업 우선순위, 선택 작업의 실행 계정, 관리자 계정, 공용 기본 로그인입니다.
공용 기본 로그인 변경은 이미 실행 중인 공식 GUI의 계정을 바꾸는 기능이 아닙니다.

계정 카드의 **등록 삭제**는 확인 후 목록과 작업 우선순위에서 계정을 제외합니다.
로그인 파일·공용 기본 로그인·대화 기록·과거 실행 내역은 보존합니다. 관리자 계정은
설정에서 다른 관리자를 선택하고, 사용 중인 작업은 계정 변경 또는 관리 등록 해제 후
삭제하세요. 예비 계정을 삭제하면 예비 역할을 비우며 다른 계정을 자동 지정하지 않습니다.
관리 화면에서 같은 별칭으로 다시 로그인하면 등록을 복원합니다.
`codexm account NAME`은 native 터미널 계정 선택이며 codexm 등록·우선순위는 바꾸지 않습니다.
CLI 삭제는 `codexm account NAME --remove`입니다. 관리 서비스가 중지된 상태에서도
기존 작업과 서비스 잠금을 검사하며, 오래된 화면의 설정 revision으로는 삭제하지 않습니다.

새 채팅·task·goal·group을 만들고 모델, 추론 강도, 읽기 전용, 완료 검사 명령, 상위 그룹을
선택할 수 있습니다. 그룹 생성은 모델을 호출하지 않습니다. 목표의 토큰 예산은 명시한 경우만
설정합니다. 목표 편집은 기존 사용량과 상태를 보존하며 상태 변경은 사용자가 선택합니다.

대화 목록 선택은 조회입니다. 상세의 관리 등록·해제, 실행권 가져오기, 일시 중지·재개·중지·완료,
계정 변경, 공식 터미널 열기는 각각 별도 조작입니다. 외부 writer가 있는 대화의 잠금이나
다른 Codex 프로세스를 제거하지 않습니다. 계정 변경과 승인에는 현재 Run ID를 검사합니다.

Enter는 전송, Shift+Enter는 줄바꿈이며 한국어 IME 조합 중 Enter는 전송하지 않습니다.
초안·이전 입력·즐겨찾기는 호스트/공식 저장소/thread ID별로 보관합니다. 대화 ID가 처음
배정되면 임시 작업 키에서 함께 옮깁니다. 읽는 중 새 출력이 와도 스크롤 위치를 유지합니다.

`/manage`, `/accounts`, `/logs`, `/usage`, `/settings`, `/status`는 버튼과 같은 직접 명령입니다.
`/manager`는 별도 관리자 요청 화면을 열며, 명시적인 제출 버튼이 모델을 호출합니다.
계정·로그·정책·설정·주기적 감시는 모델을 호출하지 않습니다. 사용 기록은 작업·계정·기간·
변경 사유별로 필터하며, 계정 전체 한도 %와 작업별 토큰·실행 시간을 구분합니다.
Discord 설정은 기존 Go 알림 경로를 사용하고 webhook을 브라우저로 보내지 않습니다.

## 재접속과 복구

브라우저는 변경 요청 ID를 먼저 보관하고 DDP 자동 재시도를 끕니다. supervisor가 같은 ID의
요청 해시와 결과를 저장합니다. 응답을 확인하지 못한 요청은 연결 상태 화면에서 결과와
실제 대화를 조회한 뒤 정리합니다. 결과가 불명확한 전송을 자동으로 다시 보내지 않습니다.

관리 journal은 상태와 같은 SQLite 트랜잭션에서 기록되며 최근 약 50,000건을 보관합니다.
DB 교체나 보관 범위 밖의 재접속은 새 snapshot으로 복구합니다. 설정 revision과 공식 목록
revision은 별도로 관측합니다. 선택한 대화의 최근 기록은 750ms 간격으로 묶어 읽고, 이전
기록은 20턴씩 추가합니다. 전체 대화 본문을 새 DB에 복사하지 않습니다.

이전 관리 DB를 처음 열 때 WAL을 포함한 `state-before-web-*.sqlite` 백업을 만듭니다.
기존 schema_version 1은 유지하고 추가 테이블·트리거만 설치하므로 이전 관리 코드도 읽을 수
있습니다. 파일 복구가 필요하면 관리 서비스가 멈춘 상태에서 백업을 선택해야 합니다.
새 작업이 생긴 후 오래된 DB를 그대로 덮어쓰면 안 됩니다. 실행 파일 rollback은 기존
`~/.local/share/codexm/versions/` 대상 링크를 복원하고 서비스의 기존 upgrade 경로를 사용합니다.

## 구현 결정과 경계

- 공식 Meteor Rspack 통합을 사용합니다. 기본 bundler에서 React Markdown의 ESM 조건부
  export를 찾지 못한 문제를 실제 개발 실행에서 확인했습니다. 개발 자산 서버도 loopback으로
  고정하며 host:port 계산 문제를 피하도록 포트를 명시합니다.
- Meteor 3.5.2가 생성하는 서버 shrinkwrap은 추가된 node-gyp 의존성과 일치하지 않았습니다.
  `contracts/meteor-3.5.2-server-*.json`에 배포용 package와 전체 lock을 고정했습니다.
  underscore 1.13.8과 node-gyp 13.1.0을 사용하고 `npm ci --omit=dev`로 설치합니다.
  [Underscore 보안 수정](https://github.com/jashkenas/underscore/security/advisories/GHSA-qpx9-hpmf-5gmw),
  [node-gyp 13.1.0](https://github.com/nodejs/node-gyp/releases/tag/v13.1.0).
- 목표 fork, 시간 예산 변경, 파일 첨부는 현재 capability에서 제공하지 않습니다.
  공식 goal/set 계약에는 토큰 예산만 있으며 fork의 예산·사용량 승계는 검증되지 않았습니다.
  지원하지 않는 복합 elicitation 입력은 공식 터미널로 연결합니다.
- 실행 파일·서비스 업그레이드와 watchdog 수명 관리는 기존 CLI 명령을 사용합니다.
  공개 release/tag/npm 게시와 다른 머신의 새 웹 적용은 이 로컬 설치에 포함하지 않습니다.

계약은 [bridge v1](../contracts/README.md), 실제 검사 결과는
[0.6.0 검증 기록](web-validation-20261021.md)에 정리합니다.
