> 이 문서는 이전 관리형 codexm의 기록입니다. 현재 공개 실행 경로는 채팅 목록·계정·공식 CLI 인계로 축약했습니다. [현재 설계](native-cli-core.md)와 [README](../README.md)를 따르세요.

# 배포 준비

제품 저장소는 `codeasy-digix/codexm`이며 현재는 소스와 로컬 설치만 준비한다.
CI에는 테스트만 있고 태그 배포나 레지스트리 업로드는 없다.

```sh
go run ./cmd/package
```

이 명령은 네 플랫폼(macOS/Linux × ARM64/AMD64)의 `codexm`만 빌드하고
`dist/prepared/`에 압축 파일, `SHA256SUMS`, Homebrew 공식과 npm 디렉터리를 만든다.
history-sync는 제거되어 코드나 실행 파일에 포함하지 않는다.
공식 Codex 실행 파일·인증·대화도 패키지에 포함하지 않는다.
준비된 모든 진입점의 이름은 `codexm`이다. `codex` 파일이나 셸 함수를 설치하지 않는다.

Homebrew 공식은 검증한 실제 빌드 해시를 사용한다. 공식이 가리키는 GitHub Release는
아직 생성하지 않으며, 저장소가 private인 동안 공개 tap에 적용하지 않는다.
npm 디렉터리는 네 플랫폼 실행 파일을 포함하고 Node로 해당 파일만 실행한다.
설치 시 다운로드·로그인·데몬 실행은 없으며 `private: true`로 게시를 차단한다.
정식 게시 전 npm 이름/권한과 공개 여부를 결정하고 이 설정을 명시적으로 바꿔야 한다.

향후 배포 순서는 로컬/플랫폼 검증 → 소스 버전 확정 → release 업로드 → 공개·인증
정책에 맞는 tap/npm 게시 → 이전 account 배포 경로 은퇴다. 지금은 게시하지 않는다.
기존 account 저장소는 자동 release와 공식 생성기를 제거한다. 이미 게시된 과거
release/tap은 이 준비 작업에서 삭제하지 않는다. 공식 `@openai/codex`는 별도 제품이다.

독립 설치는 `./install.sh --build`를 사용한다. 정식 release 이후 `./install.sh`는
GitHub archive의 SHA-256을 검사한 다음 버전을 선택한다. 실패한 다운로드나 충돌은
현재 설치를 보존한다. 로컬 소스 빌드는 실행 파일 hash가 포함된 별도 immutable 경로를
사용해 다음 개발 빌드와 정식 Release가 충돌하지 않는다. `.home`은 이 과정에 관여하지 않는다.
제품 소스의 빌드·설치기는 이 저장소에서만 실행한다.

소스/설치 소유권은 [환경 관리](deployment.md)를 따른다.
`.home`은 제품 소스 pin/clone/build/install/update나 바이너리 배포를 수행하지 않는다.

2026-11-01 사용자 지시: Homebrew 등 배포 작업은 정리가 끝난 뒤 별도 요청 전까지
진행하지 않는다. 현재 작업은 소스 정리와 로컬 설치·검증에 한정한다.
