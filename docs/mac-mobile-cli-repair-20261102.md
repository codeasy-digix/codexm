# mac-mobile의 CLI 패키지와 Documents 접근 진단

2026-11-02. 대상은 `ssh mac-mobile`이며 제품 코드·빌드·공개 배포는 변경하지 않았다.

## 패키지 누락: 수정 및 목록 확인 완료

`~/.local/bin/codex`가 바이너리 하나만 복사된
`~/.local/share/codex/versions/0.166.0/bin/codex`를 먼저 실행했다.
이 디렉터리에는 `codex-package.json`, 보조 실행 파일, resources/path 디렉터리가
없었다. `--version`과 `--no-daemon` 실행 성공만으로 정상 설치를 판정할 수 없다.

같은 장비에는 정식 npm `@openai/codex` 0.166.0이 이미 설치되어 있었다.
기존 복사본과 npm 패키지의 native 실행 파일 SHA-256은 같았으며 서명 검증도
통과했다. npm 패키지에는 manifest와 보조 파일이 함께 있었다.

앞을 가리던 `~/.local/bin/codex` 심볼릭 링크만 private 백업 디렉터리로 이동했다.
기존 복사본과 정식 npm 패키지는 보존했다. 새 로그인 셸의 `codex`는 nvm의
공식 npm launcher를, codexm의 resolver는 그 패키지 안의 native 실행 파일을
선택한다. wrapper나 `--no-daemon` alias를 추가하지 않았다.

실제 mac-mobile의 전용 tmux PTY에서 다음을 확인했다.

- `codex --version`: 0.166.0.
- `codex resume --all`: 기존 채팅 선택 목록 표시. 채팅을 선택하거나 작업을
  전송하지 않고 Ctrl+C로 종료했다. 공식 CLI가 필요한 daemon package를 준비했다.
- `codex app-server daemon version`: CLI/managed package/app-server 모두 0.166.0.
- `codexm`: TUI에서 활성 채팅 143개, 계정 목록 표시.
- `codexm status --json`: 전체 148개, 보관 5개, 공식 npm native 경로 선택.
- 수정 전후 default auth, native config, codexm config 해시 일치.

기존 셸이 제거된 경로를 캐시했다면 `rehash` 후 사용한다. 정식 CLI의 향후
업데이트는 해당 npm 설치를 대상으로 한다. 제품 설치기가 Codex 바이너리를
단독 복사하거나 패키지 구조를 대신 구성하면 안 된다.

## Operation not permitted: macOS 승인 대기

오류 직전 codexm의 마지막 실행 영수증은 `~/Documents/Codex/...`를 작업
디렉터리로 넘겼다. 14:06:34 KST의 TCC 로그에서 SSH 책임 프로세스
`sshd-keygen-wrapper`를 통한 Codex의 전체 디스크/문서 폴더 권한 요청을
확인했다. 새 SSH 연결에서 해당 프로젝트와 `~/Documents`를 `ls`해도
`Operation not permitted`가 재현됐다. 목록 조회와 실제 프로젝트 접근은
별도 검증 대상이다.

시스템 설정의 **일반 → 공유 → 원격 로그인 ⓘ → 원격 사용자의 전체 디스크
접근 허용**을 사용자가 인증하여 켜야 한다. 시스템 설정만 열었으며 TCC DB,
보안 정책, 프로젝트 위치와 대화 데이터는 수정하지 않았다. 사용자 승인 전에는
프로젝트 재개까지 복구됐다고 주장하지 않는다.

승인 후 새 SSH 연결로 Documents 접근을 다시 확인하고, 기존 GUI/CLI 작업을
중단하지 않는 범위에서 선택한 채팅의 재개를 확인한다. GUI 종료가 필요하면
별도 실행별 확인 규칙을 그대로 적용한다.

백업과 화면 증거는 mac-mobile의
`~/.local/state/codexm/validation/mac-mobile-cli-repair-20261102/`에 보관한다.
실사용 채팅 내용·인증 파일·로그 원문은 Git에 포함하지 않는다.

참고: [공식 CLI 설치](https://developers.openai.com/codex/cli/),
[Apple 원격 로그인 안내](https://support.apple.com/guide/mac-help/mchlp1066/mac).
