# 2026-11-01 로컬 HTTPS 적용 검증

현재 Mac에만 소스 빌드와 Meteor 번들을 설치했다. Homebrew·릴리스·다른 장비의
codexm 설치는 변경하지 않았다. 기존 설치 링크와 웹 실행 설정은 private state에
백업했으며, 네이티브 config/auth와 codexm config의 전후 SHA-256은 동일했다.

실행한 검사:

- `go test -race -p 4 ./...`, `go vet ./...` 통과.
- 웹 typecheck와 Node 테스트 5개, 실제 Chrome의 HTTPS 브라우저 검사 2개 통과.
- Meteor production bundle 빌드·서명 검증·독립 설치와 `codexm doctor` 통과.
- 웹 직접 production 의존성과 Meteor 배포 server 의존성의 npm audit 0건.
  개발 도구의 audit 경고는 남아 있으며 이 결과에 포함하지 않는다.
- 공식 CLI 0.166.0 격리 검사에서 live writer의 archive 거부를 확인했다. 종료 후
  archive/unarchive와 동일 ID resume, 이전 입력을 포함하는 후속 모델 요청이 성공했다.
- 브라우저 fixture에서 로그인 전 snapshot/terminal 호출 거부, 잘못된 비밀번호,
  Origin 거부, 로그인 후 채팅·계정 선택, literal 입력, 재접속·종료·로그아웃을 확인했다.
- 실제 설치본에 지정된 사용자로 로그인하고 목록과 선택한 ID, 계정 화면을 읽은 뒤
  로그아웃했다. 네이티브 CLI 실행 버튼은 누르지 않았다. 브라우저 오류 0건이었다.
- 다른 네트워크의 SSH 호스트에서 공개 HTTPS의 인증서 검증과 HTTP 200을 확인했다.
  익명 ticket 요청은 401, 다른 Origin의 요청은 403이었다. 원격 장비 설정은 변경하지 않았다.

Meteor는 127.0.0.1:4317, Caddy는 80/443에서 실행한다. IP 인증서는 Let's Encrypt가
발급했고 Caddy가 갱신한다. Caddy의 `default_sni`를 설정해 SNI 없는 IP 접속도 검증했다.

상시 세션은 `codexm-web`, `codexm-https`다. 브라우저 fixture·HTTPS fixture·공식 CLI
테스트 프로세스는 종료했다. 기존 Discord 수신기는 유지했다. 실제 사용자 대화, 락,
GUI 프로세스, 계정별 설정을 수동 편집하거나 종료하지 않았다.

실제 공인 IP, 로그인 해시, 인증서 키, 스크린샷·로그는 Git에 넣지 않는다. 로컬 증거는
`~/.local/state/codexm/validation/public-web-20261101/`, 백업은
`~/.local/state/codexm/backups/public-web-20261101/`에 있다.
