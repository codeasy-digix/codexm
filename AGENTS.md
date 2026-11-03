# codexm

- The active product is a local conversation catalog, account selector and official
  Codex CLI launcher. Keep `codex` native: never wrap/replace its binary or update
  command. `codexm account` may change the current shell or the explicit native default.
- Running `codexm` opens the Rust TUI in crates/terminal. Enter directly resumes
  with the last registered account or default; Right/Left move to/from details,
  and F2 opens settings/account management. Keep explicit choices across polling.
- Public entrypoints are in internal/cli/compact.go and internal/nativecli. Do not
  start the old supervisor, workers, manager model, scheduler, account rotation,
  watchdog, notifications or management plugin from the compact path. Prior
  implementation and tests remain migration references, not active commands.
  Legacy CLI files require `-tags legacy`; they are excluded from normal builds.
- Explicit `handoff THREAD --account NAME` is a bounded exception: verify a
  current codexm launch receipt, PID incarnation, executable and exactly one
  native writer before SIGTERM. Wait for both process exit and writer release
  before starting the same ID with one continuation prompt. Never SIGKILL or
  replay uncertain prompts. The temporary helper uses `tmux -L codex` and exits.
  `--if-low 5` uses fresh 5h/week limits; unknown limits are not permission to
  switch, aliases of the same login are not another account. GUI requests still
  require a real foreground terminal and confirmation each time. See
  docs/account-handoff.md. No always-on rotation service is implied.
- Native conversations are read-only catalog data. The only handoff mutations are
  official archive/unarchive commands or owner-side RPC for the explicitly selected
  thread. Skip archive/unarchive for unlocked, unarchived chats. A reachable shared
  daemon must list the selected thread as loaded before sending archive; confirm
  writer release before unarchive/resume. Never start/restart the daemon or change
  its authentication for a handoff. Fail before resume if mutations fail, and never
  replay uncertain archive requests. Never delete native writer locks, edit native
  SQLite/rollouts, kill an external CLI, silently fork, or assume unarchive
  guarantees takeover of a live writer. Preserve IDs, credentials, goals and budgets.
- A verified macOS GUI writer may be stopped with SIGTERM only after an explicit
  foreground terminal confirmation for that execution. Show that all GUI jobs
  stop and do not auto-continue. Never persist consent, accept piped confirmation,
  SIGKILL the GUI, or retry uncertain archive mutations via process termination.
  Arm the bounded GUI-reopen helper before stopping it, reopen after the selected
  CLI owns its writer or on failure, and preserve unrelated CLI/daemon processes.
- Account selection changes authentication only. Preserve shared model/effort
  preferences and each account's GUI state and model cache. Never synchronize
  account-server preferences or copy one account's state onto another.
- Preselect a chat's last known registered account, or native `default` when
  unknown/removed. Never overwrite an explicit selection during polling. Web
  and TUI account management may use a temporary isolated official auth server for
  add/re-login; never start conversation workers or restore retired policy.
  Keep login URLs/codes owner-scoped and out of logs/durable receipts.
- The Rust TUI owns one serial local stdio bridge to validated Go account/catalog
  code. No HTTP listener or per-keystroke subprocess. Restore terminal state and
  reap the owned bridge/auth helpers before exec into the official-CLI launcher.
  Do not block navigation on background reads or replay uncertain mutations.
- The foreground launcher replaces itself with the installed official CLI. The
  web launches that CLI in named codexm-cli-* tmux terminals, reads their output,
  and forwards literal input. It does not interpret chat turns or run model logic.
  Closing a web viewer must not stop the CLI. Never replay uncertain input.
- Before a fresh foreground resume, reuse a matching live codexm tmux CLI only
  after verifying the receipt, pane/process and that process's open thread-writer
  file. Preserve running authentication, including old default sessions. Refuse
  an account mismatch instead of silently reusing a different selection. A GUI
  stdio owner needs owner-side archive or the explicitly confirmed GUI restart;
  a new CLI archive cannot release that writer. The desktop's optional shared-daemon mode is not enabled by codexm.
- Keep the TUI event reader separate from the main signal/shutdown loop. Test
  PTY loss and SIGTERM, including disappearance of owned frontend/bridge PIDs,
  not only disappearance of a tmux session.
- The Meteor listener stays authenticated and loopback-only. Explicit public access
  requires a configured password and an HTTPS reverse proxy with an exact origin;
  never expose the native listener or retain plaintext passwords. Its bridge methods
  are an explicit allowlist. Render user/native output as text. No shell evaluation
  of thread IDs, account names, input, directory names or terminal keys.
- Keep running work and old metadata. During migration retire only verified idle
  legacy product services; do not stop official Codex, GUI or unrelated tmux jobs.
  New Codex-related sessions use `tmux -L codex` for create/list/attach/control.
  Never alias all tmux globally. Receipts without a server retain `-L default`;
  do not move or kill live legacy sessions. Test sessions are cleaned up afterward.
- Run Go race tests/vet and relevant isolated official-CLI tests; run web checks
  when changing web behavior. Exercise Rust TUI keys/account mutations/handoff in
  an isolated tmux PTY. Use synthetic accounts/chats for handoff tests.
  Report unsupported ownership cases instead of hiding failures.
- This repository alone owns source, builds, installation, updates and product docs.
  `.home` only registers an installed command and loads personal shell settings.
  Never restore product cloning/pins/build/install/update helpers there.
- Prefer Rust for new native modules and standalone data/file processing tools.
  Reuse the root Cargo workspace, locked dependencies, and `crates/` libraries.
  Keep TypeScript for Meteor/React; do not add Go/Rust subprocesses or FFI to a hot
  path solely to change languages. Replace an old implementation in a bounded,
  tested unit; measure before claiming speedups. Existing active Go code remains
  until its replacement passes behavior and installation checks. See docs/rust.md.
- Run cargo fmt/check, locked workspace tests and clippy for Rust changes. The
  state audit is manual developer diagnostics, not automatic account/sync logic.
- Source builds/installations include matching Go and Rust TUI siblings in one
  immutable version. Hash both; replace executed Mach-O files using a new inode.
  Web is optional compatibility, not a dependency of the normal TUI install.
- Cross-machine history-sync is retired. Do not restore engines, service entries,
  compatibility links or automatic history writes. Preserve archival data.
- The user authorized the local Mac, mac-secondary and mac-mobile for this change. Verify
  matching artifacts and host-local settings; do not copy auth/history/preferences.
  Homebrew, releases/tags/npm and deployment to other hosts remain on hold.
  Commit and push reviewed source changes; preserve concurrent edits and secrets.
