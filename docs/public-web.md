# HTTPS 웹 접속과 로그인

선택적인 채팅 웹 구성 안내다. 기본 제품은 [Rust TUI](terminal-ui.md)이고 웹/HTTPS
서비스를 요구하지 않는다. 이 문서는 기존 웹을 명시적으로 운영할 때만 적용한다.

웹은 Meteor 3.5.2와 React로 구성한다. HTTP 세션 쿠키와 일회용 DDP ticket으로
인증한다. 서버 자체의 사용자 한 명을 설정하는 방식으로, Codex 계정 인증이나
다른 장비의 설정을 변경하지 않는다. 회원가입·암호 복구 메일 기능은 제공하지 않는다.

## 구성

도메인을 사용할 때는 DNS A 레코드를 현재 공인 IPv4로 지정하고, 공유기 뒤 장비라면
TCP 80/443만 해당 장비의 같은 포트로 전달한다. 내부 IP는 DHCP 예약으로 유지한다.
유효한 IPv6 경로가 없으면 AAAA 레코드를 추가하지 않는다. Meteor의 4317 포트는
외부에 전달하지 않는다. 기존 DMZ·SSH·다른 서비스의 포워딩은 보존한다.

도메인에는 [도메인 Caddy 설정](../deploy/Caddyfile.domain.example)을 사용한다.
기존 [IP 주소용 설정](../deploy/Caddyfile.example)은 shortlived ACME 프로필을 명시하는
별도 예시다. DNS와 외부 80/443 경로가 확인된 뒤 인증서 발급을 시작한다.

1. `codexm web user --username YOUR_USERNAME`으로 비밀번호를 입력한다. 자동화 시에는
   `--password-stdin`을 사용하며 비밀번호를 명령행 인수·스크립트·Git에 넣지 않는다.
2. `codexm web --public-origin https://YOUR_HOST --no-open`으로 Meteor를 실행한다.
   `YOUR_HOST`는 공개 도메인 또는 공인 IP다. Meteor는 `127.0.0.1:4317`에만 바인딩한다.
   이 명령만으로 HTTPS 프록시까지 시작하지는 않는다.
3. 기존 Caddy 2.11 이상으로 [설정 예시](../deploy/Caddyfile.example)를 적용한다.
   `CODEXM_PUBLIC_HOST`, `CODEXM_TLS_STORAGE`, `CODEXM_CERT_EMAIL` 값을 지정하거나,
   로컬 `~/.config/codexm/Caddyfile`에 실제 값으로 치환한다. 영구 프록시는
   launchd/systemd 등 OS 서비스 관리자로 실행한다. IP 인증서는 shortlived ACME 프로필을 사용하고 Caddy가 자동 갱신한다.
   `default_sni`는 SNI를 보내지 않는 IP 접속에서도 올바른 인증서를 선택한다.
4. 외부에서 80/443이 해당 장비로 연결되도록 한다. 80은 HTTPS 리다이렉트와 ACME 검증,
   443은 로그인·웹·WebSocket에 사용한다. 4317은 외부에 열지 않는다.

인증서를 발급할 수 없는 구성에서 HTTP 로그인으로 자동 전환하지 않는다.
공인 IP가 바뀌면 공개 Origin과 Caddy 호스트를 함께 변경하고 인증서를 다시 발급한다.

## 추후 서버 이전

현재 실행·검증 범위는 로컬 Mac이다. 서버 이전은 별도로 진행하며, 이 문서의 설정은
Mac 공인 IP나 홈 경로를 소스에 고정하지 않는다. 서버에서도 `HTTPS 프록시 →
loopback Meteor → 같은 장비의 공식 CLI` 구조를 사용한다.

- 서버 OS/아키텍처에 맞춰 제품 저장소에서 CLI와 웹 번들을 다시 빌드·설치한다.
  Mac의 설치 디렉터리나 Meteor 번들을 Linux로 그대로 복사하지 않는다. 설치기는
  번들의 OS/아키텍처를 확인한다. 공식 Codex CLI, Node 24, tmux와 HTTPS 프록시가
  실행에 필요하고, 소스 빌드에는 Go·Rust와 Meteor 도구가 추가로 필요하다.
- `runtime.web_node_binary`, 프로젝트·대화·계정·state 경로는 서버의 실제 경로로
  설정한다. 현재 Mac의 config 파일을 그대로 사용하지 않는다. 빌드와 설치는 계속
  codexm 저장소가 담당하며 `.home`에 제품 빌드를 추가하지 않는다.
- 서버의 공개 도메인 또는 IP를 `--public-origin`과 프록시에 동일하게 지정한다.
  80/443에 기존 프록시가 있으면 그 구성에 통합하고, 별도 Caddy를 중복 바인딩하지
  않는다. HTTPS와 WebSocket, Origin 및 클라이언트 IP 헤더 전달을 함께 검증한다.
- 웹 로그인은 서버에서 설정하고 브라우저 세션은 새로 발급한다. 인증서는 서버 주소에
  맞춰 발급·자동 갱신한다. 웹 배포 자체가 대화나 프로젝트를 자동으로 이전하지 않으며,
  폐기한 history-sync를 복구하지 않는다.
- Meteor는 제품의 OS 사용자 서비스로 실행한다. 사용자 로그인 전 실행이 필요하면
  실제 서버의 systemd linger 등 OS 정책도 설정한다. HTTPS 프록시는 별도 OS 서비스다.

서버 전환 완료 여부는 외부 브라우저의 로그인·채팅 조회·격리 작업 입력, HTTPS와
WebSocket 연결, 서비스 재시작 후 복구를 실제 서버에서 확인한 뒤 판단한다.

## 실행과 중지

`codexm web serve --public-origin https://YOUR_HOST --web-only`는 설치한 웹 번들을
포그라운드에서 실행합니다. macOS 시스템 LaunchDaemon처럼 외부 서비스 관리자가
부팅 시 이 명령을 실행할 수 있습니다. `install.sh --web`가 만든
`$XDG_DATA_HOME/codexm/bin/codexm-web`을 실행 경로로 사용합니다. 기본 데이터 경로는
`~/.local/share`이고, 후속 설치에 웹 번들이 없으면 이 링크는 이전 웹 배포를 가리킵니다.
외부 관리 구성의 시작·중지·재시작은 서비스 관리 명령으로 수행합니다.
`codexm web status`와 `codexm web logs`로 웹 프로세스를 확인할 수 있습니다.

`--web-only`에서 계정을 선택하면 다음 입력에 적용됩니다. 같은 계정으로 보내면
실행 중인 턴에 개입하거나 유휴 채팅에 새 턴을 시작합니다. 다른 계정으로 보내거나 큐에
추가하면 해당 턴을 중단하고, 실제 writer 잠금이 풀린 뒤 같은 ID를 선택 계정에서
엽니다. 한도로 이미 실패한 턴은 대상의 실행 상태를 확인한 뒤 바로 해제를 진행합니다.
웹 실행에서는 공식 구독 해제를 우선 사용합니다. 다른 실행은 공식 Unix 소켓으로
writer를 검증한 경우에만 선택한 대화를 보관·복원해 인계합니다. CodexM이 직접 띄운
CLI는 실행 영수증과 하나의 채팅 writer가 일치할 때만 해당 PID를 정상 종료합니다.
공식 연결을 확인할 수 없는 GUI나 직접 실행한 CLI는 원래 창에서 해당 채팅을
해제해야 합니다.

계정을 바꿔 **큐에 추가**하면 새 입력을 선택한 계정의 대기열에 저장합니다.
인계 때문에 진행 중인 턴을 중단했다면 같은 채팅에서 그 작업을 먼저 이어갑니다.
이어가는 턴이 완료된 뒤 대기열이 진행됩니다. 실행 중인 목표는 Codex가 같은 목표와
예산으로 이어가며, 목표가 완료될 때까지 대기열이 기다립니다. 실행이 중단되거나
완료를 확인할 수 없으면 새 입력은 대기열에 남습니다.

명시적으로 계정을 바꾸면 이전 계정으로 등록한 웹 대기 입력은 보류 상태로 남습니다.
큐에서 꺼내 다시 보내거나 원래 계정으로 전환해 이어갈 수 있습니다. 같은 계정의
대기 순서와 등록된 로그인은 유지합니다. 대기 내용은 재시작 뒤에도 남고 실행 결과가
불명확한 입력은 확인을 기다립니다.

채팅 보관은 그 채팅의 대기 입력도 일시적으로 보류합니다. 복원 후 지정 계정으로
진행합니다. 웹 소유 작업이 실행 중이면 서비스 재시작 전에 작업 상태를 확인하세요.

```sh
codexm web --public-origin https://YOUR_HOST --no-open
codexm web status
codexm web logs
codexm web stop
```

`web`은 소유한 launchd/systemd 사용자 서비스를 시작하고 재사용한다. 설정 변경이나
설치 후에는 웹 소유 작업을 먼저 정리하고 `codexm web stop` 후 새 버전에서 다시 시작한다.
웹 중지는 웹의 app-server 작업도 중단하지만 별도 GUI/CLI 작업은 보존한다.
등록 제거는 `codexm web uninstall`이다. HTTPS 프록시 재시작·중지는 해당 OS 서비스
관리 명령으로 수행한다. 기존 프록시를 중복 실행하거나 임의 종료하지 않는다.

웹 로그는 `codexm web logs`, 서비스 실행 오류는 `<state_dir>/services/web.log`에서 확인한다.
TLS 로그는 구성한 프록시 서비스에서 확인한다.
`web status`는 Meteor 프로세스와 로컬 리스너를 확인한다. 외부 경로와 TLS까지 정상인지
판단하려면 다른 네트워크에서 공개 URL에 접속해 로그인과 WebSocket 연결을 확인한다.

macOS 프록시 등록은 [launchd 템플릿](../deploy/codexm-https.launchd.plist.example)을
사용한다. `{{...}}`를 해당 장비의 절대 경로·호스트·인증서 연락 이메일로 치환하고 XML
특수문자를 escape한다. 실제 파일은 `~/Library/LaunchAgents/org.codeasy.codexm.https.plist`,
Caddy 설정은 `~/.config/codexm/Caddyfile`, TLS와 로그는 private state 경로에 둔다.
플레이스홀더·비밀번호·다른 장비 경로를 그대로 등록하지 않는다.

등록 전 `plutil -lint`와, 템플릿의 `CODEXM_*` 환경변수를 지정한 `caddy validate`를
통과해야 한다. 웹의 HTTPS Origin 적용에는 웹 서비스 재시작이 필요하므로 먼저 웹
소유 작업이 없는지 확인한다. GUI나 다른 CLI는 종료하지 않는다.

```sh
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/org.codeasy.codexm.https.plist
launchctl print gui/$(id -u)/org.codeasy.codexm.https
launchctl bootout gui/$(id -u)/org.codeasy.codexm.https
```

프록시만 중지해도 웹 app-server는 유지된다. 재개는 `bootstrap`, 실행 중 프록시의
설정 재적용은 검증 후 `launchctl kickstart -k gui/$(id -u)/org.codeasy.codexm.https`를
사용한다. 이때 브라우저 연결은 끊길 수 있지만 입력을 자동 재전송하지 않는다.

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

웹의 선택 계정별 공식 stdio app-server가 같은 채팅 ID에 턴을 시작하거나 개입한다.
프로젝트·보관·삭제와 큐도 공식 API로 처리한다. 브라우저는 응답과 접힌 도구 과정을
구조화해 표시한다. 웹 전용 실행에서는 위의 채팅 단위 인계를 적용합니다. 호환 모드에서
외부 writer에게 큐를 전달하면 원래 창의 계정이 처리합니다.
상세 입력·페이지·계정 경계는 [로컬 웹 안내](local-web.md)를 따른다.

## 검증

- 단위 검사: HTTPS 필수, Origin, 인증 실패, code 우회 차단, rate limit, 쿠키,
  세션 만료·로그아웃·암호 변경 후 회수와 로그인 파일 권한.
- 브라우저: 로그인 → 최근 이력/추가 페이지 → 개입/큐 → 재접속 → 프로젝트/보관/삭제 →
  로그아웃 차단을 실제 공식 runtime과 로컬 합성 모델로 확인한다. 두 모바일 크기도 검사한다.
- 공식 CLI: 격리 HOME에서 긴 도구 출력 지연 읽기, 외부 writer 보존과 원래 계정의 큐
  처리도 확인한다. 실제 사용자 채팅을 시험에 쓰지 않는다. 최신 결과는
  [웹 검증 기록](web-chat-core-progress.md)에 있다.

공식 참고: [Caddy TLS](https://caddyserver.com/docs/caddyfile/directives/tls),
[default_sni](https://caddyserver.com/docs/caddyfile/options#default_sni),
[Let's Encrypt IP 인증서](https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability/).
