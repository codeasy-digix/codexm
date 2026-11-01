# HTTPS 웹 접속과 로그인

웹은 Meteor 3.5.2와 React로 구성한다. HTTP 세션 쿠키와 일회용 DDP ticket으로
인증한다. 서버 자체의 사용자 한 명을 설정하는 방식으로, Codex 계정 인증이나
다른 장비의 설정을 변경하지 않는다. 회원가입·암호 복구 메일 기능은 제공하지 않는다.

## 구성

1. `codexm web user --username YOUR_USERNAME`으로 비밀번호를 입력한다. 자동화 시에는
   `--password-stdin`을 사용하며 비밀번호를 명령행 인수·스크립트·Git에 넣지 않는다.
2. `codexm web --public-origin https://YOUR_HOST --no-open`으로 Meteor를 실행한다.
   `YOUR_HOST`는 공개 도메인 또는 공인 IP다. Meteor는 `127.0.0.1:4317`에만 바인딩한다.
   이 명령만으로 HTTPS 프록시까지 시작하지는 않는다.
3. 기존 Caddy 2.11 이상으로 [설정 예시](../deploy/Caddyfile.example)를 적용한다.
   `CODEXM_PUBLIC_HOST`, `CODEXM_TLS_STORAGE`, `CODEXM_CERT_EMAIL` 값을 지정하거나,
   로컬 `~/.config/codexm/Caddyfile`에 실제 값으로 치환한다. 프록시도 이름 있는 tmux에서
   실행한다. IP 인증서는 shortlived ACME 프로필을 사용하고 Caddy가 자동 갱신한다.
   `default_sni`는 SNI를 보내지 않는 IP 접속에서도 올바른 인증서를 선택한다.
4. 외부에서 80/443이 해당 장비로 연결되도록 한다. 80은 HTTPS 리다이렉트와 ACME 검증,
   443은 로그인·웹·WebSocket에 사용한다. 4317은 외부에 열지 않는다.

인증서를 발급할 수 없는 구성에서 HTTP 로그인으로 자동 전환하지 않는다.
공인 IP가 바뀌면 공개 Origin과 Caddy 호스트를 함께 변경하고 인증서를 다시 발급한다.

## 실행과 중지

```sh
codexm web --public-origin https://YOUR_HOST --no-open
tmux new-session -d -s codexm-https 'exec caddy run --config "$HOME/.config/codexm/Caddyfile" --adapter caddyfile >> "$HOME/.local/state/codexm/web/https.log" 2>&1'
codexm web status
tmux attach -t codexm-web
tmux attach -t codexm-https
```

이미 실행 중이면 중복으로 tmux 세션을 만들지 않는다. 설정 변경이나 설치 후에는
`codexm web stop`과 `tmux send-keys -t codexm-https: C-c`로 각각 종료한 뒤 위 명령으로
다시 시작한다. 두 서비스가 내려가도 별도 `codexm-cli-*`의 작업은 계속 실행된다.
tmux 서비스는 로그아웃 후 유지되지만 재부팅 자동 시작은 이 설정에 포함하지 않는다.

웹 로그는 `codexm web logs`, TLS 로그는 `~/.local/state/codexm/web/https.log`에 있다.
`web status`는 Meteor 프로세스와 로컬 리스너를 확인한다. 외부 경로와 TLS까지 정상인지
판단하려면 다른 네트워크에서 공개 URL에 접속해 로그인과 WebSocket 연결을 확인한다.

## 인증 데이터

`<state_dir>/web/login.json`은 mode 0600으로 PBKDF2-SHA256 600,000회·임의 salt를
사용한 비밀번호 해시만 보관한다. 브라우저 세션은 8시간이며 HttpOnly·SameSite=Strict,
외부 HTTPS에서는 Secure 쿠키를 사용한다. 비밀번호를 다시 설정하면 이전 세션과
DDP ticket은 무효가 된다. 로그아웃하면 해당 세션의 DDP 연결도 닫힌다.

로그인은 클라이언트별·전체 요청 빈도와 동시 해시 계산 수를 제한한다. Caddy는
`X-Codexm-Client-IP`를 직접 설정하며, 인터넷 클라이언트가 보낸 값을 신뢰하지 않는다.
Host·Origin이 설정된 HTTPS 주소와 다르면 요청을 거부한다. 패스워드 모드에서는
기존 일회용 접속 코드가 있어도 로그인 우회에 사용할 수 없다.

## 선택한 채팅의 실행

웹에서 채팅과 계정을 선택하면 공식 `archive → unarchive → resume` 명령을 사용해
같은 채팅 ID를 재개한다. 이미 보관된 채팅은 unarchive부터 시작한다. 이후 입력은
그 CLI의 tmux 터미널에 그대로 전달하며 다른 관리 모델에 일을 맡기지 않는다.

이 과정이 다른 프로세스의 실제 소유권까지 강제로 없애는 것은 아니다. 현재 공식 CLI
0.166.0 격리 검사에서 살아 있는 app-server writer의 archive는 거부됐으며, 원래 writer가
종료된 뒤 같은 ID의 보관·복원·즉시 재개와 후속 지시 처리는 성공했다. 인계 실패 시
새 resume을 실행하지 않고 화면에 오류를 표시한다. 기존 GUI/CLI에서 해당 대화를
보관하거나 세션을 종료한 후 다시 열 수 있다. 잠금 삭제·강제 종료·새 ID fork는 하지 않는다.

## 검증

- 단위 검사: HTTPS 필수, Origin, 인증 실패, code 우회 차단, rate limit, 쿠키,
  세션 만료·로그아웃·암호 변경 후 회수와 로그인 파일 권한.
- 브라우저: 비로그인 조회/실행 차단 → 로그인 → 채팅·계정 선택 → 터미널 출력/입력 →
  새로고침 후 동일 CLI 연결 → 종료/재열기 → 로그아웃. 격리된 가짜 CLI fixture를 사용한다.
- 공식 CLI: 격리 HOME과 로컬 mock 모델로 archive/unarchive, 동일 ID·이전 문맥 보존,
  새 후속 지시 전달과 live-writer 인계 실패를 확인한다. 실제 사용자 채팅을 시험에 쓰지 않는다.

공식 참고: [Caddy TLS](https://caddyserver.com/docs/caddyfile/directives/tls),
[default_sni](https://caddyserver.com/docs/caddyfile/options#default_sni),
[Let's Encrypt IP 인증서](https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability/).
