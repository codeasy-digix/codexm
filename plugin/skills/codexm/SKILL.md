---
name: codexm
description: Explain codexm's direct management and account buttons, inspect registration status, or register the current local Codex conversation when the user explicitly asks to apply account management.
---

An explicit `@codexm` invocation asking to apply management to this chat authorizes
registration of this existing conversation. Run `~/.local/bin/codexm register --current --json`.
Use `--account NAME` only if the user names a worker account. The CLI resolves the
current `CODEX_THREAD_ID`; do not substitute the newest thread or create a chat.

Report the returned job ID, account policy and actual state briefly. `external_writer`
means registration/monitoring is active while the original Codex owns the writer;
account handoff waits for its release. Do not claim the original running agent's
account already changed. The returned `account` is the planned codexm worker account;
`registration.origin_account`, when known, identifies the original execution account.
Registration preserves its thread, goal and budget.

The primary GUI entrypoints are buttons: Plugins -> codexm -> `codexm 관리`
and `계정 관리`. The sidebar and conversation panel also expose `codexm 관리`
when supported by the host. The same panel has `작업`, `계정`, `사용 기록`,
`설정` and `+ 새 작업`. `계정 관리` opens the account tab directly; its
`계정 추가` and `재로그인` buttons use official browser/device authentication,
including first setup while the manager service is stopped. Do not ask users
to type a natural-language chat request as the normal way to open or operate it.

If explicitly asked to open it through this chat, the available `codexm_open`
tool requests the panel. Calls nested inside code-mode `exec` are not native
MCP cards in some hosts. A successful tool result does not prove the UI was
displayed. Try now on the plugin details is not a managed job start action.
`@codexm manage` is not an implemented native slash/zero-token command; do not
present it or `/setting` as one. An open request alone never authorizes chat
registration, account login or starting work. Do not launch a separate app or
website. If the host has not loaded the local MCP connection, report that exact
condition; a filesystem link is not a plugin invocation.

For status, read `codexm status --json` through a script that prints only job IDs,
thread IDs, accounts, states and quota metadata; raw status includes private text.
For requested removal, use `codexm unregister JOB_ID` for the registered job only.
Do not switch global Codex login, delete writer locks, exit other chats, change
goals or send Discord messages just because this skill was invoked.
