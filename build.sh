#!/bin/sh
set -eu
source_root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
target_os=${1:-$(uname -s | tr '[:upper:]' '[:lower:]')}
target_arch=${2:-$(uname -m)}
case "$target_arch" in aarch64) target_arch=arm64 ;; x86_64) target_arch=amd64 ;; esac
case "$target_os/$target_arch" in darwin/arm64|darwin/amd64|linux/arm64|linux/amd64) ;; *) echo 'Unsupported platform' >&2; exit 2 ;; esac
platform_root="$source_root/../../artifacts/$target_os-$target_arch"
build_id="build-$(date -u +%Y%m%dT%H%M%S)-$$"
build_output=${3:-"$platform_root/codexm/$build_id/codexm"}
mkdir -p "$(dirname -- "$build_output")"
output_parent=$(CDPATH= cd -- "$(dirname -- "$build_output")" && pwd)
build_output="$output_parent/$(basename -- "$build_output")"
cd "$source_root"
CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -buildvcs=false -o "$build_output" ./cmd/codexm
if [ "$#" -lt 3 ]; then
  ln -s "codexm/$build_id" "$platform_root/.codexm-current-$$"
  case "$(uname -s)" in
    Darwin) mv -fh "$platform_root/.codexm-current-$$" "$platform_root/codexm-current" ;;
    *) mv -fT "$platform_root/.codexm-current-$$" "$platform_root/codexm-current" ;;
  esac
fi
printf '%s\n' "$build_output"
