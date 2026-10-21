# codexm

- codexm owns management, accounts, logs and recovery. Keep the official `codex`
  command unmodified; no shell interception, alternate codex binary, or daemon RPC
  prerequisite for native help, version, login, or execution.
- Retain existing credentials, thread IDs, goal budgets and settings. Auth/history,
  binaries, sockets and logs stay outside Git. Never print credentials or goal text.
- Normal supervision is deterministic and makes no model calls. `/manager` is an
  explicit separate request. The official engine powers both native TUI and plugin UI.
- Persistent product services use user launchd/systemd units. Temporary long-running
  test processes use named tmux sessions. Preserve existing active legacy workers;
  clean up only owned temporary services, sessions and workers.
- CLI, service, local ChatGPT plugin panels and the Meteor local web in `web/`
  are one product. Share management policy in Go through `codexm bridge --stdio`.
  The web binds loopback, authenticates HTTP/DDP, and owns no conversation database.
  Use React/TypeScript, native HTML and small plain CSS files. Do not patch the
  ChatGPT application bundle. Preserve work when the web or bridge exits.
- Run `go test -race ./...`, `go vet ./...`, and relevant isolated native integration
  for runtime changes. Account/library failure must never break official Codex.
- Distribution is preparation only. Build local archives with `go run ./cmd/package`;
  do not create releases/tags, publish npm packages, or update a live tap without
  a later explicit release instruction. Commit and push reviewed source changes.
