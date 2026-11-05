# Persistent CLI workspace

`codexm` keeps the conversation picker and account controls. Enter first looks for
an existing interactive Codex CLI owning the selected conversation. It reconnects
to that screen, preserving authentication and work. It submits no new prompt.
Without an existing CLI, it creates an official `--no-daemon` CLI in a new window
of the default tmux server's `codexm` workspace. Each terminal attaches through a
fresh `codexm-view-*` grouped session, sharing the workspace's windows and
processes but keeping its own current/previous window. Opening chat B in another
terminal does not move a terminal watching chat A. Opening the same chat twice
shares that CLI's screen and input; it does not start another writer.

```sh
codexm                         # select a chat, then Enter
codexm resume THREAD --account NAME
codexm terminal SESSION        # reconnect using the receipt shown by codexm
tmux list-windows -t codexm
tmux list-panes -s -t codexm -F '#{window_name} #{pane_id} #{pane_pid}'
tmux capture-pane -p -t codexm:WINDOW -S -30
```

Detach with `Ctrl-b d`; the CLI keeps running. Return through the picker or the
printed `codexm terminal` command. The last client leaving a viewer destroys only
that temporary group member, including on terminal loss; no background cleanup
daemon is needed. `codexm` itself remains detached with its work intact. Viewer
names are not persisted in chat receipts. An account handoff follows only clients
watching the old chat, giving each its own view of the replacement window.

For administration, `tmux attach -t codexm` still opens the shared workspace view.
Its clients share the current window; use `codexm` for independent terminal views.
Existing clients are not detached or moved during installation. Detach those old
connections and reconnect through `codexm` to use the independent views. The jobs
need no restart. When invoking the picker inside tmux, exactly one attached client
must be viewing its pane. If several clients share that picker, codexm cannot tell
which supplied a key and asks you to reconnect from a separate terminal instead
of switching an arbitrary client. A detached picker cannot switch another client.

See the [tmux session-group documentation](https://man.openbsd.org/tmux.1#new-session)
for the shared-window and independent-selection semantics.

Use `/exit` inside a CLI to finish it. Cleanup removes only an owned pane, never
the shared workspace. An explicit account handoff remains a separate operation;
choosing a different account while reconnecting does not change a live login.

Discovery checks the selected writer file, one loaded chat, process identity and
pane ancestry, including an npm wrapper or login shell. It does not trust a title,
an old PID, or a conversation UUID in a command string. Discovering an unmanaged
CLI never grants permission to terminate it. GUI/app-server owners and terminals
outside tmux are not attachable CLI screens. GUI handoff retains its confirmation
on every execution, default No, and warning that all GUI work stops.

Same-server windows can be linked without interrupting their clients. tmux cannot
move a live PTY between servers. Legacy `codex` server jobs appear through
`legacy-*` connection windows. Their original process/server remains until they
finish; closing the connection window alone does not stop the job. Existing
OS-supervised services keep their supervisor. No global tmux alias is installed.

## Verification

Viewer regression tests use a separate, temporary default socket with synthetic
jobs. They exercise concurrent clients, independent A/B selection, shared input
to one pane, reconnect, normal detach, abrupt client loss, failed attachment and
account-handoff viewer routing. Two real PTYs exercise the production attach path.
They verify that the workspace's window selection and original process IDs remain
unchanged. No user login, active conversation or production history is used.
The viewer checks passed on macOS with tmux 3.6a and 3.8, including a real official
CLI using a synthetic conversation and local mock model endpoint. This viewer
change is installed locally; it does not deploy to other hosts or publish a
Homebrew release.

The change passed Go race tests and vet, Rust workspace tests and clippy. Isolated
real tmux tests exercised a shell-launched native fixture with an actual writer
lock, unchanged PID and no replay, new CLI input, neighboring-window preservation,
and account replacement including a writer that refuses to exit. No real account
or history is used in those automated tests.

During the earlier workspace migration, matching native Go/Rust components were
installed atomically on three macOS hosts.
Live migration compared each pane's PID and terminal device before/after. On a
remote Mac, both direct reconnect and TUI search/Enter reconnected existing CLIs;
the original PIDs and start times were retained. Neither GUI restart nor history
migration was part of that verification. This workspace update does not change
Homebrew tags or the tap. Existing open TUI processes must be exited and reopened
to load the new picker; already running official CLIs keep working.
