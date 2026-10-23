# 계정 삭제와 실행 시 우선순위 선택

## 확인한 문제

`RemoveRegistration`은 실제 프로세스 유무 대신 `Managed && !Terminal`인
과거 계정 배정까지 사용 중으로 판정했다. 로컬 account-work 배정은 paused, idle,
auth_required 상태이며 연결된 worker가 없어도 삭제가 거부됐다. GUI의 공용
로그인과 작업의 과거 배정은 서로 다른 정보다.

## 변경 계약

1. 작업 시작·재개·새 turn 전송마다 남은 계정을 현재 우선순위로 조회한다.
   저장된 Account/InitialAccount는 다음 실행의 고정 계정이 아니다.
   이미 진행 중인 turn에 지시를 추가하는 steer는 현재 실행을 유지한다.
   단순 열람은 모델을 실행하지 않으며 최근 관측을 재사용할 수 있다.
2. 계정 삭제는 목록·우선순위·예비 계정에서 즉시 등록을 해제한다.
   idle/paused/auth_required 등 실행 없는 배정은 지운다. pause, goal,
   예산, thread ID, 입력 대기열은 보존한다. 과거 실행의 계정 기록은 남긴다.
3. 소유 worker가 있으면 durable 인계 작업으로 안전하게 중지하고 다음
   가용 계정에서 이어간다. GUI 등 외부 writer는 종료하거나 잠금을 제거하지
   않는다. 선택할 계정이 없으면 작업을 보존하고 가용 계정을 기다린다.
4. 설정의 removed 목록을 복구 기준으로 삼는다. 설정 저장 직후 장애가 나도
   재접속 시 배정 정리가 재실행된다. 삭제·새 작업·계정 선택 경합에서 삭제한
   계정에 새 실행이 붙지 않게 한다. 중복 요청은 기존 operation 결과를 쓴다.
5. 관리자 역할은 별도이며 자동으로 다른 관리자로 바꾸지 않는다. 관리자 삭제는
   역할을 먼저 바꾸라는 명확한 안내를 유지한다. 진행 중 로그인은 파일 변경
   경합을 막기 위해 완료 후 삭제한다. 로그인 파일·공용 로그인·대화는 보존한다.
6. CLI/TUI/Meteor/plugin은 같은 Go 정책과 결과를 사용한다. 삭제 확인에 자동
   배정 정리와 인계/대기 동작을 표시한다.

## 검증과 적용

격리된 계정·상태 디렉터리에서 중지/완료/실행/외부 writer, 전 계정 소진,
unknown과 예비 계정 제한, 재시작·요청 재전송·삭제 경합을 검증한다.
매 실행 시 새 quota 응답으로 선택이 바뀌는지 확인한다. Go race/vet 및
관련 native 통합, plugin/web 확인을 실행한다. 실제 계정 삭제나 GUI writer
종료는 하지 않는다. 설치 후 observer만 안전하게 업그레이드하고 기존 worker,
계정 로그인, goal/예산이 유지되는지 확인한다. 최종 소스는 main에 반영·push한다.

## 구현과 검증 결과

- 삭제한 별칭의 실행 없는 배정과 대기 중 수동 전환을 자동 해제한다. 실제 worker는
  `account_reassignment`와 durable `reassign_account` 작업으로 안전하게 정리한다.
  새 작업과 경합하는 삭제도 dispatch gate에서 처리한다. 이미 죽은 RPC 연결 때문에
  인계가 무한 대기하지 않도록 정리하며, 재시작은 tombstone에서 복구한다.
- start/resume/send 및 목표의 다음 turn은 새 한도 응답으로 우선순위를 확인한다.
  수동 전환은 해당 인계에만 적용되고 이후 요청을 고정하지 않는다. 단순 열람만
  유효한 최근 관측을 재사용한다. 실패한 관측은 캐시로 가용 판정하지 않는다.
- 기존 플러그인 연결과 호환되도록 `accounts_api=1`은 유지하고
  `account_reassignment_api=1`을 추가했다. 새 클라이언트는 인계 지원을 확인한다.
  GUI/native 로그인은 별도로 보존하며, 삭제한 worker 별칭도 원래 GUI 로그인이라는
  출처 정보로 기록할 수 있다. 계정 미배정 자동 작업도 등록 성공으로 처리한다.
- `go test -race -p 4 ./...`, `go vet ./...` 통과.
- 공식 Codex 0.164.0으로 격리된 native 통합 검사 4개 통과: 삭제와 다음 우선 계정
  연결, 원본 writer/paused goal 보존, observer 교체·실패 rollback·명시 종료,
  기존 대화·예산·열람 보존. 같은 thread ID를 유지하고 모델 turn은 생성하지 않았다.
- plugin UI 18개, 웹 TypeScript 검사와 Node 검사 3개, Meteor 배포 번들 빌드 통과.
  실행권을 가진 GUI를 강제로 종료하거나 실제 저장 계정을 삭제하지 않았다.

검증 로그·배포 번들·설치 전 관리 DB 백업은 로컬 private 경로
`~/.local/state/codexm/validation/account-removal-priority-20261023/`에 보관한다.
실행권을 가진 외부 GUI 연결을 스킬에서 해제하는 기능은 이 변경에 포함되지 않는다.
