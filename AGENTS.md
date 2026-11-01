# codexm

- The active product is a local conversation catalog, account selector and official
  Codex CLI launcher. Keep `codex` native: never wrap/replace its binary or update
  command. `codexm account` may change the current shell or the explicit native default.
- Public entrypoints are in internal/cli/compact.go and internal/nativecli. Do not
  start the old supervisor, workers, manager model, scheduler, account rotation,
  watchdog, notifications or management plugin from the compact path. Prior
  implementation and tests remain migration references, not active commands.
  Legacy CLI files require `-tags legacy`; they are excluded from normal builds.
- Native conversations are read-only catalog data. The only handoff mutations are
  the official archive/unarchive commands for the explicitly selected thread.
  Fail before resume if they fail. Never delete native writer locks, edit native
  SQLite/rollouts, kill an external GUI/CLI, silently fork, or assume unarchive
  guarantees takeover of a live writer. Preserve IDs, credentials, goals and budgets.
- Account selection changes authentication only. Preserve shared model/effort
  preferences and each account's GUI state and model cache. Never synchronize
  account-server preferences or copy one account's state onto another.
- The foreground launcher replaces itself with the installed official CLI. The
  web launches that CLI in named codexm-cli-* tmux terminals, reads their output,
  and forwards literal input. It does not interpret chat turns or run model logic.
  Closing a web viewer must not stop the CLI. Never replay uncertain input.
- The Meteor listener stays authenticated and loopback-only. Explicit public access
  requires a configured password and an HTTPS reverse proxy with an exact origin;
  never expose the native listener or retain plaintext passwords. Its bridge methods
  are an explicit allowlist. Render user/native output as text. No shell evaluation
  of thread IDs, account names, input, directory names or terminal keys.
- Keep running work and old metadata. During migration retire only verified idle
  legacy product services; do not stop official Codex, GUI or unrelated tmux jobs.
  Test processes use named tmux sessions and are cleaned up after verification.
- Run Go race tests/vet, relevant isolated official-CLI tests, web typecheck/tests
  and browser interaction checks. Use synthetic accounts/chats for handoff tests.
  Report unsupported ownership cases instead of hiding failures.
- This repository alone owns source, builds, installation, updates and product docs.
  `.home` only registers an installed command and loads personal shell settings.
  Never restore product cloning/pins/build/install/update helpers there.
- Cross-machine history-sync is retired. Do not restore engines, service entries,
  compatibility links or automatic history writes. Preserve archival data.
- Installation and verification are local-Mac-only. Homebrew, archives/formula
  preparation, releases/tags/npm and other-host codexm deployment are on hold.
  Commit and push reviewed source changes; preserve concurrent edits and secrets.
