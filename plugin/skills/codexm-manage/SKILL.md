---
name: codexm-manage
description: Open the installed codexm account and task management panel when the user invokes this command or requests its management screen.
---

The exact commands `@codexm-manage`, `$codexm-manage` and `@codexm manage`
are handled before inference by codexm's synchronous `UserPromptSubmit` hook.
The desktop picker's namespaced `$codexm:codexm-manage` form and its
Markdown skill link use the same route.
The hook opens the official MCP App deep link and blocks prompt submission.
First installation or a changed hook requires the user's review/trust in the
official Hooks settings (CLI: `/hooks`). Do not edit hook trust records or bypass
trust for the user. Native slash command registration is not provided.

If an exact command reaches this model, the no-model route did not run. Explain
that a local Codex conversation needs the enabled, trusted codexm hook, then stop.
If those conditions were already verified, report a command-routing failure; do
not ask the user to approve the same hook again. Do not disguise a model tool
call as a direct command. Additional instructions after the skill are ordinary
user requests; follow them rather than treating them as an exact open command.

For a separate natural-language request to open the panel through this chat,
call `codexm_open` with `{}` once, return its UI result, and end the turn. If the
tool is unavailable, report that exact condition without inferring a connection failure.

This command opens the panel; it does not authorize registering a conversation,
starting a job, signing in, switching accounts, or changing settings. Do not
inspect files, enumerate chats, poll status, find a browser ID, or run a manager
after opening it unless the user separately requests that work.

If the host exposes the tool only through nested code-mode `exec`, its output may
not create a native UI card. Do not substitute printed JSON or a localhost web
page, or keep retrying the open call. Report that limitation and point to the
official Hooks setup for direct commands. A tool acknowledgment alone is not proof of display.
