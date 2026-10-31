# 선택적 대화 relay

history-sync 소스와 테스트는 `codexm` 제품에 통합되어 있다. 계정 전환·native resume·
일반 관리 서비스와 독립된 명시적 기능이다. 설치, 셸 시작, 계정 변경, `codex` 실행에
동기화를 붙이지 않는다. 기본 설정을 만들거나 자동 서비스를 켜지도 않는다.

```sh
codexm history-sync --help
codexm history-sync status
codexm history-sync conflicts
codexm history-sync plan
codexm history-sync sync --dry-run
# 실제 반영은 명시적 enabled:true 설정과 별도 운영 판단 후:
codexm history-sync sync
```

기본 출력은 건수·요약 오류다. `--json`/`--verbose`를 명시해야 상세 결과를 출력한다.
`status`와 `conflicts`는 native DB를 열지 않는다. `plan`과 `--dry-run`은 native 기록을
조사하지만 적용하지 않는다. SSH hub 조회는 발생할 수 있다. 실행 중인 프로세스,
writer lock, 불완전한 파일·첨부·새 DB 스키마는 기존 엔진의 보류/거부 규칙을 따른다.

설정 탐색 순서는 명시적 `--config PATH`, `$XDG_CONFIG_HOME/codexm/history-sync.json`,
기존 `$XDG_CONFIG_HOME/codex-history-sync/config.json`, `~/.codex-history-sync/v2/config.json`이다.
잘못된 새 설정을 조용히 무시하고 옛 설정으로 되돌아가지 않는다. `codexm`의 전역
`--config`는 관리자 TOML용이므로 relay에서는 하위 명령 뒤에 쓴다.

설정 예시는 [history-sync.example.json](../config/history-sync.example.json)이다.
`enabled`는 명시적으로 켜기 전까지 false로 둔다. `home`은 인증용 named home이 아닌
공식 공용 기록 저장소다. `store`·`hub_store`는 native 저장소 밖에 둔다.
계정 토큰·GUI 연결 설정은 전송하지 않는다. 기존 ID, 최신 이력 비교·충돌 원장,
active-writer 보호와 pending 형식은 이번 소스 이동에서 변경하지 않았다.

## 서비스

새 독립 설치는 사용자 launchd 또는 systemd를 쓴다.

```sh
codexm history-sync service install   # 정의만 설치; 시작하지 않음
codexm history-sync service status
codexm history-sync service start     # 명시적 시작 + 자동 실행
codexm history-sync service stop      # 중지 + 자동 실행 해제
codexm history-sync service restart
codexm history-sync service logs
codexm history-sync service uninstall
```

`daemon`은 전면 실행 명령으로 OS 서비스가 호출한다. Linux 사용자 서비스는 부팅 직후
로그인 전 실행이 필요하면 OS의 linger 설정이 별도로 필요하다. macOS 사용자 서비스는
사용자 세션을 따른다. 시스템 서비스가 필요한 기존 장비는 아래 기존 선언을 유지한다.

기존 `kr.digix.codex-history-sync` 시스템 launchd 서비스나 Linux
`codex-history-sync.service`가 있으면 새 사용자 서비스를 동시에 설치·시작하지 못한다.
`service status`는 기존 정의가 있다는 사실과 새 서비스 상태를 구분한다. 기존 장비는
다음 `.home` 관리 명령을 계속 사용한다. 이 호환 OS 선언은 코드 소유권과 별개다.

```sh
homectl service status codex-history-sync
homectl service stop codex-history-sync
homectl service disable codex-history-sync
# 다시 운영할 때 enable → start. restart는 실행 중 서비스의 재시작.
```

장비를 지정할 때 `--host server`, `--host remote-mac`를 쓴다. 자동 원격 적용은 없다.
사용자 서비스로 옮기려면 기존 시스템 서비스를 중지·비활성화하고 정의를 백업·제거한 뒤
새 서비스를 설치한다. 공유 store의 lock과 pending을 삭제해서 전환하지 않는다.

SSH peer의 `hub_binary`는 여전히 단일 실행 파일 경로다. 기존 peer에는
`codex-history-sync` 호환 링크를 사용할 수 있다. `"codexm history-sync"`를 경로에 넣지 않는다.
이전 Python 아카이브 프로토콜은 이 relay와 호환되지 않는다.

## 현재 운영 한계 (2026-11-01)

세 장비의 스케줄러는 실행 중이지만, 최신 native schema의
`threads.attached_thread_parent_id`를 지원하지 않아 inventory 단계에서 실패한다.
로컬 최근 대화 표본 5건이 두 원격 DB에 없는 것도 확인했다. **실행 중은 동기화 완료가 아니다.**
이번 통합은 지원 열 목록을 늘리거나 pending을 강제 적용하지 않는다. 이 오류의 수정과
대기 대화의 실제 재전송·적용은 별도 검증 작업이다. GUI와 CLI가 작업 중인 장비에
무리하게 기록을 설치하거나 GUI 호스트 선택 문제의 해결을 보장하지 않는다.

이동 원본: `codeasy-digix/codex-accounts@b8a7b1284de68f8507247544369190b3f8196cec`
의 `internal/historysync`와 CLI 테스트. 과거 검증·복구 문서는
[기록 보관](archive/home-20261101/codex-sync.md)에 역사 자료로 분리했다.
