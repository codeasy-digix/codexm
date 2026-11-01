#!/bin/sh
set -eu
source_root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
target_os=${1:-$(uname -s | tr '[:upper:]' '[:lower:]')}
target_arch=${2:-$(uname -m)}
case "$target_arch" in aarch64) target_arch=arm64 ;; x86_64) target_arch=amd64 ;; esac
case "$target_os/$target_arch" in darwin/arm64|darwin/amd64|linux/arm64|linux/amd64) ;; *) echo 'Unsupported platform' >&2; exit 2 ;; esac
platform_root="$source_root/dist/$target_os-$target_arch"
build_id="build-$(date -u +%Y%m%dT%H%M%S)-$$"
build_output=${3:-"$platform_root/codexm/$build_id/codexm"}
mkdir -p "$(dirname -- "$build_output")"
output_parent=$(CDPATH= cd -- "$(dirname -- "$build_output")" && pwd)
build_output="$output_parent/$(basename -- "$build_output")"
cd "$source_root"
CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -buildvcs=false -ldflags '-s -w -buildid=' -o "$build_output" ./cmd/codexm
cargo_bin=$(command -v cargo 2>/dev/null || true)
if [ -z "$cargo_bin" ] && [ -x "$HOME/.cargo/bin/cargo" ]; then cargo_bin="$HOME/.cargo/bin/cargo"; fi
[ -n "$cargo_bin" ] || { echo 'Rust toolchain is required for the terminal UI source build.' >&2; exit 1; }
case "$target_os/$target_arch" in
  darwin/arm64) rust_target=aarch64-apple-darwin ;;
  darwin/amd64) rust_target=x86_64-apple-darwin ;;
  linux/arm64) rust_target=aarch64-unknown-linux-gnu ;;
  linux/amd64) rust_target=x86_64-unknown-linux-gnu ;;
esac
"$cargo_bin" build --manifest-path "$source_root/Cargo.toml" --locked --release --package codexm-tui --target "$rust_target" --target-dir "$source_root/target"
# Replace the inode instead of overwriting a previously executed Mach-O file;
# macOS can otherwise keep its old code-signing cache and kill the new process.
tui_staged=$(mktemp "$output_parent/.codexm-tui-XXXXXX")
trap 'rm -f "$tui_staged"' EXIT HUP INT TERM
cp "$source_root/target/$rust_target/release/codexm-tui" "$tui_staged"
chmod 755 "$tui_staged"
mv -f "$tui_staged" "$output_parent/codexm-tui"
printf '%s\n' "$build_output"
