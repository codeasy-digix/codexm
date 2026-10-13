---
name: codexm-accounts
description: Open codexm's account management tab when the user invokes this command or requests its account screen.
---

Call the plugin's `codexm_accounts_open` MCP tool with `{}` once, return its UI
result, and end the turn. If the tool is unavailable, report that this environment
does not expose `codexm_accounts_open` and end the turn. Do not infer a connection failure.

The tool selects the account tab; it does not authorize signing in, switching
accounts, registering chats or changing account order/settings. The user uses
the panel's buttons for those actions. Do not inspect files, enumerate chats,
poll usage or find a browser ID after opening it unless separately requested.

If the host exposes the tool only through nested code-mode `exec`, its output may
not create a native UI card. Do not substitute printed JSON or a localhost web
page, or retry in a loop. Report that limitation and point to the plugin's
`계정 관리` button. A tool acknowledgment alone is not proof of display.

GUI users select this skill with `@`; CLI/IDE users use `$codexm-accounts`.
Skill invocation uses a model turn; direct panel buttons do not.
