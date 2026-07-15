#!/bin/zsh

set -euo pipefail

ROOT_DIR="${0:A:h:h}"
APP_NAME="Codex Usage.app"
OUTPUT_APP="$ROOT_DIR/dist/$APP_NAME"
STAGING_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/codex-usage-app.XXXXXX")"
trap 'rm -rf "$STAGING_ROOT"' EXIT
APP_DIR="$STAGING_ROOT/$APP_NAME"
CONTENTS_DIR="$APP_DIR/Contents"
APP_STORE_BUILD="${APP_STORE_BUILD:-0}"

cd "$ROOT_DIR"

swift build -c release

if [[ ! -f "$ROOT_DIR/Resources/AppIcon.icns" ]]; then
    swift "$ROOT_DIR/scripts/generate-icon.swift" "$ROOT_DIR/Resources/AppIcon.icns"
fi

rm -rf "$APP_DIR"
mkdir -p "$CONTENTS_DIR/MacOS" "$CONTENTS_DIR/Resources"

cp "$ROOT_DIR/.build/release/CodexUsageMenuBar" "$CONTENTS_DIR/MacOS/CodexUsageMenuBar"
RUNTIME_SOURCE="$("$ROOT_DIR/scripts/prepare-codex-runtime.sh")"
cp "$RUNTIME_SOURCE" "$CONTENTS_DIR/MacOS/CodexRuntime"
chmod 755 "$CONTENTS_DIR/MacOS/CodexRuntime"
cp "$ROOT_DIR/Resources/Info.plist" "$CONTENTS_DIR/Info.plist"
cp "$ROOT_DIR/Resources/AppIcon.icns" "$CONTENTS_DIR/Resources/AppIcon.icns"
cp "$ROOT_DIR/THIRD_PARTY_NOTICES.md" "$CONTENTS_DIR/Resources/THIRD_PARTY_NOTICES.md"
cp "$ROOT_DIR/Resources/CodexRuntime-LICENSE.txt" "$CONTENTS_DIR/Resources/CodexRuntime-LICENSE.txt"

# Cloud-synced folders can attach Finder metadata to a newly created bundle.
# Distribution signatures reject those extended attributes.
xattr -cr "$APP_DIR"

if [[ "$APP_STORE_BUILD" == "1" ]]; then
    codesign --force --sign - --entitlements "$ROOT_DIR/Resources/CodexRuntime.entitlements" "$CONTENTS_DIR/MacOS/CodexRuntime"
    codesign --force --sign - --entitlements "$ROOT_DIR/Resources/CodexUsage.entitlements" "$APP_DIR"
else
    codesign --force --sign - "$CONTENTS_DIR/MacOS/CodexRuntime"
    codesign --force --sign - "$APP_DIR"
fi
codesign --verify --deep --strict "$APP_DIR"

rm -rf "$OUTPUT_APP"
mkdir -p "$ROOT_DIR/dist"
/usr/bin/ditto --norsrc "$APP_DIR" "$OUTPUT_APP"

echo "$OUTPUT_APP"
