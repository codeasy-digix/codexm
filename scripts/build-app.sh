#!/bin/zsh

set -euo pipefail

ROOT_DIR="${0:A:h:h}"
APP_NAME="Codex Usage.app"
APP_DIR="$ROOT_DIR/dist/$APP_NAME"
CONTENTS_DIR="$APP_DIR/Contents"

cd "$ROOT_DIR"

swift build -c release

if [[ ! -f "$ROOT_DIR/Resources/AppIcon.icns" ]]; then
    swift "$ROOT_DIR/scripts/generate-icon.swift" "$ROOT_DIR/Resources/AppIcon.icns"
fi

rm -rf "$APP_DIR"
mkdir -p "$CONTENTS_DIR/MacOS" "$CONTENTS_DIR/Resources"

cp "$ROOT_DIR/.build/release/CodexUsageMenuBar" "$CONTENTS_DIR/MacOS/CodexUsageMenuBar"
cp "$ROOT_DIR/Resources/Info.plist" "$CONTENTS_DIR/Info.plist"
cp "$ROOT_DIR/Resources/AppIcon.icns" "$CONTENTS_DIR/Resources/AppIcon.icns"

codesign --force --deep --sign - "$APP_DIR"
codesign --verify --deep --strict "$APP_DIR"

echo "$APP_DIR"
