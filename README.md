# Codex Usage for macOS

Codex CLI의 주간 사용량을 macOS 메뉴 바에서 바로 확인하는 작은 네이티브 앱입니다. 실행하면 Dock 아이콘이나 일반 창 없이 메뉴 바에만 아이콘이 나타납니다.

## 주요 기능

- 주간 사용률, 남은 비율, 초기화 시각 표시
- 5분마다 자동 갱신 및 수동 새로고침
- Finder에서 실행한 경우에도 Homebrew, npm, nvm, mise, asdf, Volta, Bun 설치 경로 탐색
- Codex CLI 미설치·미로그인·업데이트 필요 상태별 안내
- macOS의 표준 로그인 항목(`SMAppService`) 선택 지원
- Dock에 나타나지 않는 표준 메뉴 바 앱(`LSUIElement`)

앱은 Codex의 공식 로컬 app-server 프로토콜인 `account/rateLimits/read`를 사용합니다. `~/.codex/auth.json`을 직접 읽지 않으며 인증 토큰을 저장하거나 외부 서버로 전송하지 않습니다.

## 요구 사항

- macOS 13 Ventura 이상
- [Codex CLI](https://developers.openai.com/codex/cli/) 및 ChatGPT 로그인

Codex CLI가 없다면 터미널에서 설치하고 로그인하세요.

```sh
npm install -g @openai/codex
codex login
```

CLI가 없을 때 앱 안에서도 설치 명령 복사와 공식 안내 링크를 제공합니다.

## 설치 및 실행

저장소를 받은 뒤 다음 명령을 실행합니다.

```sh
./scripts/install.sh
```

앱은 현재 사용자용 표준 앱 폴더인 `~/Applications/Codex Usage.app`에 설치되고 즉시 실행됩니다. 이후에는 Finder의 응용 프로그램에서 평소 macOS 앱처럼 실행할 수 있습니다.

## 삭제

별도 제거 도구나 셸 명령이 필요하지 않습니다.

1. 메뉴 바의 **Codex Usage → 종료**를 누릅니다.
2. Finder에서 `~/Applications`를 엽니다.
3. **Codex Usage**를 휴지통으로 이동합니다.

로그인 시 자동 열기를 켰다면 삭제 전에 앱에서 끄거나, **시스템 설정 → 일반 → 로그인 항목**에서 표준 방식으로 관리할 수 있습니다.

## 개발

```sh
swift test
./scripts/build-app.sh
```

현재 로그인된 Codex 계정까지 포함한 통합 테스트는 명시적으로 실행할 수 있습니다.

```sh
CODEX_LIVE_TEST=1 swift test --filter LiveCodexIntegrationTests
```

빌드 결과는 `dist/Codex Usage.app`에 생성됩니다. 로컬 빌드는 ad-hoc 서명되며 배포용 Developer ID 공증은 포함하지 않습니다.

## 참고

Codex app-server는 현재 실험적 인터페이스이므로 향후 CLI 버전에서 프로토콜이 바뀔 수 있습니다. 조회 오류가 발생하면 먼저 `codex update`로 CLI를 업데이트하세요.

## 라이선스

MIT
