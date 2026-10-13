---
name: codexm-manage
description: Open the installed codexm account and task management panel when the user invokes this command or requests its management screen.
---

Call the plugin's `codexm_open` MCP tool with `{}` once, return its UI result,
and end the turn. If the tool is unavailable, report that this environment does
not expose `codexm_open` and end the turn. Do not infer a connection failure.

This command opens the panel; it does not authorize registering a conversation,
starting a job, signing in, switching accounts, or changing settings. Do not
inspect files, enumerate chats, poll status, find a browser ID, or run a manager
after opening it unless the user separately requests that work.

If the host exposes the tool only through nested code-mode `exec`, its output may
not create a native UI card. Do not substitute printed JSON or a localhost web
page, or keep retrying the open call. Report that limitation and point to the
plugin's `codexm 관리` button. A tool acknowledgment alone is not proof of display.

GUI users select this skill with `@`; CLI/IDE users use `$codexm-manage`.
Skill invocation uses a model turn; direct panel buttons do not.
