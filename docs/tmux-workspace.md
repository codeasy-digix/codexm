# Persistent CLI workspace

`codexm` keeps the conversation picker and account controls. Enter first looks for
an existing interactive Codex CLI owning the selected conversation. It reconnects
to that screen, preserving authentication and work. It submits no new prompt.
Without an existing CLI, it creates an official `--no-daemon` CLI in a new window
of the default tmux server's `codexm` session and attaches the current terminal.

```sh
codexm                         # select a chat, then Enter
codexm resume THREAD --account NAME
tmux attach -t codexm
tmux list-windows -t codexm
tmux list-panes -s -t codexm -F '#{window_name} #{pane_id} #{pane_pid}'
tmux capture-pane -p -t codexm:WINDOW -S -30
```

Detach with `Ctrl-b d`; the CLI keeps running. Return through the picker or tmux.
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

The change passed Go race tests and vet, Rust workspace tests and clippy. Isolated
real tmux tests exercised a shell-launched native fixture with an actual writer
lock, unchanged PID and no replay, new CLI input, neighboring-window preservation,
and account replacement including a writer that refuses to exit. No real account
or history is used in those automated tests.

On three macOS hosts, matching native Go/Rust components were installed atomically.
Live migration compared each pane's PID and terminal device before/after. On a
remote Mac, both direct reconnect and TUI search/Enter reconnected existing CLIs;
the original PIDs and start times were retained. Neither GUI restart nor history
migration was part of that verification. This workspace update does not change
Homebrew tags or the tap. Existing open TUI processes must be exited and reopened
to load the new picker; already running official CLIs keep working.
