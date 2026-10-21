> 이전 구현 기록입니다. 현재 명령 의미는 [native 계정·터미널 선택](native-account-shell-20261022.md)과 [README](../README.md)를 따릅니다.

# 기본 계정 CLI 인수 수정 계획

현재 동작은 0.5.4에서 native 기본 로그인 변경으로 정정했다. 아래는 0.5.2의 과거 검증 기록이다. [최종 범위와 검증](native-account-default-plan.md)을 따른다.

2026-10-13. `codexm account account-main default`는 현재 추가 인수를 거부하며
로그인 방식 오류를 표시한다. 실제 기본 계정 저장이나 서비스 반영 전에 종료한다.

1. `account NAME default`를 기본 작업 계정 저장의 명시적 동의어로 지원한다.
   기존 `account NAME`과 `account NAME --set-default`, 로그인 옵션을 유지한다.
   알 수 없는 추가 인수와 로그인 방식 충돌을 구분하여 저장 전 거부한다.
2. 격리 CLI 시험에서 저장값·다음 실행의 기본값, 잘못된 입력의 무변경과 로그인 취소,
   공식 Codex 로그인·부모 셸·기존 작업 계정 보존을 검사한다.
3. Go race/vet와 관련 공식 엔진 격리 시험을 실행하고 소스 commit/push·main 병합 후
   로컬 0.5.2 실행 파일과 관찰자·플러그인 캐시를 갱신한다. 운영 기본값은 변경하지 않는다.
   `.home` 고정 소스를 갱신하며 설정·작업·기존 worker·공식 CLI를 비교한다.

이 명령은 새 작업과 처음 등록하는 대화의 기본 계정을 저장한다. 원래 GUI 대화의
계정 인계·전역 Codex 로그인·기존 작업 계정 변경은 포함하지 않는다. 다른 장비나
정식 배포는 갱신하지 않는다.

## 구현과 격리 검증

0.5.2에서 `NAME default`를 기존 기본 계정 저장 경로에 연결했다. bare NAME,
`--set-default`, 명시적 로그인 옵션을 유지하며 알 수 없는 추가 인수는 usage와 함께
저장·인증 전에 거부한다. browser/device 충돌은 별도의 로그인 방식 오류다.

- 설치된 0.5.1로 빈 설정에서 원래 명령을 실행해 exit 1·잘못된 로그인 방식 오류와
  설정 파일이 생성되지 않음을 재현했다. 운영 설정 지문은 그대로였다.
- `go test -race ./...`, `go vet ./...`, `git diff --check`: 통과.
- CLI 시험은 명시적 default·기존 flag·default와 device 로그인, 다음 실행의 저장값,
  잘못된 인수의 무변경·인증 미실행·기존 로그인 취소/부모 셸 보존을 검사했다.
  supervisor 시험은 기본값 저장·새 작업 적용과 기존 작업·관리자 보존을 검사했다.
- 실제 빌드한 0.5.2 CLI를 빈 인증의 fixture 엔진에 연결하여 원래 명령 exit 0,
  저장 기본값 account-main, 다음 실행의 조회와 잘못된 인수의 설정 보존을 확인했다.
  이것은 CLI 저장 시험이며 운영 account-main의 실제 인증 성공 시험은 아니다.
- UI 시험 14개와 공식 CLI 0.162.1·앱 내장 엔진 0.162.0-alpha.17.2의 격리 plugin
  발견·열기 시험이 통과했다. 실제 운영 로그인·모델 턴은 생성하지 않았다.

공식 엔진 시험용 tmux는 `codexm-test-account-default-20261013`이며 종료 후 정리했다.
private 증거는 `~/.local/state/codexm/validation/account-default-20261013/`이다.

## 로컬 적용 확인

구현 `02c9ac2cfb14cb9bee22358e72ac05517108e6c1`을 main에 병합·push하고 `.home` 고정
소스로 복구·빌드·설치했다. 설치된 CLI도 빈 인증 fixture에서 원래 명령의 저장과 다음
실행의 조회를 통과했다. 운영 계정에 default 저장 명령을 실행하지 않았다.

관찰자 upgrade·watchdog·공식 plugin 캐시를 0.5.2로 갱신했다. supervisor PID 95420과
watchdog PID 95437의 실제 실행 파일이 같은 불변 바이너리이며 공식 daemon PID 13383은
유지됐다. `plugin doctor`의 버전·캐시 일치와 스킬 3개, `codexm doctor`가 통과했다.

설치 전후 기본값 account-secondary·manager/config/알림 정책·전역 로그인·공식 CLI와 작업 9개의
대화·계정·실행 의도·목표·예산·설정·run/engine/input, legacy worker PID 3개와
`.home`의 동시 작업 파일 10개가 같았다. account-backup 인증 파일의 지문은 달라졌으며 관측
계정 ID는 같았다. 기존 reader는 정상 토큰 갱신을 요청하므로 byte 보존으로 표시하지 않는다.
`homectl doctor`의 제품·공식 CLI 검사는 통과했고 전체 실패 3개는 기존 markdown2pdf
동시 개발 경로다. 영수증은 private `production-after/receipt.json`에 있다.
