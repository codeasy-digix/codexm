# Distribution

CodexM is one source repository: https://github.com/codeasy-digix/codexm.
The Homebrew tap is https://github.com/codeasy-digix/homebrew-tap.
Support: **support@digix.kr**. The old menu app is integrated under `apps/macos`.

The default Homebrew Formula builds the matching CLI/TUI from audited source on
macOS and Linux, and also builds `CodexM.app` on macOS. Go and Rust are build-only
dependencies, Swift is the macOS system toolchain, and Python is not a product
runtime dependency. The official Codex CLI must be installed separately. No native
Codex binary, login, conversation, machine config or history-sync engine is bundled.
`codex` and its updater are never wrapped or replaced.

The locally built macOS app is ad-hoc signed. This is not Developer ID signing or
notarization. A binary Cask/App Store release is not claimed. Do not strip quarantine
or disable Gatekeeper to distribute an unnotarized app. A future Cask requires a
verified Developer ID certificate, hardened-runtime signing, notarization and a
stapled ticket, followed by a real clean-install check.

```sh
brew install codeasy-digix/tap/codexm
codexm
# macOS menu app:
open "$(brew --prefix codexm)/CodexM.app"
```

The Formula and caveats preserve user configuration. Upgrading does not change the
native default, migrate credentials automatically, enable activation, start a service,
stop existing work, or install a web server. The menu offers an explicit legacy
account import; review its preview first. Keep only one copy of the menu app running.

## Release procedure

1. Validate Go race/vet, locked Rust tests/clippy/fmt, Swift shared-bridge/locale/UI
   tests, web contracts and isolated official CLI/TUI behavior. Versions in `VERSION`,
   Go, Cargo and desktop metadata must match.
2. Preserve original private Git bundles. In a separate clone sanitize all source,
   published history/commit metadata/tags/ref names, file paths and assets. Keep raw
   audits/backups/auth outside Git. Never make an unaudited private repository public.
3. Scan every published ref and release file for secrets and private identifiers;
   inspect encoded forms and binaries as well as text. GitHub secret scanning/push
   protection supplements this review; it does not remove arbitrary personal names.
   Keep third-party license attribution intact. Rotate any real leaked credential.
4. Commit/push only cleaned refs to the public repository. Produce the source archive
   with `git archive` from the validated public tag. Preserve clean authors and no
   personal usernames in archive ownership. Generate the Formula with
   `scripts/render-formula.sh VERSION SOURCE_SHA256`; it points to that exact release
   asset, never a private repo or moving branch.
5. Native archives, when supplied, contain both `codexm` and `codexm-tui` plus license
   notices. Build on each target platform, then run `scripts/package-native.sh`.
   Never publish the obsolete Go-only archive. Collect archive checksums into
   `SHA256SUMS`, audit the final assets, and publish the release.
6. Validate the tap Formula from source on macOS and Linux and run `brew test` before
   publishing its update. Remove the former menu app repository only after verified
   integrated deployment/distribution and private rollback backups are retained.

`build.sh` and `install.sh` remain independent source/standalone installers. Their
matching native pair lives in one immutable version and is replaced using fresh
inodes. `.home` only registers installed tools and personal shell settings; it has no
product clone/pin/build/update role. The old npm preparation and embedded Codex runtime
are retired. No public artifact may contain the private migration/validation capsule.

The 0.7.0 distribution uses the source Formula. Use `./install.sh --build` for a
standalone source installation; the no-argument installer requires optional native
archives, which are not promised for every release/platform. An incomplete native
pair is rejected before switching an installed entrypoint.
