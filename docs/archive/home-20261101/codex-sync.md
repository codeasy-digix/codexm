> Historical record imported from `.home` at `896d075` on 2026-11-01.
> Current ownership and commands: [deployment](../../deployment.md), [history relay](../../history-sync.md).

# Codex 기록 보관과 실행 호스트

기존 `codex sync`의 양방향 native 기록 갱신 진입점은 영구 차단했다. 일반 `codex`, `resume`,
`fork`, `exec`, `review`, 도움말 호출에는 기록 동기화나 아카이브 작업을
자동으로 붙이지 않는다. 공식 `codex`는 직접 실행되고 계정 관리와 작업 선택은
독립 `codexm`이 담당한다.

새 `codex-history`는 native 저장소 밖에 기록을 보관하는 명시적 명령이다.
다른 장비의 보관 자료를 현재 장비의 `~/.codex`에 설치하거나 기존 대화를 교체하는
기능은 제공하지 않는다. 로컬 Mac·`server`·`remote-mac`에 새 helper와 v2 설정을
배포하고 구형 진입점 차단을 확인했다. 세 장비의 snapshot 집계와 중앙 아카이브 조회를
확인했다. 일부 대화의 보관 검증은 보류 중이다. 현재 실행 소유 호스트 확인과 기존
native 복제본 정리는 아직 완료로 표시하지 않는다.

## 실행과 보관의 경계

한 native 대화 UUID에는 실행을 계속할 호스트를 하나만 정한다. 다른 호스트에서
그 대화를 보려면 Codex 앱의 원격 연결을 이용해 해당 호스트의 대화를 연다.
대화 내용, native 기록과 실행은 그 호스트에 남는다.

외부 아카이브는 `(source.device_id, source.native_thread_id)`를 키로 사용한다. 같은 UUID가 여러
장비에 있어도 장비별 기록을 별도로 보관하며, 장비 간 내용으로 상대 native 저장소를
덮어쓰지 않는다. 내용 해시와 불변 revision으로 보관 상태를 확인한다. 새 기록을
게시한다고 다른 장비를 자동 검사하거나 그 장비의 원본을 적용하는 구형
`participant` 작업을 실행하지 않는다.

`source.device_id`는 그 자료를 읽은 장비다. 과거 동기화가 이미 복제한 대화도 있으므로
이 값만으로 최초 생성 장비나 현재 실행 소유 호스트를 확정할 수 없다.
기존 같은-UUID 복제본은 원문과 실행 이력을 검토해 소유 호스트를 정한 뒤 정리한다.
이 검토, native 복제본 삭제와 복구 저널 재생은 새 아카이브 명령이 수행하지 않는다.

2026-10-03 설치 앱 조사에서 `app.asar` 내부 `.vite/build/main-BbeJ4AAR.js`의
`readThreadHostId`는 같은 UUID의 호스트 후보를 최근 활동·생성 시각 순서로 선택했다.
내용 충돌 수가 0이어도 읽기 호스트가 바뀌는 실행 정체성 충돌은 남는다.
이것은 해당 앱 버전의 조사 결과이며 공개 API 계약이 아니다. 원격 조회와 실행
호스트를 명시하는 것은 이 문제를 피하기 위한 운영 규칙이다.

## 명시적 명령

독립 보관 명령의 도움말을 확인한다.

```zsh
codex-history --help
```

| 명령 | 역할 |
| --- | --- |
| `codex-history configure --hub` | 이 장비의 외부 아카이브 설정 작성 |
| `codex-history configure --remote SSH_ALIAS` | 새 아카이브 프로토콜을 사용하는 SSH 호스트 지정 |
| `codex-history snapshot` | 현재 장비의 안정된 native 기록을 읽어 외부 아카이브에 보관 |
| `codex-history list` / `list --json` | 아카이브의 장비별 대화 목록 조회 |
| `codex-history show DEVICE_ID THREAD_ID` | 지정한 장비·대화의 최신 보관 revision 조회 |
| `codex-history show DEVICE_ID THREAD_ID --revision N` | 지정한 보관 revision 조회 |
| `codex-history status` | 새 아카이브 설정과 상태 확인 |
| `codex-history serve` | SSH 연결에서 사용하는 아카이브 전용 stdio 처리 |

운영 대상에 새 helper와 프로토콜을 준비한 뒤 설정한다. 예를 들어 `server`를
보관 호스트로 쓰면 서버에서 `configure --hub`, 현재 장비에서
`configure --remote server`를 실행한다. 서버 자신의 대화도 서버에서 명시적으로
`snapshot`을 실행해야 보관된다. 다른 장비를 자동으로 검사하지 않는다.

```zsh
# 보관 호스트에서
codex-history configure --hub

# 기록을 보관할 장비에서
codex-history configure --remote server
codex-history snapshot
codex-history list
codex-history show <보관-장비-ID> <대화-UUID>
```

위 명령은 계정 로그인, 기본 계정, GUI 실행 호스트를 바꾸지 않는다.
`codexm account NAME`은 현재 터미널의 native Codex 계정을 선택하며,
기존 대화는 `codexm` 목록에서 가져오거나 공식 `codex resume`으로 연다.
인증 파일을 아카이브에 보내지 않는다.

2026-10-12에 이전 `codex history` 셸 확장을 제거하면서 같은 보관 어댑터를
독립 `codex-history` 명령으로 연결했다. 별도 `codex-history-sync` relay도 유지한다.
아래 이전 검증 기록의 `codex history` 표기는 당시의 명령 이름이다.

`serve`는 연결별 stdio 명령이며 지속 실행 서비스나 자동 스케줄러를 설치하지 않는다.
별도의 지속 실행 작업이 필요한 경우에는 관리 가능한 이름의 tmux 세션으로 실행하고
작업 종료 뒤 세션을 정리한다.

## 새 설정과 구형 명령 차단

기본 저장 경로는 `~/.codex-sync`이며 새 설정 파일은 `archive-config.json`이다.

| 구분 | format / version |
| --- | --- |
| 설정 | `codex-history-config` / `2` |
| 아카이브 revision | `codex-history-revision` / `2` |
| SSH 프로토콜 | `codex-history-archive` / `2` |

이전 `config.json`의 숫자 `format: 1`은 새 설정으로 해석하지 않는다.
기존 서버의 `codex_sync.py hub`도 새 `serve`로 자동 변환하지 않는다.
SSH 접근이 구형 강제 명령에 고정되어 있거나 원격 프로토콜이 다르면 새 작업을
중단한다. 아카이브 설정과 프로토콜 전환은 따로 검증한다.

기존 진입점은 인자 없이 실행하거나 `sync`, `--full`, `hub`, `configure`,
`recover`, `status`, `conflicts`를 지정해도 모두 종료 코드 1로 중단한다.
native 저장소, 설정 파일과 pending 복구 저널을 열기 전에 거부하고 SSH를 시작하지
않는다. `CODEX_SYNC_DISABLED`를 지우거나 구형 `config.json`을 복원해도 이
진입점에서 양방향 동기화를 다시 켤 수 없다.

```zsh
# 모두 중단되며 codex history로의 전환 안내만 출력
codex sync
codex sync --full
python3 ~/.utils/codex_sync.py hub
python3 ~/.utils/codex_sync.py configure --hub
python3 ~/.utils/codex_sync.py recover
```

bundle 검증과 native `inventory`·`row`·`rollout_path`·`export`는 새 외부 아카이브가
사용하는 읽기 전용 어댑터로 남아 있다. 구형 동기화의 쓰기 구현과 SSH 프로토콜은
제거했다. 모듈을 직접 import해 `NativeStore._write`·`install`·`writer_lock`,
`Client.sync`·`recover`, `RemoteHub`, `participant`·`serve`를 호출해도 인자·경로·설정·
복구 저널을 조사하거나 파일·DB·잠금·SSH에 접근하기 전에 거부한다.
`NativeReader.store.install`도 같은 거부 경로를 사용한다. 구형 `connect`·`database`는
읽기 전용으로만 열 수 있고 명시적인 쓰기 요청은 거부한다. 환경 변수·인자·구형 설정으로
쓰기 기능을 다시 켜는 경로는 없다.

이 퇴역 조치는 구형 모듈이 native 행과 기록을 다시 가져오는 경로를 차단한다.
이미 여러 호스트에 복제된 UUID와 GUI의 호스트 선택 문제는 별도의 복구 대상이다.
과거 캐시 벤치마크와 구형 native import fixture의 성공은 현재 기능이나 안전한
운영 복구 절차를 뜻하지 않는다.

테스트나 별도 외부 저장 경로를 지정할 때 사용하는 환경 변수:

| 환경 변수 | 기본값 / 의미 |
| --- | --- |
| `CODEX_HISTORY_HOME` | `~/.codex-sync`; 새 설정과 장비 식별·외부 작업 상태 |
| `CODEX_HISTORY_ARCHIVE_HOME` | `$CODEX_HISTORY_HOME/archive`; 외부 아카이브 |
| `CODEX_HISTORY_NATIVE_HOME` | `~/.codex`; 읽을 native 저장소 |
| `CODEX_SYNC_HOME` | 새 루트 변수 미지정 시 경로 호환만 제공; v1 설정 재사용 안 함 |
| `CODEX_SHARED_HOME` | 새 native 변수 미지정 시 읽을 저장소 경로 호환 |

외부 설정·아카이브·작업 상태 경로는 native 저장소 안에 놓지 않는다.
`CODEX_HOME`이 계정 홈을 가리키더라도 계정 인증 파일을 수집하지 않는다.
일반 사용에서는 테스트용 native 경로 변수를 설정하지 않는다.

## 보관 범위와 한계

native DB는 읽기 전용으로 연다. 안정된 rollout과 관련 대화 메타데이터·인덱스를
묶어 외부 revision으로 보관한다. 기록을 읽는 동안 바뀌거나 실행 중인 대화,
완성되지 않은 JSONL과 지원하지 않는 저장 구조는 안정된 snapshot으로 취급하지
않는다. native 파일·행을 설치하거나 pending 저널을 재생하지 않는다.
SQLite의 읽기 잠금·SHM 관리가 발생할 수 있으므로 모든 보조 파일까지 바이트 단위로
불변이라고 주장하지 않는다. 대화·스키마·인덱스·첨부·세션을 쓰는 경로는 호출하지 않는다.

보관 대상 대화가 참조하는 native `attachments` 파일만 함께 수집한다.
내부 첨부가 없거나 읽는 동안 바뀌면 그 대화는 보관 성공으로 처리하지 않는다.
native 밖의 참조는 외부 참조로 남기고 미해결 수를 표시한다. 일반 프로젝트 파일을
첨부로 간주해 자동 수집하지 않는다.

개별 첨부는 64MiB, revision/object는 512MiB까지 지원한다.
state DB 5세대·thread history DB 1세대보다 새로운 저장 구조나 컬럼 불일치는
지원 여부를 확인하기 전 중단한다. 이것은 현재 어댑터의 지원 범위이며 새 Codex
버전에 대한 무조건적인 호환 보장이 아니다.

대화 보관은 전체 장비 백업과 다르다. 프로젝트 코드, 대화가 참조한 일반 로컬 파일,
설치된 플러그인, 프로젝트·사이드바 배치와 실행 중인 프로세스 상태를 재현하지 않는다.
계정 로그인·MCP 인증·native 설정은 보관 대상이 아니다. rollout 텍스트가 있다는
이유로 완전한 환경 복구를 주장하지 않는다.

보관 자료를 다른 호스트의 native 대화로 손실 없이 가져오는 공개 import API는
이번 구현에서 확인하거나 사용하지 않았다. 파일·DB 직접 설치 기능을 안전한
호스트 이동이나 공식 import 기능으로 설명하지 않는다.

## 실행 호스트 변경과 새 분기

기존 대화를 다른 호스트에서 계속할 때는 Codex 데스크톱 앱의 명시적 **Hand off**를
사용한다. 목적 호스트를 연결하고 같은 Git 저장소의 프로젝트를 저장해야 한다.
저장소 하위 디렉터리를 프로젝트로 쓰면 양쪽에서 같은 하위 디렉터리를 지정한다.
앱은 일치하는 프로젝트가 있는 목적지를 제시하고 대화와 Git 상태를 옮긴다.
진행 중 응답은 이동 전에 중단된다.
[공식 OpenAI 문서: 호스트 간 대화 이동](https://learn.chatgpt.com/docs/remote-connections#hand-off-a-chat-between-hosts)

다른 방향으로 작업할 때는 native `thread/fork`가 새 대화 ID를 만드는 공식 분기
기능을 사용한다. 같은 UUID를 두 호스트에서 독립적으로 계속 쓰는 방식은 소유 호스트
변경이나 분기로 간주하지 않는다.
[공식 OpenAI 문서: app-server API](https://learn.chatgpt.com/docs/app-server#api-overview)

`codex history`는 GUI handoff를 자동으로 실행하지 않는다. 프로젝트가 없는 대화나
앱이 지원하지 않는 이동 조건은 별도 확인이 필요하다. 보관 성공만으로 다른 호스트의
native 실행이 가능해졌다고 간주하지 않는다.

## 운영 전환 확인: 2026-10-03

로컬 Mac·`server`·`remote-mac`의 초기 배포 코드 SHA가 일치했다. 각 장비에 별도 배포 전
백업을 만들고 helper·셸 파일을 원자적으로 교체했으며, 서버는 hub, 두 Mac은 지정한
SSH 별칭을 사용하는 remote 역할의 v2 설정을 적용했다. 제한된 SSH 키는 강제 실행
명령만 새 `codex_history.py serve`로 바꾸고 키와 기존 옵션을 보존했다.
격리와 구형 진입점 차단 `ba1c61a`, 파서·첨부 보완 `ea06830`, 구형 컨트롤러의
셸 전달 보완 `1b60d16`을 commit·push했다.
snapshot helper는 `ea06830` 그대로다. `codex-accounts` 저장소와 공개 Homebrew
0.3.0 패키지는 변경하지 않았고, 패키지·tap 릴리스도 배포하지 않았다.
이번 운영 변경은 개인 helper·셸과 별도 아카이브 설정 배포다.
구형 `config.json`은 별도 비공개 백업 이름으로 보존했다.

두 SSH 별칭의 v2 hello에서 `native_import=false`를 확인했다. 초기 운영 smoke에서
세 장비의 `codex --version`·`codex --help` 정상 종료, sync 안내문 부재, history 상태와
구형 진입점의 거부를 확인했다. 당시 로컬은 native 버전 `0.160.0`과 도움말의
`Usage:`를 확인했지만, 서버·IBS의 구형 컨트롤러 출력은 native 전달 성공으로
계산하지 않는다. 초기 smoke에서 `resume --help`·`update`·`doctor`는 실행하지 않았다.

추가된 `run` fallback은 별도 셸 회귀 검사로 검증한 뒤 세 장비에 배포했다.
새로 불러온 셸의 `codex --version`·`codex --help`·`codex resume --help`·
`codex doctor --help`를 각 컨트롤러가 선택하는 기존 native의 직접 실행과 비교했다.
모든 명령에서 stdout·stderr·종료 코드가 일치했고 종료 코드는 0이었다.
로컬은 native `0.160.0`, 서버·IBS의 공개 0.3.0 컨트롤러는 기존 번들 native
`0.159.3`을 실행했다. 원격 PATH에 있는 native는 서버 `0.159.2`, IBS `0.160.0`이며
컨트롤러가 선택하는 번들과 구분한다. 이 검사는 native 패키지나 선택 환경을 바꾸지
않고 기존 실행 경로를 확인한 결과다. 실제 `update`나 대화 `resume`은 실행하지 않았다.

실제 허브의 `serve`에서 `NativeReader`·`NativeStore`·`Client`를 호출할 수 없도록
막은 검사에서도 native 접근은 0이었다. 이 결과는 아카이브 서버가 자체 native
대화를 읽거나 적용하는 구형 participant 동작을 실행하지 않는다는 검증이다.
최종 검사에서 세 장비의 구형 Python helper와 sync·hub·recover 프로세스는 0개였고,
이번 snapshot 작업의 tmux 세션도 모두 정리됐다. 실행 중인 native 작업·daemon은
이 정리 대상으로 삼지 않았다.

계정 홈의 공용 잠금 symlink 대상은 로컬 Mac 4/4, `remote-mac` 4/4에서 정상으로
확인했다. 서버의 이번 계정 홈 검사 표본은 0개이므로 서버 계정 전체에 대한 검증
결과를 추가하지 않는다.

서버의 첫 snapshot은 29개를 외부에 보관한 뒤 유효한 JSON 문자열의 U+2028을
줄 경계로 오인하는 파서 오류로 중단됐다. native 원문 손상은 없었다. 물리적인 LF만
JSONL 경계로 처리하고 공백이 포함된 첨부 경로를 인식하도록 수정한 뒤 재시도했다.
세 장비의 실행 결과는 다음과 같다.

| 장비 | 외부 snapshot 확인 결과 | native 대화 수: snapshot 전→후 |
| --- | --- | --- |
| `server` | 보관 확인 462: 새 보관 433, 기존 revision 동일 29; 검증 보류 18 | 480→480 |
| 로컬 Mac | 새 보관·업로드 461, 사용 중 5, 검증 보류 18 | 484→484 |
| `remote-mac` | 새 보관·업로드 459, 사용 중 2, 검증 보류 19 | 480→480 |

native 대화 수는 각 snapshot의 직전·직후를 비교한 값이다. 후속 로컬 CLI 검사에서는
490→490을 확인했다. snapshot과 후속 검사 사이에 신규 ID 6개가 관찰됐고 삭제는
0개였으므로, 전체 작업 기간 동안 모든 native ID가 불변이었다고 해석하지 않는다.
이 기간 차이를 실패로 집계한 최초 검사 결과도 보존하고 검사별 범위를 구분했다.
각 snapshot과 후속 CLI 검사에서 확인한 안정된 파일 표본의 내용은 유지됐다.

중앙 허브에는 장비별 보관 항목과 revision이 각각 1,382개이며, 고유 UUID는 463개다.
같은 UUID의 장비별 namespace가 분리됐고, 조회 표본 6개에서 source와 내용 해시를
확인했다. 첫 서버 시도에서 보관한 29개 revision도 유지됐다. 이 수치는 장비별
아카이브 집계이며, 기존 중복 UUID 480개의 실행 소유 호스트를 확정하는 수가 아니다.

사용 중이거나 아카이브 검증을 통과하지 못해 보류한 대화는 원본을 유지하며,
보관된 것으로 간주하지 않는다. 서버의 보류 18개 중 2개는 projection byte offset이
rollout 파일 크기를 벗어난 것을 메타데이터에서 확인했다. 나머지 16개는 원인이
확정되지 않았으며, 이 검사만으로 native 원문 손상이라고 판단하지 않는다.
세 장비 전체 자료의 보관 완료나 완전한 환경 복구를 주장하지 않는다.

## 전환 상태와 안전한 복구

구형 설정, hub의 불변 자료, recovery 사본과 기존 백업은 검토 자료로 보존한다.
우선 자동 호출과 구형 원격 진입을 차단한 상태를 유지하고, 장비별 같은 UUID의
내용·revision·현재 실행 상태를 확인해 native 소유 호스트를 정한다.
미검토 복제본의 삭제, 대화 재개, 복구 저널 자동 마무리는 수행하지 않는다.
보관 장비 ID를 정했다고 이 소유 호스트 검토가 완료되는 것은 아니다.

전환 검토 대상은 장비 간 중복 native UUID 480개이며, 장비별 파일 복제본의 총수가
아니다. 이 대화들의 기존 native 복제본은 이번 배포에서 수정·삭제·native archive
하지 않았다. 비공개 보고서의 원래 호스트 선호 후보 456개와 별도 검토 대상 24개는
현재 실행 소유 호스트의 확정 목록이 아니다. 관찰된 archive source와 현재 owner를
구분해 유지한다. 현재 실행 소유 호스트가 미확정이고 active writer가 있으므로,
복제본 정리는 유지보수 시점에 전체 writer 종료 후 다시 검증해야 한다.

native 0.159.2·0.160.0의 정확한 소스와 격리 fixture에서는 root 및 알려진 child의
writer 잠금을 파일 이동 전에 획득하고 유지하며, 어느 잠금이든 사용 중이면 native
archive를 거부하는 것을 확인했다.
[0.159.2 잠금 소스](https://github.com/openai/codex/blob/rust-v0.159.2/codex-rs/thread-store/src/local/archive_thread.rs#L15-L64),
[0.160.0 잠금 소스](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/thread-store/src/local/archive_thread.rs#L15-L64)

다만 subtree 조회·읽기와 같은 App Server에 로드된 대화의 shutdown은 파일 writer
잠금 획득보다 먼저 진행된다. archive가 거부됐다는 이유로 이미 로드된 실행 상태도
변경되지 않았다고 판단할 수 없다.
[사전 shutdown 처리](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/app-server/src/request_processors/thread_processor.rs#L1048-L1080),
[archive 처리 순서](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/app-server/src/request_processors/thread_processor.rs#L1716-L1788)

root 처리 뒤 child 일부가 실패해도 root RPC가 성공할 수 있다. 공식 회귀 검사도
root·grandchild는 archived, 실패한 child는 active로 남는 결과를 검증한다.
성공 응답 하나만으로 전체 subtree의 원자적 정리나 소유 호스트 전환 완료를
판정하지 않는다. 복제본 정리는 별도 maintenance 구간에서 전체 writer 종료를
확인한 뒤 예상 descendant 각각의 상태와 ownership을 다시 검증해야 한다.
[부분 성공 회귀 검사](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/app-server/tests/suite/v2/thread_archive.rs#L536-L680),
[공식 OpenAI archive API](https://learn.chatgpt.com/docs/app-server#archive-a-thread)

문제가 생기면 새 `codex history snapshot` 실행을 멈추고 외부 아카이브와 설정,
현재 native 원본을 각각 보존한다. 일반 CLI는 history 명령을 호출하지 않으므로
계속 사용할 수 있다. 구형 셸 함수·helper·v1 설정으로 되돌려 양방향 동기화를
다시 켜는 것을 복구 절차로 사용하지 않는다.

실제 native 자료 복원이 필요한 경우에는 모든 Codex 앱·CLI·daemon을 종료한 뒤
현재 자료를 추가 백업하고, 해당 장비의 검증된 백업 범위·DB·WAL·SHM과 버전
호환성을 검토하는 별도 작업으로 진행한다. 실행 중인 SQLite 위에 백업 DB만
덮어쓰지 않는다. 과거 recovery를 새 snapshot이 자동 재생하는 기능은 없다.

## 검증 범위

셸 테스트는 가짜 컨트롤러와 helper로 일반 CLI·원격 런타임·구형 컨트롤러·계정
fallback의 인자·stdout·stderr·종료 코드와 자동 보관 호출이 없음을 확인한다.
구형 컨트롤러 fixture는 도움말·버전·doctor의 자체 처리를 재현한 뒤 명시적 `run`이
이를 우회하는지 검사한다. 공백이 있는 인자, future command, 계정·continue 관리
경로와 계정 환경도 검증한다.
안전 게이트 테스트는 v1 설정, v2 설정 동시 존재, 미완성 복구 저널과 구형 hub stdin
요청을 넣어 구형 CLI가 파일·DB 변경이나 SSH 시작 전에 거부되는지 확인한다.
직접 import의 7개 재유입 경로도 정상 스키마·원본 bundle·pending 저널을 가진 임시
fixture로 검사한다. 기존 UUID 덮어쓰기와 새 UUID 설치를 모두 거부하며, 파일 바이트·
SQL 행·잠금·원격 호출이 바뀌지 않는지 확인한다. fixture는 테스트 안에서 SQLite를
직접 생성하고 채운다. 제거한 v1 동기화·native import 성공 검사는 읽기 어댑터와
퇴역 거부 검사로 대체했다.
새 아카이브 fixture는 같은 UUID의 장비별 분리, 원문·인덱스 보관, 불변 revision,
중단·응답 유실 후 재시도, 첨부 누락·외부 참조 표시, 읽기 전용 native 접근과
경로·symlink·구형 프로토콜 차단을 검사한다. per-thread 잠금을 유지하면서
coordination을 해제하고 revision을 하나씩 읽는 것도 확인한다.

```zsh
python3 -B -m unittest discover -s tests -p 'test_codex_shell.py' -v
python3 -B -m unittest discover -s tests -p 'test_codex_sync_safety.py' -v
python3 -B -m unittest discover -s tests -p 'test_codex_sync.py' -v
python3 -B -m unittest discover -s tests -p 'test_codex_history.py' -v
```

import 퇴역 이전인 2026-10-03 파서·첨부와 구형 컨트롤러 전달 보완 후 통합 검사 `test_codex_*.py`
98개 중 96개가 통과했고, 실제 CLI의
임시 native import·resume을 요구하는 선택 검사 2개는 실행하지 않았다.
새 아카이브 검사 28개가 통과했다. 실제 native 버전·`resume --help` 전달과
`history --help` 출력은 로컬 검사에서 확인했으며, 위의 세 장비 운영 검사와 구분한다.
`run` fallback 보완의 셸 검사 10개와 `zsh -n .codex_rc`도 통과했다.

같은 날 구형 import 경로를 퇴역시킨 뒤 통합 검사 `test_codex_*.py`는 74개 중
73개가 통과했다. 실제 CLI 저장소 확인을 요구하는 선택 검사 1개는 실행하지 않았다.
새 아카이브 검사 28개와 셸 검사 10개는 모두 통과했다. 구형 sync 알고리즘과
native import 성공 검사 대신 읽기 어댑터 5개와 CLI·import 퇴역 검사 7개를 실행했다.
`zsh -n .codex_rc`와 `git diff --check`도 통과했다.

fixture 검증과 운영 배포·프로토콜 검증, 실제 외부 snapshot 시도를 구분한다.
실제 사용자 대화의 수정·삭제·native archive, live 양방향 sync, GUI 재시작과
cross-host native import는 수행하지 않았다. 구형 알고리즘의 합성 fixture 검사는
과거 helper 호환성 검사로 남아 있으며 현재 사용자 명령이 아니다.
