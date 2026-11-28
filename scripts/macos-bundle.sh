#!/usr/bin/env bash
# Builds dist/Docket.app: the desktop app wrapped in a macOS app bundle and
# signed ad hoc. macOS only lets an app with a bundle identifier post
# notifications, so the bare ./docket from `mise run app` never shows any.
set -euo pipefail

if [[ "$(uname -s)" != Darwin ]]; then
	echo "macos-bundle: this builds a macOS app bundle and has to run on macOS" >&2
	exit 1
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

app="dist/Docket.app"
version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
build="$(git rev-list --count HEAD 2>/dev/null || echo 1)"
short="${version#v}"
# CFBundleShortVersionString wants numbers; use 0.0.0 until there is a tag.
[[ "$short" =~ ^[0-9]+(\.[0-9]+){0,2} ]] && short="${BASH_REMATCH[0]}" || short="0.0.0"

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

echo "building docket $version"
CGO_ENABLED=1 go build -tags desktop,production \
	-ldflags "-X main.version=$version" \
	-o "$app/Contents/MacOS/docket" .

sed -e "s/@VERSION@/$short/" -e "s/@BUILD@/$build/" build/darwin/Info.plist \
	>"$app/Contents/Info.plist"
plutil -lint "$app/Contents/Info.plist" >/dev/null

# The icon, from the same PNG the window uses.
iconset="$(mktemp -d)/icons.iconset"
mkdir -p "$iconset"
src=internal/desktop/appicon.png
for size in 16 32 128 256 512; do
	sips -z "$size" "$size" "$src" --out "$iconset/icon_${size}x${size}.png" >/dev/null
	double=$((size * 2))
	sips -z "$double" "$double" "$src" --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$app/Contents/Resources/icons.icns"
rm -rf "$(dirname "$iconset")"

# An ad hoc signature is enough on your own Mac. Sharing the app with others
# needs a Developer ID signature and notarization.
codesign --force --deep --sign - "$app"

echo "built $app"
