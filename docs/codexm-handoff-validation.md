# codexm 0.1.1 로컬 인계와 작업로그 검증

2026-10-10, 현재 Mac의 macOS ARM64, 공식 Codex CLI 0.162.1에서 확인했다.
다른 장비에 codexm을 적용하지 않았다. 기존 작업에서 인계된 목표와 권한은 유지했다.

## 실제 세션 인계

현재 이 관리 대화를 제외한 Codex 터미널 4개를 확인했다. 각 PID가 해당 Thread의
writer lock을 소유했는지 검사한 뒤 터미널에 `/exit`와 Enter를 전송했다.
4개 기존 프로세스의 종료와 writer lock 해제를 확인했다. 강제 종료는 사용하지 않았다.
기존 goal DB의 인계 전 백업과 시각·PID·Thread 대응표는 권한 0700/0600의
`~/.local/state/codexm/backups/handoff-20261010/`에 보관한다. 인증과 대화 내용은 Git에 넣지 않았다.

| Thread | 인계 후 관리 | 계정 선택 | 인계 시 작업 상태 |
| --- | --- | --- | --- |
| `00000000-6ef7-77e5-97fc-94aca5fb1871` | 동일 목표 재개 | account-work | usageLimited 목표, 진행 중 요청은 종료 시 interrupted |
| `00000000-a1f5-75d3-9158-4d5475ecb75f` | 완료 상태 보관 | account-secondary | complete 목표, 마지막 요청 완료 |
| `00000000-21c8-7a97-9c1c-0a0457c255a5` | 일반 채팅 보관 | account-work | 마지막 요청 완료, 추가 입력 대기 |
| `00000000-ccd4-79ff-9604-9d2a34be5165` | 일반 채팅 보관 | account-work | 마지막 요청 완료, 추가 입력 대기 |

첫 목표를 `resume-job`으로 재개한 뒤 동일 Thread와 목표가 active로 바뀌고
새 턴이 running인 것을 확인했다. 재개 당시 목표의 1,475,909 사용 토큰과
명시적 예산 없음은 그대로 보존했다. 완료 목표의 584,905 사용 토큰과 complete 상태도 유지했다.
완료된 다른 대화는 `manage`로 등록해 추가 모델 생성 없이 후속 지시를 받을 수 있게 했다.

미완료 작업의 실행 구간은 17:56:55 KST에 account-work로 시작했다. 18:01:29 KST 조회에서
실행시간 4분 34초, 기본 한도 49%→55%, 보조 한도 75%→76%가 기록됐고
표본이 60초 간격으로 갱신됐다. 이 %는 계정 전체 관측값이며 다른 세션 사용도 포함한다.
이전 터미널의 전체 사용 시간이나 사용률은 확인할 자료가 없어 역산하지 않았다.
관측한 이전 계정과 인계 후 선택 계정의 변화는 별도 로그에 보관했다.

이 확인 동안 관리자 모델 호출은 0회였다. 원래 첫 작업의 터미널 `/dev/ttys002`에
codexm TUI를 열어 관리 목록을 표시했다. 실행 중에는 supervisor와 작업 tmux를 유지해
사용자 화면 종료 후에도 이어갈 수 있게 했다. 재개한 목표는 이후 complete가 됐다.

18:19:19 KST에 최종 턴과 실행 환경 정리를 확인했다. 전체 실행 구간은 22분 23초,
기본 한도 49%→74%, 보조 한도 75%→79%로 종료 로그가 저장됐다. 목표 사용량은
1,770,183으로 증가했고 목표 내용·예산은 인계 전과 같았다. 기존 두 목표는 모두 complete,
다른 두 일반 대화는 idle이며 작업 tmux와 임시 검증 tmux는 모두 정리됐다.
관리 화면이 열린 동안 supervisor는 목록과 추가 지시를 제공하며 유지된다.
실제 한도 소진은 이번 실행에서 발생하지 않았다.

## 자동 검사와 실제 TUI

```sh
cd ~/.home/tools/codexm
go test -race ./...
go vet ./...
CODEXM_NATIVE_TEST=1 CODEXM_BINARY=~/.local/bin/codexm \
  go test -race ./internal/runtime ./internal/supervisor -run 'TestNative|TestOfficial' -v
```

저장·재시작 후 시작 시각과 종료 시각 보존, 중복 종료의 멱등성, 종료 뒤 표본 추가 금지,
계정 변경과 Job 계정의 원자적 저장, 작업별 로그 필터, 0%와 미확인 값 구분,
한도 갱신 및 종류 변경 시 차이 계산 금지, 완료 대화 관리 시 모델 호출 없음,
TUI 로그 갱신이 입력 포커스를 빼앗거나 Esc 뒤 화면을 다시 열지 않는 것을 검사했다.

공식 엔진 검사에서는 격리된 임시 계정·상태로 account-work→account-secondary 실행 환경을 교체했다.
같은 Thread·모델·추론 수준·중지된 목표·30,000 토큰 예산·기존 사용량을 보존하고,
두 계정의 실행 구간과 변경 시각·사유 및 첫 구간 종료가 로그에 남는 것을 확인했다.
이 검사의 한도 리더는 모의값이며 모델 생성은 없다. 실제 계정 한도 소진을 고의로
발생시키지는 않았다. 600초 상위 계정 재확인 정책도 기존 모의 검사로 확인한다.

`codexm-tui-check` tmux에서 실제 관리 목록과 `l` 로그 화면을 열어
관리자 account-backup, 실행 계정 account-work, 계정 전체 한도 표시가 보이는 것을 확인했다.
해당 임시 tmux는 종료 후 제거했다. 실제 작업의 tmux와 기존 `0` 세션은 유지했다.

## 조회와 제어

```sh
codexm                    # 현재 관리 목록과 후속 지시
codexm logs               # 전체 실행과 계정 변경
codexm logs 76cc42730137961e0f8e0d64 --json
tmux attach -t codexm-supervisor
codexm attach 76cc42730137961e0f8e0d64  # 같은 작업의 공식 Codex TUI
codexm pause 76cc42730137961e0f8e0d64
codexm resume-job 76cc42730137961e0f8e0d64
```

진행 중인 작업의 tmux 이름은 `codexm status --json`에서 확인한다.
작업 종료 시 실행 환경은 정리하며, 저장된 로그는 이후에도 조회할 수 있다.

## 로컬 배포와 복구

`codexm-v0.1.1`의 독립적인 배포 파일을 비공개 저장소에 등록했다.
현재 Mac의 바이너리를 실행 검증했고 Linux/Intel Mac용 파일은 이 Mac에서 교차 빌드했다.
`/tmp/codexm-restore-verify-011/`의 빈 설치 경로에 Go를 제외한 PATH로 실제 배포 파일을
다운로드했다. 설치 스크립트의 체크섬 검사·버전 확인을 통과하고, 복구한 바이너리의
SHA-256이 잠금 명세와 일치했다. 결과는 그 폴더의 `result.json`에 보관한다.
기존 목표는 보관된 공식 Codex 엔진으로 계속 실행하며, CLI 배포 파일 갱신으로
진행 중인 작업 엔진을 교체하지 않는다. 다른 장비에는 설치하거나 적용하지 않았다.
