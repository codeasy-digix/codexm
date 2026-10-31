# codexm 0.1.2 관리 화면과 별도 터미널 검증

> 과거 검증 기록입니다. 아래 `tools/codexm` 경로는 퇴역했습니다.
> 현재 설치는 제품 저장소의 [설치 관리](deployment.md)를 따릅니다.

2026-10-10, 현재 Mac의 macOS ARM64와 보관한 공식 Codex CLI 0.162.1에서 확인했다.
이 기록은 동일 버전의 미배포 초안에 있던 Codex 명령 복제/접두사 방식을 대체한다.

## 구현 범위

- codexm은 목록·상태·로그·사용량·계정 정책을 관리한다. 채팅 기능은 공식 Codex가 제공한다.
- 목록 Enter 또는 `/open`으로 선택한 계정/세션을 새 Terminal 창에서 연다.
- 관리자 명령에는 접두사가 없다. `/status`, `/usage`는 전체, `/info`는 선택 세션이다.
- `/logs`는 선택한 작업, `/logs all`은 전체 실행/계정 변경 기록이다.
- 입력창은 기본 포커스이며 일반 텍스트는 작업 검색이다. `/manager`는 명시적인 모델 호출이다.
- `/` 명령 목록·필터·Tab 완성·Esc 초안 보존과 좁은 터미널 표시를 유지한다.
- 완료/일시 중지한 대화 열기는 실행 환경만 준비한다. 목표 상태·예산·누적 토큰을 유지한다.
- 새 빈 채팅은 제목을 저장해 공식 TUI에서 즉시 불러올 수 있게 준비한다.
- 새 터미널의 환경은 해당 계정으로 격리한다. 전역 계정 선택은 바꾸지 않는다.
- 실행 계정/소켓이 교체되면 열린 화면을 같은 새 실행으로 연결한다. 정상 `/quit`는 화면만 닫는다.
- 관리자 `/pause`, `/stop`은 실행을 제어하며 화면을 닫는다. 완료 실행은 마지막 화면 종료 후 정리한다.

## 자동 검사

```sh
cd tools/codexm
go test -race ./...
go vet ./...
CODEXM_NATIVE_TEST=1 CODEXM_BINARY=/absolute/path/to/codexm \
  go test -race ./internal/supervisor -run TestOfficialNativeWorkerLifecycleAndWriterLock -count=1 -v
```

전체 테스트와 race/vet 검사가 통과했다. 공식 엔진 검사에서는 빈 채팅의 제목/이력 파일,
공식 화면과 같은 path 기반 `thread/resume`, 실제 writer 잠금과 해제, account-work→account-secondary 계정
교체 후 세션·모델·추론·paused 목표/30,000 토큰 예산 보존을 확인했다. 완료 목표를
다시 열어도 누적 토큰과 예산을 유지하고 생성 지시를 큐에 넣지 않았다.

실제 자식 프로세스를 사용하는 화면 연결 테스트는 계정/소켓 교체, 교체 준비 동안 대기,
정상 창 종료와 종료 코드 0인 연결 종료의 구분, 일시 중지 후 재접속 방지를 검사했다.
실제 한도 소진을 유도하는 모델 작업은 수행하지 않았다.

## 실제 Terminal 연결

운영 대화와 별도인 0700 테스트 설정/DB/이력과 임시 계정 인증 사본을 사용했다.
`codexm open JOB_ID`로 Mac Terminal 새 창이 열리고 공식 Codex 입력 화면이 표시됐다.
해당 프로세스의 환경/인수에서 다음을 확인했다. 인증 내용은 출력하지 않았다.

- 계정 `account-secondary`와 그 계정의 격리된 CODEX_HOME
- 관리자에 기록된 정확한 thread ID와 해당 작업의 app-server 소켓
- 관리 화면과 별개인 공식 Codex 자식 프로세스
- 관리자 모델 호출 0회, 사용자 턴/토큰 생성 없음
- `/pause` 후 작업 실행 정리, 화면 연결 종료, 일시 중지 상태 유지

처음에는 지시가 없는 빈 대화의 rollout이 아직 준비되지 않아 공식 화면의 resume가
실패했다. `thread/name/set`으로 제목을 저장해 이력을 준비한 뒤 실제 새 창 연결을
재검증했고, 이 경로를 공식 엔진 테스트에 추가했다.

## 실제 관리 화면

`codexm-manager012-test-ui` tmux에서 120×40과 40×20 크기로 실행했다.
다음 9개 검사가 통과했고, 작업 지시나 관리자 모델 호출을 보내지 않았다.

- `/`에서 관리 목록 표시, `/open` 존재, 기존 접두사 없음
- 기본 입력 포커스에서 한글 입력
- 일반 텍스트 + Enter가 작업 검색으로 동작
- `/usage`에 네 계정과 잔여 한도 표시, 내부 JSON 제외
- `/logs all`의 전체 세션 범위
- `/logs`의 선택 작업 범위
- `/status`의 전체 관리 상태
- Codex 채팅 명령이 관리 화면에서 실행/복제되지 않음
- 40×20에서 명령 목록과 입력창 표시

검증 결과는 `~/.local/state/codexm/validation/manager-012/`의 private JSON에 남겼다.
테스트 UI·supervisor·worker tmux, 새 테스트 Terminal 창과 인증 사본은 정리했다.

## 적용 범위

현재 Mac만 설치·실행 검증한다. macOS Intel과 Linux 두 아키텍처는 cross compile만
확인하며, 다른 장비에 로그인하거나 설치/재시작하지 않는다. 보관한 공식 엔진은
upstream CLI 자동 업데이트와 별개로 유지한다.

## 현재 Mac 설치 후 확인

private 구성요소의 Mac ARM64 파일을 다운로드하고 manifest 체크섬과 실제 설치 바이너리의
SHA-256 일치를 확인했다. 감시 서비스도 0.1.2로 전환하고 새 관리 Terminal 화면을 열었다.
기존 4개 세션 ID, 관리자 account-backup 선택, 두 완료 목표의 상태/누적 토큰을 보존했다.
외부 프로세스에서 열려 있는 대화의 writer와 실행은 변경하지 않았다.

실제 완료 세션 `00000000-6ef7-77e5-97fc-94aca5fb1871`를 account-work 계정의 새 공식 화면으로
열어 thread ID/소켓과 native 자식 프로세스 연결을 확인했다. 목표는 complete와
1,770,183 tokens를 유지했고 관리자 모델 호출은 0회였다. 검증 화면을 닫은 후 완료
실행 tmux가 정리됐고, 새 관리 화면과 `codexm-supervisor`만 관리용으로 유지한다.
감시 접속은 `tmux attach -t codexm-supervisor`다.
