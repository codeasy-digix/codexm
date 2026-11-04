# Desktop integration progress

The private integration branch remains a development record. Public publication
must use a separately sanitized clone; do not change the private repo's visibility.

Completed implementation and local validation:

- Menu app now uses the shared serial stdio backend for catalog, usage, login,
  presentation, unregister and native-default selection. Its duplicate credential
  registry, embedded official runtime and old process clients are removed.
- Default/login revisions protect stale reads and mutations. Login identity includes
  both user and workspace. Registration no longer silently changes retired worker
  priority policy. Login cancellation before the challenge arrives is cleaned up.
- Swift pipe transport reads available bytes instead of waiting to fill a buffer;
  real backend integration confirms requests continue after validation errors.
- Four UI languages, name/order/preferences and explicit opt-in activation remain.
  Activation aliases share an identity schedule; old timing is migrated conservatively.
- Go race tests and vet, Rust locked workspace tests/fmt/clippy, web typecheck/tests,
  70 Swift tests including real bridge and rendered locale views passed locally.
  The Rust TUI passed its isolated owned tmux PTY interaction/shutdown test.
- A matching 0.7.0 CLI/TUI/macOS app build was produced. It is ad-hoc signed only.
  The active host has no former menu registry to import; synthetic migration tests
  cover identity reuse, copy-only import, source preservation and idempotence.
- Build/install docs and a source-based Homebrew Formula template are being prepared.
  The Go-only/npm packager is retired; native archives require both matching binaries.

Remaining release gates:

1. Finish isolated official CLI/auth checks and controlled installed app verification.
2. Finish packaging/CI and validate the source Formula on macOS and Linux.
3. Sanitize all published Git objects, author/committer/message/tag metadata, names,
   source paths and release assets in a separate clone. Re-scan for secrets and
   encoded private identifiers; retain original private backups and license attribution.
4. Publish the cleaned source repository/release/tap only after their checks pass.
5. Remove the former menu app GitHub repository only after integrated distribution
   verification and preserved rollback backups. No public release or repo removal
   is complete at this checkpoint.

Local evidence, raw audit reports and migration backups belong in private state
outside this repository. `.home` owns no product builds or source copies.
