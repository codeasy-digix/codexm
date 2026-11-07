#!/bin/sh
# Developer-only build. No installation, native settings, signing or packaging changes.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
cargo build --locked --release -p codexm-gui -p codexm-tui
mkdir -p "$root/target"
output=$(mktemp -d "$root/target/gui-experiment.XXXXXX")
go build -tags gui_experiment -o "$output/codexm" ./cmd/codexm
cp "$root/target/release/codexm-gui" "$output/codexm-gui"
cp "$root/target/release/codexm-tui" "$output/codexm-tui"
chmod 755 "$output/codexm" "$output/codexm-gui" "$output/codexm-tui"
git rev-parse HEAD > "$output/source-commit"
if ! git diff --quiet HEAD; then printf '%s\n' dirty > "$output/source-dirty"; fi
(cd "$output" && shasum -a 256 codexm codexm-gui codexm-tui > SHA256SUMS)
printf '%s\n' "$output/codexm"
