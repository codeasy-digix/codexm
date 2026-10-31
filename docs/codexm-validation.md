# codexm 로컬 검증 기록

> 과거 검증 기록입니다. 아래 `.home/tools/codexm` 경로와 빌드 명령은 퇴역했습니다.
> 현재 설치는 제품 저장소의 [설치 관리](deployment.md)를 따릅니다.

아래는 0.1.0 최초 검증 기록이다. 이후 실제 작업 인계·계정 사용 로그 검증은
[0.1.1 로컬 인계 검증](codexm-handoff-validation.md)에 보관한다.

2026-10-10, `codexm 0.1.0`, macOS ARM64, Go 1.26.3, 공식 Codex CLI 0.162.1 기준이다.
사용자 요청에 따라 검증과 설치는 현재 Mac에서 진행했다. 다른 장비에는 적용하지 않았다.
Linux 및 Intel Mac 실행 파일은 이 Mac에서 교차 빌드했으며 해당 장비에서 실행 검증한 것으로
간주하지 않는다.

## 자동 검사

```sh
cd ~/.home/tools/codexm
go test -race ./...
go vet ./...
go build -trimpath -buildvcs=false -o /tmp/codexm-dev ./cmd/codexm
CODEXM_NATIVE_TEST=1 CODEXM_BINARY=/tmp/codexm-dev \
  go test -race ./internal/runtime ./internal/supervisor -run 'TestNative|TestOfficial' -v
```

일반 검사는 사건·미처리 동작의 원자적 저장, 중복 사건, 오래된 실행의 사건 무시,
재시작 시 불확실한 입력의 재전송 금지, 사용자 중지, 겹치는 폴더의 편집 제한,
재시도 대기 시간, 관리자 지시 범위, 잘못된 승인 응답과 한글 여러 줄 입력을 확인한다.
지원하지 않는 미래 DB 스키마는 파일을 변경하지 않고 거부한다.

계정 정책 검사는 `account-work → account-secondary → account-main → account-backup`, 세 기본 계정의 소진이
모두 확인된 경우만 fallback 허용, 미확인 상태와 인증 오류의 구분을 확인한다.
599초에는 확인하지 않고 601초에는 확인하여 account-work 복귀 동작을 기록하며,
이 과정의 관리자 모델 호출 수는 0이다. 실제 계정의 한도를 고의로 소진하지 않았으므로
한도 소진 사건과 복귀 시점은 모의 계정 리더로 재현했다.

선택적인 native 검사는 실제 공식 app-server, tmux 및 writer lock을 쓴다.
인증·한도 리더는 임시 계정으로 격리하고 모델 생성은 수행하지 않는다.
새 대화 생성, 재개, 목표 상태, writer 해제, account-work에서 account-secondary로 실행 계정 교체 후
같은 Thread·모델·추론 수준·paused 목표·30,000 토큰 예산·사용량 보존을 확인했다.

엔진 검사는 외부 Codex 파일 교체·삭제 후에도 보관한 엔진을 다시 불러오는 경우,
필수 프로토콜이 없는 새 엔진 채택 실패 시 기존 선택 유지, 보관 파일 변조 검출을 포함한다.
설치 검사에서는 잘못된 다운로드 체크섬과 같은 버전의 파일 충돌이 현재 설치를
교체하지 않는지 확인했다.

## 실제 로컬 동작

테스트 설정과 상태는 `/tmp/codexm-verification-20261010/`에 별도로 두고,
실제 로그인 계정과 기존 공식 대화 저장소를 사용했다. 원래 작업의 목표나 입력은 수정하지 않았다.

| 시나리오 | 확인한 결과 |
| --- | --- |
| 일반 새 채팅 | `CODEXM_SMOKE_OK` 응답 후 idle, 자동 추가 생성 없음 |
| TUI 후속 지시 | 한글 지시 입력, 같은 Thread에서 `CODEXM_FOLLOWUP_OK` 응답 |
| 공식 Codex TUI 접속 | 기존 두 턴 표시, 화면 종료 후 관리자 연결 유지 |
| 최초 관리자 선택 | 별도 빈 설정의 화면에서 account-backup 선택, 0600 저장, 다음 실행에서 불러오기, 모델 호출 0 |
| 명시적 관리자 판단 | 연결 검증 지시에 actions 없는 짧은 판단, 실제 관리자 사용량 기록 |
| 관리자 하위 채팅 생성 | 자식 하나 생성, 부모와 같은 폴더·읽기 전용·모델·추론 수준, `CODEXM_CHILD_OK` 응답 |
| native 목표 완료 | update_goal complete, 최종 `CODEXM_GOAL_FINAL_OK`, 턴 completed, 작업 tmux 정리 |
| 관리자 강제 종료 | 작업 서버 두 개 유지, 재시작 후 같은 Thread·Run·tmux 복구, 중복 턴 0 |
| 복구 시 모델 사용 | 추가 관리자 호출 0, 이미 완료한 입력 재전송 없음 |
| 전체 종료 | 관리한 테스트 서버·작업 tmux 모두 종료, 기존 tmux `0` 유지 |

명시적인 두 관리자 판단은 각각 공식 보고 사용량 7,359토큰과 8,694토큰을 기록했다.
조회·감시·일반 채팅·복구는 관리자 모델을 호출하지 않았다. 판단은 이전 관리자
대화를 누적하지 않는 일회성 요청이며, 사건별·시간별 호출 제한이 적용된다.

검증용 주요 Thread는 일반 채팅 `00000000-dd55-7852-997c-54ade6a6f3a5`,
관리자 자식 `00000000-124a-711a-93ac-3d1cd5bb169d`, 최종 응답 검사 목표
`00000000-f0e5-7902-950a-ac5442e668d6`다. 실제 후반 검증 요약은
`/tmp/codexm-local-smoke-result.json`에 남겼다. 임시 파일은 영구 설치 설정이 아니다.

## 설치와 적용 범위

```sh
cd ~/.home
tools/codexm/build.sh
homectl plan
homectl apply
homectl doctor --json
codexm doctor --json
```

현재 Mac의 `~/.local/bin/codexm`을 독립적인 `codexm-current` 실행 파일에 연결했고
plan/apply/doctor를 통과했다. 계정 선택 설정은 최초 실행에서 사용자가 저장하도록
남겨 두었다. 배포 묶음의 버전·파일·체크섬은 `manifests/codexm.lock.json`과
`manifests/codexm-assets.sha256`에 기록한다. 기존 homectl 배포와 Codex 설치를 바꾸지 않는다.

`codexm-v0.1.0` 배포 파일을 비공개 저장소에 등록한 뒤, `/tmp/codexm-restore-verify-20261010/`의
빈 설치 경로에서 실제 다운로드·체크섬 검사·실행을 확인했다. 검사 PATH에는 Go를 넣지 않았다.
설치한 바이너리의 버전은 `codexm 0.1.0`이며 SHA-256이 잠금 명세와 일치했다.
이 과정도 현재 Mac에서 수행했고 다른 장비에는 연결하거나 적용하지 않았다.

원래 두 작업은 임시 관리 목록에 열람 대상으로만 가져왔다.
첫 번째 `00000000-6ef7-77e5-97fc-94aca5fb1871`는 관측 당시 usageLimited,
두 번째 `00000000-a1f5-75d3-9158-4d5475ecb75f`는 complete였다.
두 대화를 소유한 외부 터미널 프로세스는 종료하거나 다시 실행하지 않았다.

다른 장비의 설치·실행 검사와 이후 공식 Codex 버전의 실제 호환성 검사는 보류했다.
실행 파일 보관은 공식 대화 DB 형식의 미래 비호환 변경까지 보장하지 않는다.
