---
name: codexm
description: Register the current local Codex conversation with codexm when the user invokes @codexm or asks to apply codexm to this chat. Also explain its built-in plugin panels and inspect registration status.
---

An explicit `@codexm` invocation asking to apply management to this chat authorizes
registration of this existing conversation. Run `~/.local/bin/codexm register --current --json`.
Use `--account NAME` only if the user names a worker account. The CLI resolves the
current `CODEX_THREAD_ID`; do not substitute the newest thread or create a chat.

Report the returned job ID, account and actual state briefly. `external_writer`
means registration/monitoring is active while the original Codex owns the writer;
account handoff waits for its release. Do not claim the original running agent's
account already changed. Registration preserves its thread, goal and budget.

For a request to open the management screen, use the available `codexm_open` tool.
The same GUI is available from the app sidebar, the conversation's plugin panel,
and the codexm plugin details' management action. Do not launch a separate app or
website. If the host has not loaded the local MCP connection, report that exact
condition; a filesystem link is not a plugin invocation.

For status, read `codexm status --json` through a script that prints only job IDs,
thread IDs, accounts, states and quota metadata; raw status includes private text.
For requested removal, use `codexm unregister JOB_ID` for the registered job only.
Do not switch global Codex login, delete writer locks, exit other chats, change
goals or send Discord messages just because this skill was invoked.
