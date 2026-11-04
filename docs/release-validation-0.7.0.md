# CodexM 0.7.0 validation

The integrated menu app uses the same account catalog and native-default role as
the CLI/TUI. The normal install contains one matching native pair and, on macOS,
the Swift menu app. The official Codex CLI is installed independently.

| Check | Observed result |
|---|---|
| Go race suite and vet | Passed locally and in macOS/Linux CI |
| Rust locked workspace tests, fmt, clippy | Passed locally and in macOS/Linux CI |
| Rust TUI owned PTY navigation/shutdown | Passed locally and in macOS/Linux CI |
| Swift account/UI/locale tests | 70 passed, including the real stdio bridge |
| Optional web typecheck/contracts | Passed |
| Official CLI preparation/account replacement | Passed with isolated synthetic homes and a local mock model |
| Official browser auth preview/cancel | Passed in an empty isolated home; no completed login or paid model call |
| Homebrew source build and `brew test` | Passed on Apple Silicon macOS and ARM64 Linux |
| Built app signature and localization | Ad-hoc signature verified; all four catalogs complete |
| Installed menu app start/stop | Shared bridge started and both owned processes exited; no native Codex jobs stopped |
| Installer failure preservation | Corrupt/incomplete native packages leave existing entrypoints intact |

Tests cover copied default credentials, same-workspace/different-user identities,
token refresh, stale usage/default edits, explicit account selection, login
cancellation, migration idempotence and preserved preferences. No live default
login was changed as part of verification. The test host had no old menu registry;
legacy migration coverage uses synthetic fixtures, not imported private credentials.

The public source was prepared separately from private development history.
All published Git objects/ref names and author metadata were inspected; known
personal identifiers, local paths and encoded variants were sanitized. Gitleaks
and an independent identifier/known-secret check reported no unexpected findings.
Third-party license attribution and the Rust `ryu` dependency name remain intact.
GitHub secret scanning and push protection are enabled as additional checks.

Release builds remap source/home/dependency cache paths. The installed macOS Go,
Rust and Swift binaries were checked for development usernames and home paths;
none remained. Private audit material and original Git bundles are retained
outside the public repository. This is a review of the published material, not a
guarantee that GitHub can remove historical external caches or copies.

The release distribution is a source Formula. No notarized app/Cask, embedded
Codex runtime, automatic account rotation service or history synchronization is
claimed. Existing live CLI/GUI jobs were preserved throughout validation.
