#!/usr/bin/env bash
# Build ResponseForge release files into private/dist/.
#
#   ./build.sh windows-amd64    Windows exe, AMD64
#   ./build.sh windows-arm64    Windows exe, ARM64
#   ./build.sh macos-amd64      macOS binary, Intel
#   ./build.sh macos-arm64      macOS binary, Apple silicon
#   ./build.sh extension        Chrome extension zip
#   ./build.sh bins             all four server binaries
#   ./build.sh all              binaries and the extension zip
set -euo pipefail

root="$(cd "$(dirname "$0")" && pwd)"
cd "$root"
artifact_dir="$root/private/dist"
mkdir -p "$artifact_dir"
cp "$root/docs/USAGE.md" "$artifact_dir/USAGE.md"
cp "$root/docs/start.bat.example" "$artifact_dir/start.bat.example"

build_server() {
  local goos="$1" goarch="$2" name="$3"
  local out="$artifact_dir/$name"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -o "$out" ./cmd/server
  echo "Wrote $out ($(du -h "$out" | awk '{print $1}'))"
}

pack_extension() {
  (cd "$root/extension" && npm run build)
  local built="$root/extension/.output/chrome-mv3"
  if [[ ! -f "$built/manifest.json" ]]; then
    echo "extension build did not produce $built/manifest.json" >&2
    exit 1
  fi
  local out="$artifact_dir/responseforge-extension.zip"
  rm -f "$out"
  (cd "$root/extension/.output" && zip -r "$out" chrome-mv3)
  echo "Wrote $out ($(du -h "$out" | awk '{print $1}'))"
}

build_bins() {
  build_server windows amd64 responseforge-windows-amd64.exe
  build_server windows arm64 responseforge-windows-arm64.exe
  build_server darwin amd64 responseforge-macos-amd64
  build_server darwin arm64 responseforge-macos-arm64
}

case "${1:-all}" in
  windows-amd64) build_server windows amd64 responseforge-windows-amd64.exe ;;
  windows-arm64) build_server windows arm64 responseforge-windows-arm64.exe ;;
  macos-amd64) build_server darwin amd64 responseforge-macos-amd64 ;;
  macos-arm64) build_server darwin arm64 responseforge-macos-arm64 ;;
  extension) pack_extension ;;
  bins) build_bins ;;
  all)
    build_bins
    pack_extension
    ;;
  *)
    echo "Unknown target: $1" >&2
    echo "Use: windows-amd64, windows-arm64, macos-amd64, macos-arm64, extension, bins, all" >&2
    exit 1
    ;;
esac
