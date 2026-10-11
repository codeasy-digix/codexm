# codexm

- codexm owns management, accounts, logs and recovery. Keep the official `codex`
  command unmodified; no shell interception, alternate codex binary, or daemon RPC
  prerequisite for native help, version, login, or execution.
- Retain existing credentials, thread IDs, goal budgets and settings. Auth/history,
  binaries, sockets and logs stay outside Git. Never print credentials or goal text.
- Normal supervision is deterministic and makes no model calls. `/manager` is an
  explicit separate request. Native chat runs in the official TUI.
- Persistent processes use named tmux sessions. Clean up every temporary test
  session and worker; do not stop unrelated sessions.
- Run `go test -race ./...`, `go vet ./...`, and relevant isolated native integration
  for runtime changes. Account/library failure must never break official Codex.
- Distribution is preparation only. Build local archives with `go run ./cmd/package`;
  do not create releases/tags, publish npm packages, or update a live tap without
  a later explicit release instruction. Commit and push reviewed source changes.
