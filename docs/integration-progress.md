# Desktop integration status

CodexM owns the menu app, CLI/TUI, account backend, builds and distribution.
The former menu app is integrated under `apps/macos`; there is no second product
credential registry or embedded official Codex runtime.

The shared stdio backend now serves catalog, usage, browser/device login,
presentation, unregister and native-default selection. Login identity contains
both user and workspace. Revision checks reject stale reads and mutations across
default changes. Account registration does not restore retired worker policy.

Four UI languages, display names/order/preferences and explicit opt-in activation
remain. Activation aliases share a conservative identity schedule. Login
cancellation and application shutdown clean up their owned helpers. Native
conversations, active Codex work and account-server settings are preserved.

The 0.7.0 source Formula builds matching CLI/TUI components on macOS and Linux,
and the menu app on macOS. The macOS app is ad-hoc signed locally; a notarized
Cask remains future work requiring Developer ID signing. Python is not a runtime
dependency. Web remains optional compatibility, separate from the default product.

See [release validation](release-validation-0.7.0.md),
[distribution](distribution.md) and [desktop design](desktop-integration.md).
Private migration capsules, original Git backups, raw audit reports, credentials
and local test evidence remain outside this repository. `.home` owns no product
builds or source copies. Cross-machine history-sync remains retired.
