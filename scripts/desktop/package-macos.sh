#!/usr/bin/env bash
# Packages Porto.app and a .dmg for macOS (universal: arm64 + amd64, window build). Needs macOS with the Xcode
# command line tools (clang, lipo, iconutil, codesign, hdiutil).
#
# Signing hooks (docs/desktop-build.md), never faked:
#   MACOS_SIGN_IDENTITY    "Developer ID Application: …": hardened-runtime signature with a timestamp
#   MACOS_NOTARY_PROFILE   notarytool keychain profile (needs the identity): notarisation and stapling
# Without them the app gets the ad-hoc signature Apple Silicon requires and the file name says -unsigned.
set -euo pipefail
. "$(dirname "$0")/common.sh"

[ "$(uname -s)" = Darwin ] || die "package-macos.sh runs on macOS"
require clang lipo iconutil codesign hdiutil
if [ -n "${MACOS_NOTARY_PROFILE:-}" ] && [ -z "${MACOS_SIGN_IDENTITY:-}" ]; then
	die "MACOS_NOTARY_PROFILE needs MACOS_SIGN_IDENTITY"
fi

work="$DESKTOP_DIST/macos"
rm -rf "$work"
mkdir -p "$work"

# Universal binary from two native-toolchain builds.
for arch in arm64 amd64; do
	carch="$arch"
	[ "$arch" = amd64 ] && carch=x86_64
	export CC="clang -arch $carch" MACOSX_DEPLOYMENT_TARGET=11.0
	go_build "$work/porto-desktop-$arch" darwin "$arch" webview
done
lipo -create -output "$work/porto-desktop" "$work/porto-desktop-arm64" "$work/porto-desktop-amd64"

app="$work/$APP_NAME.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$work/porto-desktop" "$app/Contents/MacOS/porto-desktop"
sed -e "s/@VERSION@/$NUMERIC_VERSION/g" -e "s/@BUNDLE_ID@/$BUNDLE_ID/g" \
	"$PACKAGING/macos/Info.plist.in" > "$app/Contents/Info.plist"

# Icon from the brand kit PNGs, copied as they are (ADR 0007).
iconset="$work/porto.iconset"
mkdir -p "$iconset"
cp "$ICON_KIT/porto_app_navy_64.png" "$iconset/icon_32x32@2x.png"
cp "$ICON_KIT/porto_app_navy_128.png" "$iconset/icon_128x128.png"
cp "$ICON_KIT/porto_app_navy_256.png" "$iconset/icon_128x128@2x.png"
cp "$ICON_KIT/porto_app_navy_256.png" "$iconset/icon_256x256.png"
cp "$ICON_KIT/porto_app_navy_512.png" "$iconset/icon_256x256@2x.png"
cp "$ICON_KIT/porto_app_navy_512.png" "$iconset/icon_512x512.png"
cp "$ICON_KIT/porto_app_navy_1024.png" "$iconset/icon_512x512@2x.png"
iconutil -c icns -o "$app/Contents/Resources/porto.icns" "$iconset"

suffix=""
if [ -n "${MACOS_SIGN_IDENTITY:-}" ]; then
	log "signing with the configured identity"
	codesign --force --options runtime --timestamp --entitlements "$PACKAGING/macos/entitlements.plist" \
		--sign "$MACOS_SIGN_IDENTITY" "$app/Contents/MacOS/porto-desktop"
	codesign --force --options runtime --timestamp --entitlements "$PACKAGING/macos/entitlements.plist" \
		--sign "$MACOS_SIGN_IDENTITY" "$app"
	[ -n "${MACOS_NOTARY_PROFILE:-}" ] || suffix="-unnotarized"
else
	log "no MACOS_SIGN_IDENTITY: ad-hoc signature (unsigned package)"
	codesign --force --sign - "$app/Contents/MacOS/porto-desktop"
	codesign --force --sign - "$app"
	suffix="-unsigned"
fi
codesign --verify --strict "$app"

dmg="$DESKTOP_DIST/${APP_NAME}_${NUMERIC_VERSION}_macos_universal${suffix}.dmg"
stage="$work/dmg"
mkdir -p "$stage"
cp -R "$app" "$stage/"
ln -s /Applications "$stage/Applications"
rm -f "$dmg"
hdiutil create -volname "$APP_NAME" -srcfolder "$stage" -ov -format UDZO "$dmg" >/dev/null

if [ -n "${MACOS_SIGN_IDENTITY:-}" ]; then
	codesign --force --timestamp --sign "$MACOS_SIGN_IDENTITY" "$dmg"
	if [ -n "${MACOS_NOTARY_PROFILE:-}" ]; then
		log "notarising"
		xcrun notarytool submit "$dmg" --keychain-profile "$MACOS_NOTARY_PROFILE" --wait
		xcrun stapler staple "$dmg"
	fi
fi
sha256_file "$dmg"
"$app/Contents/MacOS/porto-desktop" -version
echo "packaged $dmg"
