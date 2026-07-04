#!/usr/bin/env bash

set -euo pipefail

version="${1:-0.0.0}"
build_number="${2:-1}"
expected_arch="${3:-$(go env GOARCH)}"

case "$version" in
  *[!0-9.]* | "")
    echo "version must contain only digits and dots: $version" >&2
    exit 2
    ;;
esac
case "$build_number" in
  *[!0-9.]* | "")
    echo "build number must contain only digits and dots: $build_number" >&2
    exit 2
    ;;
esac
case "$expected_arch" in
  arm64 | amd64) ;;
  *)
    echo "unsupported macOS architecture: $expected_arch" >&2
    exit 2
    ;;
esac

actual_arch="$(go env GOARCH)"
if [[ "$actual_arch" != "$expected_arch" ]]; then
  echo "runner architecture is $actual_arch, expected $expected_arch" >&2
  exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist="$root/dist"
work="$(mktemp -d "${TMPDIR:-/tmp}/lensm-dmg.XXXXXX")"
trap 'rm -rf "$work"' EXIT

app="$work/Lensm.app"
contents="$app/Contents"
mkdir -p "$contents/MacOS"
cp "$root/packaging/macos/Info.plist" "$contents/Info.plist"

/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $version" "$contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $build_number" "$contents/Info.plist"
plutil -lint "$contents/Info.plist"

(
  cd "$root"
  CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o "$contents/MacOS/lensm" .
)

# --deep is deprecated for signing (macOS 13+); sign nested code first.
codesign --force --sign - "$contents/MacOS/lensm"
codesign --force --sign - "$app"
codesign --verify --deep --strict "$app"

image="$work/image"
mkdir -p "$image"
cp -R "$app" "$image/Lensm.app"
ln -s /Applications "$image/Applications"

mkdir -p "$dist"
dmg="$dist/Lensm-${version}-macos-${expected_arch}.dmg"
rm -f "$dmg"
hdiutil create -quiet -volname Lensm -srcfolder "$image" -format UDZO -ov "$dmg"
hdiutil verify -quiet "$dmg"
echo "Created $dmg"
