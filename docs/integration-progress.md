# Desktop integration checkpoint

This is a work-in-progress handoff, not a release. The installed product has not
been replaced. Keep this branch private until the publication checks below pass.

## Accepted direction

- One CodexM repository owns the Rust TUI, validated existing Go account backend
  and Swift menu app in `apps/macos`. Keep the old app's MIT attribution and four
  UI languages. The old app's history was imported with a full Git subtree merge.
- The menu app must use the same local stdio account catalog, authentication,
  usage and native-default operations as the CLI. Remove its duplicate production
  credential registry/runtime implementation after behavioral tests pass.
- Preserve display/workspace names, order, preferences and explicit opt-in quota
  activation. Migration is copy-only and must not overwrite existing credentials,
  native defaults, conversation history, model/effort preferences or live writers.
- Sources, binaries and the Homebrew tap will be public after privacy review.
  Linux gets the CLI/TUI; macOS also gets the menu app. Do not publish private audit
  reports, transcript exports, backups or local migration state.

## Implemented but not fully validated

- Shared identity/revision, catalog/presentation, usage, one-time legacy import and
  opt-in activation bridge endpoints exist in `internal/accounts` and
  `internal/nativecli`.
- A Swift stdio bridge scaffold exists in
  `apps/macos/Sources/CodexUsageMenuBar/CodexMBridge.swift`. It is not wired into
  `UsageStore` yet. Its DTO expects account fields that still need implementation.
- The main Go module and first-party URLs now use the organization namespace.
  Personal examples were replaced with neutral names. Production config no longer
  predefines the developer's login list or fallback account.

## Known failing checks and next work

The focused Go account/native/CLI checks passed before the privacy transformation.
The subsequent full `go test ./...` does **not** pass. Do not describe this snapshot
as tested or install it as a stable build.

1. Update test fixtures that depended on the former production default login list.
   Set fictional priorities explicitly in legacy fixtures; keep new-install defaults
   empty. Audit identity discovery and first-login config behavior, rather than
   hiding regressions by restoring personal defaults.
2. Fix an escaped account-name fixture that survived word-boundary substitution in
   `internal/cli/account_test.go`. Extend privacy checks to escaped/encoded values,
   filenames, authors/committers, messages, tags and every published ref.
3. Finish and test Swift shared-backend integration. Remove duplicate credential
   readers, embedded official-runtime handling and obsolete tests only when their
   replacement coverage exists. Preserve default-switch/stale-response protections.
4. Align the activation expression protocol with the existing Swift generator;
   preserve opt-in schedules and avoid duplicate activation of aliases of one login.
5. Complete CLI/Swift/Rust/web contract checks and isolated real-CLI validation,
   then controlled local migration and installed UI validation.
6. Finish history sanitization in a separate clone, re-scan it, and publish only
   cleaned refs/assets to a clean organization repository. The existing private
   source repository and original bundles must remain private. A general scan found
   one false positive in a validation checksum; this is not proof of no secrets.
7. Reconcile versioning, reproducible packages, signing/notarization and Homebrew.
   Delete the former app repository only after backups, integrated deployment and
   distribution verification. No source repository or release has been deleted or
   published by this checkpoint.

Machine-specific handoff details and test/audit reports live in the private
`~/.local/state/codexm/` capsule; never commit that capsule or credentials.
