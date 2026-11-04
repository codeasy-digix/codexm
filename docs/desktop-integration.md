# Shared desktop accounts

`apps/macos` is a frontend of CodexM, not another account system. One serial local
stdio connection uses the same account homes and native-default operations as the
CLI/TUI. The bridge exposes `accounts.catalog`, `accounts.usage`,
`accounts.presentation`, `accounts.importLegacy`, `accounts.activate`,
`accounts.remove`, `accounts.setDefault` and owner-scoped `login.*` operations.
Only `accounts.activate` intentionally consumes model quota; it requires explicit
opt-in in the menu and a current account revision.

The catalog reads local identity/display metadata without scanning conversation
DBs or opening official account servers. Usage is read through a short-lived
owned official account probe. No app authentication bytes cross the stdio API.
A logical validation error keeps the bridge alive; a transport error is displayed
without automatically replaying mutations. Closing the app closes its owned bridge
and authentication helpers, not official GUI/CLI sessions.

Presentation is stored in `config.toml.accounts.json` beside the selected CodexM
configuration, private to the user. Its keys combine user and workspace identity;
they cannot mix different members of the same workspace. An account revision also
includes the resolved credential target. Token refresh alone keeps the identity;
a default target/login change invalidates stale edits and usage. The app's default
card always drives the menu icon, even after reordering.

The old menu app's registry is imported once, copy-only, with a private receipt and
rollback files in the CodexM state directory. Removed aliases remain removed. No
model/effort preference, native conversation DB, writer lock or history-sync engine
is written or introduced. The old bundle ID is retained solely for local preference
and login-item continuity. See [menu app](../apps/macos/README.md) for user steps.

The repository owns source, builds, installation and updates. `.home` may register
an installed command and load personal shell settings; it never builds this product.
