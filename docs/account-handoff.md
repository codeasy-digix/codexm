# 같은 채팅의 계정 교체

계정 선택과 기존 공식 CLI의 실행 구조는 유지한다. 명시적인 교체 요청을 받았을 때만
짧게 실행되는 helper가 **기존 CLI 종료 → 잠금 해제 확인 → 같은 ID의 새 CLI 실행**을
담당한다. 별도 요약, 대화 복제, 새 thread ID, 모델 관리자와 상주 감시기는 없다.

```sh
# 일반 재개와 즉시 작업 재개
codexm resume THREAD_ID --account NAME
codexm resume THREAD_ID --account NAME --continue

# 실행 중인 codexm CLI의 계정 교체
codexm handoff THREAD_ID --account NEXT_ACCOUNT
codexm handoff THREAD_ID --account NEXT_ACCOUNT --if-low 5

# 현재 사용량과 접수한 인계 결과
codexm usage --all
codexm usage --account NAME --model MODEL
codexm handoff-status THREAD_ID
```

`--continue`는 공식 `codex resume ID PROMPT` 인자로 고정된 계속 진행 지시를 한 번
전달한다. 이미 실행 중인 터미널에는 다시 주입하지 않는다. 기존 요청, 사용자 중지,
승인 대기, 목표와 예산을 유지하고 불확실한 외부 작업의 결과부터 확인하도록 한다.
프로세스 메모리나 실행 중 도구를 옮기는 기능은 아니며 저장된 같은 대화를 이어간다.

## 한도와 에이전트 사용

`usage`는 계정별 공식 사용량 RPC를 조회한다. JSON의
`five_hour_remaining_percent`, `weekly_remaining_percent`, `checked_at`, `error`를
사용한다. 컨텍스트 사용률과 혼동하지 않는다. 모델별 한도가 있으면 `--model`을
명시하며, handoff는 대화 인덱스의 모델 정보를 사용한다. 조회 실패·인증 실패·없는
한도는 0%로 간주하지 않는다. 알 수 없는 상태에서는 기존 CLI를 유지한다.

현재 계정의 두 한도 중 하나가 `--if-low` 이하일 때만 인계한다. 다음 계정은 둘 다
5%를 초과해야 한다. 로그인 ID가 같은 별칭은 거절한다. 대상 계정은 호출자가
지정하며, 과거 worker 우선순위나 전역 기본 로그인은 바꾸지 않는다.

에이전트 지침 예시:

> codexm이 실행한 CLI에서만 작업 구간이 끝날 때 실제 계정 사용량을 확인한다.
> `CODEXM_NATIVE_THREAD_ID`와 현재 `CODEX_THREAD_ID`가 일치할 때,
> `codexm usage --account "$CODEXM_NATIVE_ACCOUNT"`의 5h 또는 주간 잔여량이
> 5% 이하이면 등록된 다른 계정의 사용량을 확인한다. 둘 다 5%를 초과하는 계정을
> 골라 `codexm handoff "$CODEX_THREAD_ID" --account NEXT --if-low 5`를 한 번
> 요청한다. 별도 요약은 만들지 않는다. 접수 후 결과가 불명확하면 재요청하지 않는다.
> 적합한 계정이 없거나 사용량을 확인할 수 없으면 사용자에게 알린다.

환경 변수는 안내용이다. 실제 종료 권한은 실행 영수증과 OS 소유권으로 검증한다.
5%는 사전 인계 기준이며 한 번의 긴 작업, 다른 세션의 동시 사용으로 그 전에 한도가
소진될 수 있다. 지침을 따른 요청 방식이므로 즉각적·상시 자동 전환을 보장하지 않는다.

## 실행 범위

- 새 버전의 codexm으로 시작한 CLI는 PID, OS 시작 식별자, native 실행 파일과
  로그인 식별 해시를 영수증에 남긴다. 인증 토큰은 기록하지 않는다.
- helper는 원래 프로세스가 선택한 채팅 하나만 소유하는지 다시 확인한다.
  PID 재사용, 다른 채팅으로 이동, 다중 채팅 소유, 변경된 로그인과 오래된 영수증은
  거절한다. 직접 실행한 `codex`와 업데이트 전 실행은 자동 종료 대상으로 삼지 않는다.
- SIGTERM 뒤 기존 프로세스와 잠금이 모두 없어져야 새 CLI를 시작한다. 제한 시간 안에
  종료되지 않으면 실패로 남기며 SIGKILL이나 잠금 파일 삭제를 하지 않는다.
- 새 CLI는 default 서버의 `codexm` 세션 안에 대화별 창으로 실행된다. 검증된 종료 pane을
  보고 있던 클라이언트만 각각 독립적인 임시 그룹 세션에서 새 창으로 연결한다.
  다른 채팅을 보는 클라이언트는 유지한다. 옛 pane만 정리하며 그룹 세션은 마지막 접속
  종료 시 자동 정리된다. `codexm` 또는 출력된 `codexm terminal SESSION`으로 다시 연결한다.
- GUI 소유 대화는 실제 foreground 터미널에서 실행한 명령만 기존의 매번 확인하는
  GUI 재시작 경로를 쓸 수 있다. GUI 안의 에이전트 도구 호출이나 `--if-low`로는
  GUI 종료를 승인하지 않는다. 다른 GUI 작업까지 중단된다는 안내는 유지한다.

## 결과와 실패

`handoff`의 성공 종료는 **요청 접수**다. 결과는 `handoff-status`로 확인한다.
`accepted → stopping → starting → started` 순서이며 `started`는 새 CLI가 같은
채팅의 writer를 확보했음을 뜻한다. 모델 응답 완료를 뜻하지 않는다. 실패는 `failed`와
오류로 보존한다. 시작/프롬프트 수신이 불명확하면 자동으로 재실행하지 않는다.

인계 상태는 `~/.local/state/codexm/native-cli/switches/` 및 `switch-status/`에 남는다.
helper의 이름은 `codexm-handoff-*`이며 정상/실패 후 자동 종료한다. 대화 DB, rollout,
전역 기본 로그인, 원본 `codex` 실행 파일과 업데이트 설정은 직접 변경하지 않는다.

운영 중 GUI나 사용자 채팅을 검증 목적으로 중단하지 않는다. 새로운 인계 기능은
먼저 로컬에만 설치하며 원격 반영과 공개 배포는 별도 적용 요청에 따른다.
