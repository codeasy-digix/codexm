# Codex Usage for macOS

Codex CLI의 주간 사용량을 macOS 메뉴 바에서 바로 확인하는 작은 네이티브 앱입니다. 실행하면 Dock 아이콘이나 일반 창 없이 메뉴 바에만 아이콘이 나타납니다.

## 주요 기능

- 주간 사용률, 남은 비율, 초기화 시각 표시
- Codex를 연상시키는 둥근 메뉴 바 테두리 안에 남은 비율 표시
- 기어 옵션에서 현재 퍼센트 또는 원형 차트 + 퍼센트 메뉴바 아이콘 선택
- 5분마다 자동 갱신 및 수동 새로고침
- Node.js나 별도 Codex CLI 설치 없이 동작하는 네이티브 Codex 런타임 내장
- 기존 `~/.codex` 로그인을 자동 재사용해 중복 로그인 방지
- 기존 로그인이 없는 경우에만 브라우저 기기 코드 로그인 지원
- macOS의 표준 로그인 항목(`SMAppService`) 선택 지원
- Dock에 나타나지 않는 표준 메뉴 바 앱(`LSUIElement`)

앱은 함께 배포되는 Codex 네이티브 실행 파일과 공식 로컬 app-server 프로토콜인 `account/rateLimits/read`를 사용합니다. 앱 자체는 `~/.codex/auth.json`의 인증 토큰을 파싱하거나 복사하지 않습니다.

## 요구 사항

- macOS 13 Ventura 이상
- ChatGPT 계정

Node.js와 Codex CLI는 필요하지 않습니다. Codex CLI에 이미 로그인된 Mac에서는 해당 로그인을 그대로 사용하며, 로그인되지 않은 Mac에서만 앱이 계정 연결을 안내합니다.

Mac App Store 빌드는 App Sandbox 정책 때문에 다른 앱이 만든 `~/.codex` 폴더를 자동으로 읽을 수 없습니다. 이 경우 최초 한 번 **기존 Codex 로그인 연결**을 눌러 `.codex` 폴더 접근을 승인합니다. 이것은 ChatGPT 재로그인이 아니며, 이후에는 보안 범위 북마크로 권한을 유지합니다.

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

빌드 결과는 `dist/Codex Usage.app`에 생성됩니다. 빌드 시 현재 CPU 아키텍처에 맞는 공식 Codex 네이티브 런타임을 앱에 포함합니다. 로컬에 런타임이 없으면 빌드 시에만 고정 버전 패키지를 내려받고 SHA-512 무결성을 검사합니다. 최종 사용자의 Mac에서는 다운로드나 Node.js 설치가 일어나지 않습니다.

App Sandbox 권한을 포함한 App Store용 번들은 다음과 같이 확인할 수 있습니다.

```sh
APP_STORE_BUILD=1 ./scripts/build-app.sh
```

이 명령은 개발 확인용 ad-hoc 서명을 사용합니다. 실제 App Store 제출 시에는 Apple Developer의 배포 인증서, 프로비저닝 프로파일 및 App Store Connect 메타데이터로 다시 서명·패키징해야 합니다.

## 참고

Codex app-server는 현재 실험적 인터페이스이므로 프로토콜이 바뀌면 앱 업데이트가 필요할 수 있습니다. 런타임은 앱 버전과 함께 고정되어 App Store 업데이트로 교체됩니다.

## 라이선스

MIT

앱에 포함된 OpenAI Codex 런타임은 Apache License 2.0을 따릅니다. 자세한 내용은 `THIRD_PARTY_NOTICES.md`와 앱 번들의 라이선스 파일을 참고하세요.
