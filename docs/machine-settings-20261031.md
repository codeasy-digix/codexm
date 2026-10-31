# 같은 Mac의 모델·effort 설정과 GUI 계정 설정

2026-10-31 조사. 운영 인증 전환, GUI 종료, 서버 설정 변경 없이 확인했다.

## 확인한 경계

- named home의 `config.toml`은 `~/.codex/config.toml`을 공유한다. 현재
  `codexm account NAME`은 이 공용 설정을 복사하거나 덮어쓰지 않는다.
  `--set-default`도 공용 `auth.json`의 선택만 변경하며 공용 설정은 보존한다.
- `model`, `model_reasoning_effort`, `desktop.enabled-reasoning-efforts`는
  로컬 공용 설정에 보존된다. 조사 시 로컬 설정은 `gpt-6.1-sol`, `max`였고
  표시 목록에도 `max`가 있었다.
- 설치된 ChatGPT 앱 26.1027.30824 (22943)는 로그인한 사용자·워크스페이스의
  서버 응답 `reasoningEffortVisibility`를 사용해 effort 표시를 결정한다.
  UI의 effort 선택 설정은 `model_picker_reasoning_effort_visibility`라는
  계정 설정으로 저장된다. 즉, 로컬 설정이 같아도 GUI 표시 목록은 계정마다
  달라질 수 있다.
- 앱에는 기존 로컬 표시 목록을 서버로 이관하는 코드가 있다. 이관은 서버
  설정의 source가 `default`일 때만 실행하며, 이미 저장된 계정별 설정을
  매번 로컬 값으로 덮어쓰지 않는다. 따라서 공용 config symlink만으로
  모든 계정의 GUI 표시 목록을 강제 일치시킬 수 없다.
- 모델 기본 effort와 지원 목록도 계정별 모델 카탈로그에서 온다. 모델
  캐시를 다른 계정에 덮어써서 같은 옵션을 제공하는 것처럼 만들지 않는다.
  기존 대화에 저장된 모델·effort는 새 대화의 공용 기본값과 별개다.

위 GUI 동작은 설치된 앱의 구현을 읽어 확인한 사실이며 공개 API 계약이 아니다.
파일 공유가 실제 설정을 잃는 현상이나 사용자가 본 과거 변경의 정확한 발생
순서까지 재현한 것은 아니다.

## 검증

`TestOfficialNativeSettingsWriteIsSharedAcrossAccounts`는 임시 디렉터리에서만
공식 GUI 번들 엔진을 실행한다. named 계정의 `config/batchWrite`로 모델,
effort, desktop 표시 목록·테마를 저장한 뒤 다른 계정 및 기본 홈의 새
app-server에서 `config/read`로 동일 값을 확인한다. 공용 파일의 실제 저장
내용과 named config 링크 유지도 확인한다. 실제 인증·대화·모델 turn은 쓰지 않는다.

공식 GUI 엔진 0.166.0-alpha.5.1과 설치 CLI 엔진 0.166.0에서 race 검사와
함께 통과했다. 전체 `go test -race -p 4 ./...`, `go vet ./...`도 통과했다.

## 적용 범위

계정 서버의 표시 목록까지 맞추면 동일 계정을 쓰는 다른 장비의 GUI에도
영향을 줄 수 있다. 사용자에게 이 범위를 확인하기 전에는 서버 설정을
갱신하지 않는다. `codexm`에 임의의 앱 패치나 비공식 서버 설정 자동 쓰기를
추가하지 않았다.

`.codex-global-state.json` 전체를 계정 사이에서 교체하는 방식은 사용하지
않는다. 이 파일에는 프로젝트·원격 호스트·대화 관련 상태도 섞여 있고 앱의
atomic rename 저장은 파일 심볼릭 링크를 교체할 수 있다.

공식 로컬 저장 위치 설명:
[Config and state locations](https://learn.chatgpt.com/docs/config-file/config-advanced#config-and-state-locations).
