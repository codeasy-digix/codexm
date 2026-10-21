# Native Codex 계정과 터미널 선택

2026-10-22 사용자 요청에 따라 계정 명령의 범위를 정리한다.

- `codexm account NAME`은 이후 이 터미널에서 시작할 공식 Codex의 계정을 선택한다.
  Go 자식 프로세스는 부모 환경을 바꿀 수 없으므로 `codexm shell-init zsh`가
  제공하는 얇은 `codexm` 함수를 사용한다. 공식 `codex` 명령은 감싸지 않는다.
- `--set-default`는 새 터미널의 native 기본 로그인만 저장한다. 현재 터미널의
  명시적 선택은 유지한다. 예전 positional `default`도 같은 뜻으로 유지한다.
- native 선택은 codexm 설정·계정 등록 해제·우선순위·관리자·예비 계정을 바꾸지
  않는다. 관리 화면의 로그인 등록 및 설정에서 작업 계정을 관리한다.
- named 인증과 공용 config/history 연결을 재사용한다. 공식 `CODEX_HOME`과
  `CODEX_SQLITE_HOME`으로 인증만 선택하고 대화 DB는 공용 위치에서 유지한다.
  파일 인증·workspace 정책을 검사하며 keyring 정책을 우회하지 않는다.
- 셸은 비공개 임시 디렉터리의 NUL 구분 결과만 읽고 문자열을 eval하지 않는다.
  명령 실패·취소·결과 충돌 시 부모 환경은 유지한다. 일반 출력/입력은 직접 전달한다.
  새 독립 터미널은 공용 기본 로그인을 사용하고 자식 셸은 부모 선택을 상속한다.
- 두 Mac의 운영 인증·설정·DB·작업 상태와 설치 링크를 Git 밖에 백업했다.
  격리 zsh/실제 공식 엔진으로 선택·실패·새 셸·공용 DB를 검증한 뒤 Go race/vet,
  설치·doctor를 확인하고 같은 검토 commit으로 두 머신을 갱신한다.

실행 중인 Codex/GUI/daemon의 메모리 인증은 바꾸지 않고 강제 재시작하지 않는다.
기존 터미널은 `source ~/.codex_rc` 한 번, 새 터미널은 시작 파일에서 자동 로드한다.
설치 자체는 운영 계정이나 공용 기본 로그인을 선택하지 않는다.

공식 설정 설명: [CODEX_HOME](https://learn.chatgpt.com/docs/config-file/config-advanced#config-and-state-locations),
[sqlite_home](https://learn.chatgpt.com/docs/config-file/config-reference).
환경변수의 DB 동작은 설치된 공식 CLI를 사용한 격리 시험으로 확인한다.

## 구현·검증

0.6.2에서 현재 터미널 선택과 native 기본값을 분리했다. `account --list`는 codexm
등록 해제 후 보존한 native 로그인도 보여 주며, native 선택으로 작업 등록을 복원하지
않는다. 관리 화면에서 같은 별칭으로 다시 로그인하면 작업 등록을 복원할 수 있다.
웹·플러그인도 native 기본값과 작업 우선순위·관리자·예비 계정을 구분해서 안내한다.

전체 `go test -race -p 4 ./...`와 `go vet ./...`, 플러그인 UI 18개, 웹 3개 및
TypeScript 검사와 실제 Meteor 번들 빌드가 통과했다. 부모 zsh에서 선택·자식 상속·
새 독립 셸·default 저장 후 현재 선택 보존·로그인 실패·종료 코드·출력 전달·결과 충돌·
잘못된 frame·readonly 변수·문자열 주입 방지·임시 파일 정리를 시험했다.
기존 로그인 만료 fixture의 20ms 창은 race/build 부하에서 pending 전에 만료될 수
있어 1초로 늘렸다. 운영 로그인 제한 시간과 코드는 바꾸지 않았다.

공식 CLI 0.163.0의 격리된 `login status`, `config/read`, `thread/list`에서 환경변수만으로
named 인증을 읽고 공용 `state_*.sqlite`를 사용하며 native config와 global default가
보존되는 것을 확인했다. 실제 native 기본 파일 저장/갱신, 빈 인증에서 로그인 준비·취소,
공식 플러그인 설치·발견도 통과했다. 모델 turn과 운영 로그인 전환은 하지 않았다.
환경 저장소의 zsh 회귀 4개와 Go 관리 엔진 race/vet도 통과했다.

두 머신의 운영 백업·설치 후 검증 증거는 Git 밖의
`~/.local/state/codexm/validation/native-account-shell-20261022/`에 보관한다.
설치 결과는 환경 저장소의 `docs/codexm.md`를 따른다.
