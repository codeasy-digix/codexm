# codexm

- The active product is a local conversation catalog, account selector and official
  Codex CLI launcher. Keep `codex` native: never wrap/replace its binary or update
  command. `codexm account` may change the current shell or the explicit native default.
- `apps/macos` is the same product's Swift menu frontend. Reuse its one local stdio
  bridge for accounts, authentication, usage, presentation and native-default changes.
  Do not restore a second credential registry or bundle the official Codex runtime.
  Preserve four locales and the compatibility preference domain. Legacy import is
  one-time and copy-only; existing credentials/default/history are never overwritten.
  Identity includes user and workspace; stale revisions cannot update a new login.
- Menu usage activation is an explicit opt-in exception to passive usage reads.
  Only its bounded `accounts.activate` endpoint makes a model request. Deduplicate
  schedules by login identity, preserve existing timing, and never replay uncertain
  activation or authentication mutations. Do not enable it during migration/tests.
- Running `codexm` opens the Rust TUI in crates/terminal. Enter directly resumes
  with the last registered account or default; Right/Left move to/from details,
  and F2 opens settings/account management. Keep explicit choices across polling.
- A second Right from the account panel opens per-chat archive/restore controls.
  Freeze the selected thread ID across refresh and confirmation; default to cancel.
  Use only official CLI archive/unarchive or the verified existing owner's RPC.
  Never stop GUI/CLI processes, delete native locks, resume a chat, or edit its DB
  from this menu. Verify the resulting archive state and never replay uncertain
  requests. Preserve pending account choices when returning with Left.
- Public entrypoints are in internal/cli/compact.go and internal/nativecli. Do not
  start the old supervisor, workers, manager model, scheduler, account rotation,
  watchdog, notifications or management plugin from the compact path. Prior
  implementation and tests remain migration references, not active commands.
  Legacy CLI files require `-tags legacy`; they are excluded from normal builds.
- Explicit `handoff THREAD --account NAME` is a bounded exception: verify a
  current codexm launch receipt, PID incarnation, executable and exactly one
  native writer before SIGTERM. Wait for both process exit and writer release
  before starting the same ID with one continuation prompt. Never SIGKILL or
  replay uncertain prompts. The temporary helper uses one window in default/codexm and exits.
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
  SQLite/rollouts, kill an unverified external CLI, silently fork, or assume unarchive
  guarantees takeover of a live writer. Preserve IDs, credentials, goals and budgets.
- A verified macOS GUI writer may be stopped with SIGTERM only after an explicit
  foreground terminal confirmation for that execution. Show that all GUI jobs
  stop and do not auto-continue. Never persist consent, accept piped confirmation,
  SIGKILL the GUI, or retry uncertain archive mutations via process termination.
  Arm the bounded GUI-reopen helper before stopping it, reopen after the selected
  CLI owns its writer or on failure, and preserve unrelated CLI/daemon processes.
- A verified external `codex exec` writer may be taken over only after fresh
  foreground terminal consent (default No). Verify current user, exact configured
  runtime, OS argv command, PID incarnation and exactly one loaded chat; revalidate
  after consent. Signal that PID once with SIGTERM and wait for both exit and writer
  release. Never kill its launcher, a shared app-server, another CLI or an ancestor
  of the requesting process. No piped/persisted approval, SIGKILL or automatic retry.
  Resume the same ID/account in the official CLI without an implicit new prompt.
  A replacement writer must fail the prepared launch, not trigger another archive
  or takeover. Thread creation source and old launch records are not ownership proof.
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
- The foreground launcher connects to a persistent official CLI window in the
  default tmux server's single codexm workspace. Each terminal attaches through
  its own temporary grouped viewer, with an independent current window. Reap
  only that viewer when it disconnects; the workspace and jobs must survive.
  Never persist a viewer as a chat's durable location or retarget other clients.
  The web reads the same terminals,
  and forwards literal input. It does not interpret chat turns or run model logic.
  Closing a web viewer must not stop the CLI. Never replay uncertain input.
- Before a fresh resume, find an existing interactive CLI by its writer file,
  process incarnation and pane ancestry, including ordinary codex launches.
  Connecting preserves the running authentication and submits no continuation.
  Explain that changing accounts requires an explicit handoff. Discovery never
  grants process-termination authority. A GUI
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
  All new jobs use named windows in the default server’s single `codexm` session.
  Automatic `codexm-view-*` group members are terminal views, not job containers.
  Never kill this shared session or alias ordinary tmux. Legacy codex-server jobs
  remain alive behind workspace connection windows until they exit. Preserve old
  receipt routing. Cleanup targets only a verified owned pane/window.
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
- Desktop changes require Swift tests including the isolated real stdio bridge and
  locale layouts. Homebrew source builds include the matching app on macOS; Go/Rust
  are build dependencies only. Ad-hoc local signing is not notarized distribution.
- Cross-machine history-sync is retired. Do not restore engines, service entries,
  compatibility links or automatic history writes. Preserve archival data.
- The active integration may be continued on the user's explicitly selected Mac.
  Preserve other hosts and their live installations; source handoff is not a
  request to deploy unfinished binaries everywhere. Never copy account credentials.
- The user now authorizes public source and Homebrew distribution only after a
  complete privacy audit of current files, Git history/metadata and release assets.
  Preserve original private backups. Keep unfinished or unaudited history private;
  do not flip repository visibility before the cleaned publication passes checks.
  Commit and push reviewed source changes; preserve concurrent edits and secrets.
