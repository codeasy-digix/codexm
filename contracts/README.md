# Local bridge v1

`codexm --config PATH bridge --stdio` exchanges one UTF-8 JSON message per line.
Stdout contains only protocol messages. The caller owns this child, not workers.
`bridge-v1.schema.json` describes envelopes; `methods-v1.json` is generated from
Go's allowlist with `go run ./cmd/webcontract`. The same generator produces
`web/imports/contracts/generated.ts`. A Go test prevents the published list drifting.
Meteor checks the protocol and method surface before enabling commands.

Requests carry `protocolVersion:1`, a transport `id`, `method`, object `params`,
server-derived `owner`, and `operation_id` for mutations. Responses carry the
same transport ID and either `result` or `{code,message,retryable}` in `error`.
JSON keys for work arguments are declared by `supervisor.NewArgs`/`JobArgs` and
settings by `settings.Values`; unknown fields are rejected at the bridge boundary.
Native conversation, goal, quota and approval payloads retain their native shapes.

| Surface | Parameters / response |
| --- | --- |
| capabilities | Product/engine/supervisor versions, methods, host/store identity, generation, feature gates |
| snapshot | Consistent management instance/sequence/jobs/accounts/pending actions; independently observed config revision/time |
| sessions.list | search, project, account, state, managed, active, cursor → at most 50 summaries, next cursor, revision/time |
| jobs.details, conversation.read | id or thread_id, optional cursor → native thread/goal, recent 20 turns, cursor; details also includes job |
| jobs.create | cwd, kind, prompt, title, account, model, effort, read_only, completion_command, parent_id, goal, optional token_budget |
| jobs.send | id, text, steer |
| jobs.account | id, account, run_id, expected_account; operation ID is also the durable account-change action ID |
| jobs.reply | id, run_id, request_id, native result |
| jobs.goal | id, text, optional token_budget and goal_status; omitted values preserve existing accounting/intent |
| settings.read/save | Redacted values/account names/revision; save requires that revision |
| login.start/status/cancel | owner from authenticated session; account/mode, login_id, generation; empty status ID resumes this owner's last login |
| accounts.remove | account and exact config revision; preserve credentials/native login/history, reject manager or in-use accounts; durable online operation, offline lease and revision guard |
| changes.replay / watch | instance, after → ordered invalidations, sequence, more/resync; watch emits changes/resync notifications |
| operation.read | operation_id → done/failed/pending/outcome_unknown and saved result/error |

Management mutations use a durable operation record before dispatch. Same ID and
canonical arguments return the saved result; different arguments conflict. A lost
result leaves the record pending and exclusive supervisor startup recovers it as
`outcome_unknown`. Do not automatically resend or fork. An ID is an idempotency
key, not authorization. All authenticated local browser sessions control the same
manager; only login brokers are isolated per owner.

Initial settings use config locking/revision before a supervisor exists. Login
operations use per-owner generation-bound deduplication in memory; a restarted
bridge never resumes them automatically. Native-default selection is locked and
idempotent. Service startup uses existing service ownership. Terminal opening is
explicit and never retried. Their IDs are not durable manager operation records.

Journal writes are atomic with their management rows. Snapshot and sequence share
one SQLite read transaction. A changed instance, invalid cursor or retained-prefix
gap requests a snapshot. Retention prunes to 50,000 entries every 256 inserts.
Operation IDs and work history are retained. Config and catalog revisions are
independent of the management transaction. Conversation identity is
`[host, canonical official store, thread_id]`; a job ID is temporary until assigned.

Input lines are limited to 1 MiB, 16 concurrent Go calls and 64 queued replies.
Meteor has at most 32 pending calls and a 16 MiB reply buffer. Overflow disconnects
and resynchronizes; unresolved writes become unknown. Calls have 120-second Go /
125-second Node deadlines. Reads can be repeated after reconnect; writes use
`noRetry`. The web child has one exclusive bridge lease per state directory.

HTTP Host/Origin validation precedes DDP transport. A private one-use access code
becomes an HttpOnly SameSite=Strict session cookie. Authenticated HTTP creates a
30-second one-use in-memory DDP ticket; cookie presence alone never authorizes DDP.
SockJS strips Origin from onConnection headers, so Origin is checked at HTTP and
upgrade boundaries and Host is checked again onConnection. Publications/methods
check session expiry. Login codes are never put into publications or browser storage.
