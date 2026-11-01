# Manual metadata diagnostic

The former `codex_state_audit.py` is replaced by the Rust library and CLI in
`crates/state-audit`. Use `cargo build --release --locked -p codexm-state-audit`,
then `./target/release/codexm-state-audit --help` from the repository root.
No Python runtime is needed. It remains an explicit developer diagnostic, not
automatic account/sync logic or an installed public product command.

Snapshots default to XDG state outside the native Codex home. Old schema-1 JSON
is still accepted for explicit diff. Do not commit snapshots or native data.
Build, usage and migration boundaries are in [Rust development](../../docs/rust.md).

The account helper and Python history/archive implementation were retired.
Use `codexm account` and official `codex resume` for current commands.
Cross-machine history-sync is removed. Historical code can be recovered from the pre-consolidation Git refs;
do not restore old shell wrappers to the executable path.
