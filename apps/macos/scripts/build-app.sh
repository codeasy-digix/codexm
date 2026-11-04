#!/bin/sh
# Build the desktop from this repository and matching native binaries.
set -eu
app_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
product_root=$(CDPATH= cd -- "$app_root/../.." && pwd)
output=${CODEXM_APP_OUTPUT:-$app_root/dist/CodexM.app}
native_dir=${CODEXM_NATIVE_BUILD_DIR:-}
if [ -z "$native_dir" ]; then
  native_binary=$("$product_root/build.sh")
  native_dir=$(dirname -- "$native_binary")
fi
version=$(cat "$product_root/VERSION")
[ "$("$native_dir/codexm" --version)" = "codexm $version" ] || { echo 'Mismatched CLI build' >&2; exit 1; }
[ "$("$native_dir/codexm-tui" --version)" = "codexm-tui $version" ] || { echo 'Mismatched TUI build' >&2; exit 1; }
[ ! -e "$output" ] || { echo "Output already exists; preserved: $output" >&2; exit 1; }
stage=$(mktemp -d "${TMPDIR:-/tmp}/codexm-app.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
cd "$app_root"
set --
# Homebrew already applies the outer build sandbox; macOS refuses nesting
# SwiftPM's sandbox-exec inside it. Keep normal source builds sandboxed.
if [ "${CODEXM_SWIFT_EXTERNAL_SANDBOX:-0}" = 1 ]; then set -- --disable-sandbox; fi
swift build "$@" -c release --scratch-path "$stage/swift" -Xswiftc -file-prefix-map -Xswiftc "$product_root=." >&2
app="$stage/CodexM.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources/native"
cp "$stage/swift/release/CodexUsageMenuBar" "$app/Contents/MacOS/CodexUsageMenuBar"
cp -R "$stage/swift/release/CodexUsageMenuBar_CodexUsageMenuBar.bundle" "$app/Contents/Resources/"
cp "$app_root/Resources/Info.plist" "$app/Contents/Info.plist"
cp "$native_dir/codexm" "$native_dir/codexm-tui" "$app/Contents/Resources/native/"
cp "$product_root/LICENSE" "$app/Contents/Resources/LICENSE.txt"
cp "$product_root/THIRD_PARTY_NOTICES.txt" "$app/Contents/Resources/THIRD_PARTY_NOTICES.txt"
cp "$app_root/THIRD_PARTY_NOTICES.md" "$app/Contents/Resources/THIRD_PARTY_NOTICES.md"
if [ -f "$app_root/Resources/AppIcon.icns" ]; then
  cp "$app_root/Resources/AppIcon.icns" "$app/Contents/Resources/"
else
  swift "$app_root/scripts/generate-icon.swift" "$app/Contents/Resources/AppIcon.icns" >&2
fi
xattr -cr "$app"
identity=${CODEXM_SIGN_IDENTITY:--}
for binary in "$app/Contents/Resources/native/codexm" "$app/Contents/Resources/native/codexm-tui"; do
  codesign --force --sign "$identity" "$binary" >&2
done
if [ "$identity" = '-' ]; then
  codesign --force --sign - "$app" >&2
else
  codesign --force --options runtime --timestamp --entitlements "$app_root/Resources/CodexM.entitlements" --sign "$identity" "$app" >&2
fi
codesign --verify --deep --strict "$app" >&2
"$app/Contents/MacOS/CodexUsageMenuBar" --localization-diagnostics > "$stage/localization.json"
mkdir -p "$(dirname -- "$output")"
/usr/bin/ditto --norsrc "$app" "$output"
printf '%s\n' "$output"
