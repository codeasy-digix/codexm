# CodexM menu app

The SwiftUI menu app, CLI and Rust TUI are one product. The app uses one local
`codexm bridge --stdio` process for account discovery, authentication, usage,
presentation and native-default changes. It has no HTTP listener and no separate
credential registry or bundled OpenAI runtime. Install the official Codex CLI
separately; its command, updates, model preferences and conversations stay native.

The menu shows 5h/week usage and reset credits when the account reports them.
Options include browser/device sign-in, re-login, unregister, native-default
selection, display/workspace names, account order and opening the terminal TUI.
The native default follows `~/.codex/auth.json`, including changes made by
`codexm account NAME --set-default`. A login identity includes both user and
workspace; responses and edits from a former default are discarded.

Usage checks do not run a model. The optional **5h activation** setting does: it
submits one ephemeral read-only arithmetic request using an available lightweight
model. It is off on a new installation. Existing opt-in and schedules are preserved;
aliases of the same login share a schedule. A successful request is due again after
five hours, attempts are at least 30 minutes apart, and uncertain requests are never
immediately replayed. Enable this on only one of your own devices if desired.

English, Korean, Simplified Chinese and Hindi, icon preferences and refresh intervals
remain available. The compatibility bundle identifier `org.codeasy.CodexUsage` is
intentional: it preserves the former app's UserDefaults and login-item identity.
It does not mean the old app is a separate product.

## Migration

Quit the former Codex Usage menu app before importing. Choose **Import legacy
accounts** to preview, then confirm, or run:

```sh
codexm migrate-toolbar
codexm migrate-toolbar --apply
```

The one-time importer reuses matching registered logins or creates `usage-UUID`
nicknames. It copies only credentials needed for a new local account; original
credentials and private rollback backups are retained. It never overwrites the
native default, existing named credentials, history, model or GUI preferences.
Display names/order are carried over when no shared presentation already exists.
Skipped records are reported and require re-login/manual review. Repeating import
does not restore removed accounts or overwrite subsequent edits.

Unregister hides an account; it does not erase credentials. Native-default selection
is an explicit action. Account-server preferences are never synchronized.

## Development

```sh
# Root repository: matching Go CLI and Rust TUI, then the Swift desktop.
./build.sh
./apps/macos/scripts/build-app.sh
# Run after quitting only the former menu app:
./apps/macos/scripts/install.sh
open "$HOME/Applications/CodexM.app"
```

The app build accepts `CODEXM_NATIVE_BUILD_DIR` for an already verified CLI/TUI pair
and `CODEXM_APP_OUTPUT` for a fresh output path. Both native versions must match the
root `VERSION`. Test with Xcode's toolchain (Swift Testing may not be present in a
standalone Command Line Tools installation):

```sh
cd apps/macos
DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer swift test
```

Set `CODEXM_TEST_BINARY` to a freshly built CLI to include the isolated real-bridge
test. It uses empty temporary account/config directories, not your logins.
Builds are ad-hoc signed unless a valid `CODEXM_SIGN_IDENTITY` is explicitly supplied.
Ad-hoc signing is not Developer ID signing or notarization. Public Cask distribution
requires separate signing/notarization validation; see [distribution](../../docs/distribution.md).

Source: https://github.com/codeasy-digix/codexm · Support: support@digix.kr
