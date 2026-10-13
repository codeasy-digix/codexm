# 모델 호출 없는 관리 명령 계획

2026-10-13. 사용자는 GUI 입력창에 `@codexm manage` 또는
`@codexm-manage`를 입력하면 LLM과 버튼을 거치지 않고 관리 패널이 열리기를 요청했다.

## 구현 방식

1. 공식 플러그인에 동기 `UserPromptSubmit` command 훅을 번들한다. 정확한 관리
   명령만 식별하고, 일반 대화·등록·작업 지시는 그대로 통과시킨다. 자연어 해석과
   모델 호출은 사용하지 않는다.
2. 관리 명령은 공식 로컬 플러그인 딥링크
   `codex://plugins/codexm@local-personal/app/codexm_open`로 전달한다.
   OS가 링크를 받았다는 사실과 실제 패널이 표시됐다는 사실을 구분한다.
3. 명령을 인식한 훅은 성공·실패 모두 프롬프트 제출을 차단한다. 링크 열기에 실패한
   명령이 LLM으로 전달되지 않게 한다. 입력 크기와 실행 시간을 제한하며, 사용자 입력을
   셸 명령이나 URL에 삽입하지 않는다. 원래 공식 Codex, 앱 번들, 계정·작업 상태는 수정하지 않는다.
4. `@codexm-manage` 스킬 선택이 제출하는 실제 텍스트 표현도 검사한다. 가능한 경우 기존
   계정 명령에도 같은 경로를 적용한다. `/manage`는 GUI 자체 라우터가 먼저 처리할 수 있으므로
   확인 없이 지원한다고 표기하지 않는다. 네이티브 slash 등록과 프롬프트 훅을 구분한다.
5. 설치기는 훅 자산을 불변 패키지 digest에 포함하고, 공식 엔진의 발견 여부를 검사한다.
   훅 신뢰는 호스트의 공식 절차를 따른다. 사용자 승인 상태를 파일 수정으로 위조하지 않는다.

## 검증과 반영

- 정확한 명령 일치, 일반 입력 통과, 링크 실패 시 제출 차단, 쉘 주입 방지와 설치 무결성을 검사한다.
- 임시 HOME과 가짜 링크 opener를 사용한 공식 엔진 시험에서 UserPromptSubmit 실행과
  모델 요청 0회를 확인한다. 임시 장기 프로세스는 명명된 tmux로 실행하고 정리한다.
- 관련 Go race/vet, UI가 바뀌면 해당 JS 검사, 공식 CLI·앱 엔진 통합 검사를 실행한다.
- 소스와 사용법을 commit/push하고 로컬 설치만 갱신한다. 기존 관리 작업·인증·목표·계정과
  다른 작업의 변경 파일을 보존한다. 실제 GUI 표시와 최초 훅 신뢰 단계가 남으면 명시한다.

공식 근거: [UserPromptSubmit](https://learn.chatgpt.com/docs/hooks#userpromptsubmit),
[플러그인 훅](https://learn.chatgpt.com/docs/hooks#plugin-bundled-hooks),
[MCP App 진입점과 딥링크](https://developers.openai.com/plugins/build/extensions).
