# Manual metadata diagnostic

`python3 tools/diagnostics/codex_state_audit.py --help` exposes the existing
filesystem-metadata snapshot/diff helper previously kept in `.home`.
It is a developer-only, optional Python tool, not a CLI runtime dependency or an
installed public command. Snapshots remain outside Git in the selected native
home's `auth-switch-audit/`. Do not commit account data or generated snapshots.

The account helper and Python history/archive implementation were retired.
Use `codexm account`, official `codex resume`, and `codexm history-sync` for current
commands. Historical code can be recovered from the pre-consolidation Git refs;
do not restore old shell wrappers to the executable path.
