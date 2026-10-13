---
name: codexm-accounts
description: Open codexm's account management tab when the user invokes this command or requests its account screen.
---

The exact commands `@codexm-accounts`, `$codexm-accounts` and `@codexm accounts`
are handled before inference by codexm's synchronous `UserPromptSubmit` hook.
The desktop picker's namespaced `$codexm:codexm-accounts` form and its
Markdown skill link use the same route.
The hook opens the official MCP App deep link at `/accounts` and blocks prompt
submission. First installation or a changed hook requires the user's review/trust
in the official Hooks settings (CLI: `/hooks`). Do not edit hook trust records or
bypass trust for the user. Native slash command registration is not provided.

If an exact command reaches this model, the no-model route did not run. Explain
that a local Codex conversation needs the enabled, trusted codexm hook, then stop.
If those conditions were already verified, report a command-routing failure; do
not ask the user to approve the same hook again. Do not disguise a model tool
call as a direct command. Additional instructions after the skill are ordinary
user requests; follow them rather than treating them as an exact open command.

For a separate natural-language request to open the account panel through this
chat, call `codexm_accounts_open` with `{}` once, return its UI result, and end the
turn. If the tool is unavailable, report that exact condition without inferring
a connection failure.

The tool selects the account tab; it does not authorize signing in, switching
accounts, registering chats or changing account order/settings. The user uses
the panel's buttons for those actions. Do not inspect files, enumerate chats,
poll usage or find a browser ID after opening it unless separately requested.

If the host exposes the tool only through nested code-mode `exec`, its output may
not create a native UI card. Do not substitute printed JSON or a localhost web
page, or retry in a loop. Report that limitation and point to the official Hooks
setup for direct commands. A tool acknowledgment alone is not proof of display.
