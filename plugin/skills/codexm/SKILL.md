---
name: codexm
description: Handle codexm manage/accounts commands, explain management buttons, inspect registration status, or register the current local conversation when the user explicitly requests management.
---

Exact `@codexm manage` / `@codexm accounts` commands are intercepted before
inference by the plugin's synchronous `UserPromptSubmit` hook, which opens the
official MCP App deep link and blocks prompt submission. The dedicated commands
`@codexm-manage` / `@codexm-accounts` (CLI: `$codexm-manage` /
`$codexm-accounts`) use the same route. Hook execution requires the user's
review/trust in official Hooks settings (CLI: `/hooks`) in a local Codex
conversation. A changed hook requires review again. Never forge trust files or
enable a production trust bypass. No native slash command is registered.

If an exact command reaches this model, the no-model route did not run. Explain
that the hook must be enabled and trusted, then stop without opening it through
a model tool call. For a separate natural-language open request, call `codexm_open`
or `codexm_accounts_open` once as appropriate and end the turn.
After an open call do not inspect files, list chats, poll usage or investigate
browser IDs unless the user separately asks. An unavailable tool is a stopping
condition, not a reason to start another agent or website.
If only JSON or an acknowledgment is returned, do not claim the screen appeared.
Explain that result once, point to the official Hooks setup and end the
turn. If the tool is not exposed, report that exact condition without inferring
a connection failure.

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

The primary command entrypoints are `@codexm manage` and `@codexm accounts`.
Plugins -> codexm also declares `codexm 관리` and `계정 관리` buttons.
The sidebar and conversation panel also expose `codexm 관리`
when supported by the host. The same panel has `작업`, `계정`, `사용 기록`,
`설정` and `+ 새 작업`. `계정 관리` opens the account tab directly; its
`계정 추가` and `재로그인` buttons use official browser/device authentication,
including first setup while the manager service is stopped. Do not ask users
to type a natural-language chat request as the normal way to open or operate it.

If explicitly asked to open it through this chat, the available `codexm_open`
tool requests the panel. Calls nested inside code-mode `exec` are not native
MCP cards in some hosts. A successful tool result does not prove the UI was
displayed. Try now on the plugin details is not a managed job start action.
The command hook requires local plugin hook support. Cloud conversations and
hosts that have not enabled/trusted it cannot promise a model-free command.
Do not describe `/setting` or `/manage` as a newly registered native slash command.
An open request alone never authorizes chat
registration, account login or starting work. Do not launch a separate app or
website. If the host has not loaded the local MCP connection, report that exact
condition; a filesystem link is not a plugin invocation.

For status, read `codexm status --json` through a script that prints only job IDs,
thread IDs, accounts, states and quota metadata; raw status includes private text.
For requested removal, use `codexm unregister JOB_ID` for the registered job only.
Do not switch global Codex login, delete writer locks, exit other chats, change
goals or send Discord messages just because this skill was invoked.
