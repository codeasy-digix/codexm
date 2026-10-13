# codexm 명령형 진입 마무리 계획

2026-10-13. 전체 목표의 후속 구현이며 기존 GUI 계정 인계 요구를 축소하지 않는다.
사용자가 제공한 실제 Codex 앱 사진으로 관리 패널의 표시와 서비스 연결을 확인했다.
사진의 실행 경로는 자연어 요청을 모델이 처리한 것이므로 전용 명령 진입의 완료 증거는 아니다.

1. 기존 `@codexm` 스킬에 `manage`와 `accounts` 동작을 구분한다. `manage`는 관리 패널,
   `accounts`는 계정 탭을 한 번 열고 종료한다. 화면 열기를 대화 등록이나 새 작업 시작으로
   해석하지 않는다. 일반 설명·현재 대화 등록·현황 확인은 기존 범위를 유지한다.
2. GUI 스킬 선택용 `codexm-manage`, `codexm-accounts`를 같은 플러그인에 묶는다.
   GUI에서는 @ 선택, CLI/IDE에서는 $ 선택으로 호출하며 실제 명령 이름과 도구를 연결한다.
   스킬은 모델을 거치는 단축 호출임을 명시한다. native `/codexm`나 토큰 없는 slash 명령으로
   표기하지 않는다. 모델 없이 여는 기존 공식 sidebar/thread/settings 버튼은 유지한다.
3. 계정 열기 도구만 모델에서 호출 가능하게 한다. 로그인 start/status/cancel과 모든 관리
   변경 도구는 app-only로 유지한다. 민감 데이터는 기존 UI-only metadata 경계를 유지한다.
4. 설치기의 고정된 단일 스킬 파일 목록을 실제 bundled skill 수집으로 바꾼다. 모든 자산의
   정렬된 경로와 내용을 digest에 포함해 캐시/불변 자산을 일관되게 갱신한다. 공식 엔진이
   등록 스킬과 명령 스킬을 각각 발견하는지 검사한다.
5. 기존 GUI 대화의 동일 thread 계정 인계는 독립 읽기 감사로 최신 공개 API를 재확인한다.
   공식 경로가 있으면 구현·시험한다. 없으면 잠금 삭제·전역 로그인 변경·fork로 대체하거나
   전체 목표를 완료 처리하지 않는다.
6. Go race/vet, skill validator, JS 회귀, 실제 공식 엔진의 package/skills/MCP 발견·열기 시험을
   수행한다. 로그인·모델 턴은 불필요한 시험에서 만들지 않는다. 장시간 fixture는 named tmux이며
   시험 후 정리한다. 코드·기록은 commit/push하고 로컬 고정 설치·서비스·plugin 캐시를 갱신한다.
   `.home` pin/문서를 갱신하며 작업/목표/예산/인증/기존 worker/공식 CLI를 비교한다.

공식 근거: [스킬](https://learn.chatgpt.com/docs/build-skills),
[Plugin Extensions](https://developers.openai.com/plugins/build/extensions).
